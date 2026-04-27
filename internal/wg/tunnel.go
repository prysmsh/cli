package wg

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// PeerConfig defines a WireGuard peer.
type PeerConfig struct {
	PublicKey    string
	Endpoint     string
	AllowedIPs   []string
	PresharedKey string // 32-byte hex PSK derived from ML-KEM (empty = no PSK)
}

// Tunnel manages an embedded userspace WireGuard interface.
type Tunnel struct {
	interfaceName string
	privateKey    wgtypes.Key
	overlayIP     string
	listenPort    int
	peers         []PeerConfig
	tunDevice     tun.Device
	wgDevice      *device.Device
}

// EnsureKeyPair creates or loads a WireGuard key pair stored under homeDir.
// Returns the private key and the base64-encoded public key.
func EnsureKeyPair(homeDir string) (privKey wgtypes.Key, pubKeyB64 string, err error) {
	privKeyPath := filepath.Join(homeDir, "prysm0.key")
	pubKeyFile := filepath.Join(homeDir, "prysm0.pub")

	// Try loading existing key pair.
	if data, readErr := os.ReadFile(privKeyPath); readErr == nil {
		decoded := strings.TrimSpace(string(data))
		if k, parseErr := wgtypes.ParseKey(decoded); parseErr == nil {
			pub := k.PublicKey().String()
			_ = os.WriteFile(pubKeyFile, []byte(pub+"\n"), 0o644)
			return k, pub, nil
		}
	}

	// Generate new key pair.
	privKey, err = wgtypes.GeneratePrivateKey()
	if err != nil {
		return wgtypes.Key{}, "", fmt.Errorf("generate wireguard key: %w", err)
	}

	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return wgtypes.Key{}, "", fmt.Errorf("create key dir: %w", err)
	}
	if err := os.WriteFile(privKeyPath, []byte(privKey.String()+"\n"), 0o600); err != nil {
		return wgtypes.Key{}, "", fmt.Errorf("write private key: %w", err)
	}

	pubKey := privKey.PublicKey().String()
	if err := os.WriteFile(pubKeyFile, []byte(pubKey+"\n"), 0o644); err != nil {
		return wgtypes.Key{}, "", fmt.Errorf("write public key: %w", err)
	}

	return privKey, pubKey, nil
}

// NewTunnel constructs a Tunnel that is ready to Start.
func NewTunnel(privateKey wgtypes.Key, overlayIP string, listenPort int) *Tunnel {
	return &Tunnel{
		privateKey: privateKey,
		overlayIP:  overlayIP,
		listenPort: listenPort,
	}
}

// Start brings up the embedded WireGuard interface.
func (t *Tunnel) Start() error {
	if err := CheckTUNPrivileges(); err != nil {
		return err
	}

	// CreateTUN can hang if privileges are insufficient (despite the check above
	// catching most cases). Use a timeout to avoid blocking forever.
	type tunResult struct {
		dev tun.Device
		err error
	}
	ch := make(chan tunResult, 1)
	go func() {
		d, e := tun.CreateTUN("utun", device.DefaultMTU)
		ch <- tunResult{d, e}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return fmt.Errorf("create tun device: %w", res.err)
		}
		t.tunDevice = res.dev
	case <-time.After(5 * time.Second):
		return fmt.Errorf("tun device creation timed out — likely a permissions issue, re-run with: sudo prysm mesh connect")
	}

	ifaceName, err := t.tunDevice.Name()
	if err != nil {
		t.tunDevice.Close()
		return fmt.Errorf("get tun name: %w", err)
	}
	t.interfaceName = ifaceName

	logger := device.NewLogger(device.LogLevelSilent, "")
	wgDev := device.NewDevice(t.tunDevice, conn.NewDefaultBind(), logger)
	t.wgDevice = wgDev

	// Configure private key and listen port via UAPI.
	var uapi strings.Builder
	uapi.WriteString(fmt.Sprintf("private_key=%s\n", hexKey(t.privateKey)))
	if t.listenPort > 0 {
		uapi.WriteString(fmt.Sprintf("listen_port=%d\n", t.listenPort))
	}
	if err := wgDev.IpcSet(uapi.String()); err != nil {
		wgDev.Close()
		return fmt.Errorf("configure wireguard device: %w", err)
	}

	if err := wgDev.Up(); err != nil {
		wgDev.Close()
		return fmt.Errorf("bring up wireguard device: %w", err)
	}

	// Assign IP address and bring interface up (macOS ifconfig — always available).
	if err := configureInterface(t.interfaceName, t.overlayIP); err != nil {
		wgDev.Close()
		return fmt.Errorf("configure interface: %w", err)
	}

	// Add initial peers.
	for _, p := range t.peers {
		if err := t.addPeer(p); err != nil {
			log.Printf("wireguard: failed to add peer %s: %v", truncateKey(p.PublicKey), err)
		}
	}

	return nil
}

// StartWithDERPBind brings up the WireGuard interface using DERP as transport
// instead of raw UDP. This allows WireGuard to work through NAT and firewalls.
// Still requires sudo for TUN device creation.
func (t *Tunnel) StartWithDERPBind(bind *DERPBind) error {
	if err := CheckTUNPrivileges(); err != nil {
		return err
	}

	type tunResult struct {
		dev tun.Device
		err error
	}
	ch := make(chan tunResult, 1)
	go func() {
		d, e := tun.CreateTUN("utun", device.DefaultMTU)
		ch <- tunResult{d, e}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return fmt.Errorf("create tun device: %w", res.err)
		}
		t.tunDevice = res.dev
	case <-time.After(5 * time.Second):
		return fmt.Errorf("tun device creation timed out — re-run with: sudo prysm mesh connect")
	}

	ifaceName, err := t.tunDevice.Name()
	if err != nil {
		t.tunDevice.Close()
		return fmt.Errorf("get tun name: %w", err)
	}
	t.interfaceName = ifaceName

	logger := device.NewLogger(device.LogLevelSilent, "")
	// Use DERP bind instead of UDP — packets flow through the DERP WebSocket relay.
	wgDev := device.NewDevice(t.tunDevice, bind, logger)
	t.wgDevice = wgDev

	var uapi strings.Builder
	uapi.WriteString(fmt.Sprintf("private_key=%s\n", hexKey(t.privateKey)))
	if err := wgDev.IpcSet(uapi.String()); err != nil {
		wgDev.Close()
		return fmt.Errorf("configure wireguard device: %w", err)
	}

	if err := wgDev.Up(); err != nil {
		wgDev.Close()
		return fmt.Errorf("bring up wireguard device: %w", err)
	}

	if err := configureInterface(t.interfaceName, t.overlayIP); err != nil {
		wgDev.Close()
		return fmt.Errorf("configure interface: %w", err)
	}

	for _, p := range t.peers {
		if err := t.addPeerDERP(p); err != nil {
			log.Printf("wireguard: failed to add peer %s: %v", truncateKey(p.PublicKey), err)
		}
	}

	return nil
}

// addPeerDERP configures a peer for DERP transport. The endpoint is the peer's
// DERP device ID (not a UDP address), so wireguard-go routes packets through
// the DERPBind which sends them via the DERP WebSocket relay.
func (t *Tunnel) addPeerDERP(p PeerConfig) error {
	pubKey, err := wgtypes.ParseKey(p.PublicKey)
	if err != nil {
		return fmt.Errorf("parse peer public key: %w", err)
	}

	var uapi strings.Builder
	uapi.WriteString(fmt.Sprintf("public_key=%s\n", hexKey(pubKey)))
	if p.PresharedKey != "" {
		uapi.WriteString(fmt.Sprintf("preshared_key=%s\n", p.PresharedKey))
	}
	// For DERP bind, the endpoint is the peer's device ID — DERPBind.ParseEndpoint
	// creates a derpEndpoint from it.
	if p.Endpoint != "" {
		uapi.WriteString(fmt.Sprintf("endpoint=%s\n", p.Endpoint))
	}
	uapi.WriteString(fmt.Sprintf("persistent_keepalive_interval=%d\n", 25))
	uapi.WriteString("replace_allowed_ips=true\n")
	for _, cidr := range p.AllowedIPs {
		uapi.WriteString(fmt.Sprintf("allowed_ip=%s\n", cidr))
	}

	fmt.Fprintf(os.Stderr, "wireguard: IpcSet for peer %s:\n%s\n", truncateKey(p.PublicKey), uapi.String())
	if err := t.wgDevice.IpcSet(uapi.String()); err != nil {
		return fmt.Errorf("configure peer %s: %w", truncateKey(p.PublicKey), err)
	}
	fmt.Fprintf(os.Stderr, "wireguard: peer %s configured OK\n", truncateKey(p.PublicKey))

	for _, cidr := range p.AllowedIPs {
		if err := addRoute(cidr, t.interfaceName); err != nil {
			return fmt.Errorf("route: %w", err)
		}
	}

	return nil
}

// AddPeer adds a peer to the running tunnel.
func (t *Tunnel) AddPeer(p PeerConfig) error {
	t.peers = append(t.peers, p)
	if t.wgDevice == nil {
		return nil // not started yet, will be applied on Start
	}
	return t.addPeer(p)
}

// Stop tears down the WireGuard interface.
func (t *Tunnel) Stop() error {
	if t.wgDevice != nil {
		t.wgDevice.Close()
		t.wgDevice = nil
	}
	if t.tunDevice != nil {
		t.tunDevice.Close()
		t.tunDevice = nil
	}

	// Clean up routes.
	if t.interfaceName != "" {
		for _, p := range t.peers {
			for _, cidr := range p.AllowedIPs {
				_ = exec.Command("route", "-n", "delete", "-net", cidr, "-interface", t.interfaceName).Run()
			}
		}
		t.interfaceName = ""
	}

	return nil
}

// addPeer configures a single peer via UAPI and adds routes.
func (t *Tunnel) addPeer(p PeerConfig) error {
	pubKey, err := wgtypes.ParseKey(p.PublicKey)
	if err != nil {
		return fmt.Errorf("parse peer public key: %w", err)
	}

	var uapi strings.Builder
	uapi.WriteString(fmt.Sprintf("public_key=%s\n", hexKey(pubKey)))
	if p.PresharedKey != "" {
		uapi.WriteString(fmt.Sprintf("preshared_key=%s\n", p.PresharedKey))
	}
	if p.Endpoint != "" {
		addr, resolveErr := resolveEndpoint(p.Endpoint)
		if resolveErr != nil {
			return fmt.Errorf("resolve endpoint %s: %w", p.Endpoint, resolveErr)
		}
		uapi.WriteString(fmt.Sprintf("endpoint=%s\n", addr))
	}
	uapi.WriteString(fmt.Sprintf("persistent_keepalive_interval=%d\n", 25))
	uapi.WriteString("replace_allowed_ips=true\n")
	for _, cidr := range p.AllowedIPs {
		uapi.WriteString(fmt.Sprintf("allowed_ip=%s\n", cidr))
	}

	if err := t.wgDevice.IpcSet(uapi.String()); err != nil {
		return fmt.Errorf("configure peer %s: %w", truncateKey(p.PublicKey), err)
	}

	for _, cidr := range p.AllowedIPs {
		if err := addRoute(cidr, t.interfaceName); err != nil {
			return fmt.Errorf("route: %w", err)
		}
	}

	return nil
}

func hexKey(k wgtypes.Key) string {
	return fmt.Sprintf("%x", k[:])
}

func resolveEndpoint(endpoint string) (string, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", err
	}
	if _, parseErr := netip.ParseAddr(host); parseErr == nil {
		return endpoint, nil
	}
	ips, err := net.LookupHost(host)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no addresses for %s", host)
	}
	return net.JoinHostPort(ips[0], port), nil
}

func truncateKey(key string) string {
	if len(key) > 8 {
		return key[:8]
	}
	return key
}

// GetPeers returns the current peer list.
func (t *Tunnel) GetPeers() []PeerConfig {
	return t.peers
}

// PrivateKeyBase64 returns the private key in base64 encoding (for NE config).
func (t *Tunnel) PrivateKeyBase64() string {
	return t.privateKey.String()
}

// Peers returns the configured peer list.
func (t *Tunnel) Peers() []PeerConfig {
	return t.peers
}

// RetriggerHandshake forces wireguard-go to re-initiate a handshake with a peer
// by removing and re-adding it via UAPI. This does NOT close the bind.
func (t *Tunnel) RetriggerHandshake(p PeerConfig) error {
	pubKey, err := wgtypes.ParseKey(p.PublicKey)
	if err != nil {
		return err
	}
	// Remove peer to reset handshake state.
	removeUAPI := fmt.Sprintf("public_key=%s\nremove=true\n", hexKey(pubKey))
	_ = t.wgDevice.IpcSet(removeUAPI)

	// Re-add with full config.
	return t.addPeerDERP(p)
}

func (t *Tunnel) IsRunning() bool {
	return t.wgDevice != nil && t.interfaceName != ""
}

func (t *Tunnel) InterfaceName() string {
	return t.interfaceName
}

func (t *Tunnel) OverlayIP() string {
	return t.overlayIP
}
