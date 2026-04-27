package wg

import (
	"context"
	"crypto/mlkem"
	"fmt"
	"os"

	"github.com/prysmsh/cli/internal/api"
)

// wgDevicePayload matches the backend wireguardDevicePayload.
type wgDevicePayload struct {
	ID        uint   `json:"id"`
	DeviceID  string `json:"device_id"`
	PublicKey string `json:"public_key"`
	Address   string `json:"address"`
	Status    string `json:"status"`
}

// wgClientConfig matches the backend wireguardClientConfig.
type wgClientConfig struct {
	Address string `json:"address"`
	CIDR    string `json:"cidr"`
	MTU     int    `json:"mtu"`
}

// WGPeer is a peer returned by the control plane.
type WGPeer struct {
	Name            string   `json:"name"`
	PublicKey       string   `json:"public_key"`
	Endpoint        string   `json:"endpoint"`
	AllowedIPs      []string `json:"allowed_ips"`
	DERPRegion      string   `json:"derp_region,omitempty"`
	MLKEMPublicKey  string   `json:"mlkem_public_key,omitempty"`  // peer's ML-KEM-768 encapsulation key (base64)
	MLKEMCiphertext string   `json:"mlkem_ciphertext,omitempty"` // ciphertext from encapsulator→us (base64)
}

// WGConfig is the WireGuard configuration returned by the control plane.
type WGConfig struct {
	Device   wgDevicePayload `json:"device"`
	Config   wgClientConfig  `json:"config"`
	Peers    []WGPeer        `json:"peers"`
	Warnings []string        `json:"warnings,omitempty"`
}

// RegisterDevice registers this device's WireGuard public key with the control plane
// and receives an overlay IP assignment and peer list.
// mlkemPublicKey is the base64-encoded ML-KEM-768 encapsulation key; pass empty to skip PQ.
func RegisterDevice(ctx context.Context, apiClient *api.Client, deviceID, publicKey, mlkemPublicKey string) (*WGConfig, error) {
	payload := map[string]string{
		"device_id":        deviceID,
		"public_key":       publicKey,
		"mlkem_public_key": mlkemPublicKey,
	}
	var resp WGConfig
	httpResp, err := apiClient.Do(ctx, "POST", "/mesh/wireguard/devices", payload, &resp)
	if err != nil {
		return nil, fmt.Errorf("register wireguard device: %w", err)
	}
	if httpResp != nil && httpResp.StatusCode >= 400 {
		return nil, fmt.Errorf("register wireguard device: %s", httpResp.Status)
	}
	return &resp, nil
}

// SubmitMLKEMCiphertext posts our ML-KEM ciphertext for a peer to the control plane.
// The backend stores it so the peer can retrieve and decapsulate it on their next config fetch.
func SubmitMLKEMCiphertext(ctx context.Context, apiClient *api.Client, fromDeviceID, toDeviceID, ciphertextB64 string) error {
	payload := map[string]string{
		"from_device_id": fromDeviceID,
		"to_device_id":   toDeviceID,
		"ciphertext":     ciphertextB64,
	}
	httpResp, err := apiClient.Do(ctx, "POST", "/mesh/wireguard/pq-ciphertext", payload, nil)
	if err != nil {
		return fmt.Errorf("submit ml-kem ciphertext: %w", err)
	}
	if httpResp != nil && httpResp.StatusCode >= 400 {
		return fmt.Errorf("submit ml-kem ciphertext: %s", httpResp.Status)
	}
	return nil
}

// GetConfig fetches the current WireGuard peer configuration for a device.
func GetConfig(ctx context.Context, apiClient *api.Client, deviceID string) (*WGConfig, error) {
	var resp WGConfig
	endpoint := fmt.Sprintf("/mesh/wireguard/config?device_id=%s", deviceID)
	httpResp, err := apiClient.Do(ctx, "GET", endpoint, nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("get wireguard config: %w", err)
	}
	if httpResp != nil && httpResp.StatusCode >= 400 {
		return nil, fmt.Errorf("get wireguard config: %s", httpResp.Status)
	}
	return &resp, nil
}

// SetupMeshWireGuard ensures keys exist, registers with the control plane,
// and starts an embedded WireGuard tunnel. The returned Tunnel should be
// stopped by the caller on shutdown.
func SetupMeshWireGuard(ctx context.Context, apiClient *api.Client, homeDir, deviceID string, insecure bool) (*Tunnel, error) {
	privKey, pubKey, err := EnsureKeyPair(homeDir)
	if err != nil {
		return nil, fmt.Errorf("ensure wireguard keypair: %w", err)
	}

	dk, mlkemPub, mlkemErr := EnsureMLKEMKeyPair(homeDir)
	if mlkemErr != nil {
		fmt.Fprintf(os.Stderr, "wireguard: ml-kem key setup failed, continuing without PQ: %v\n", mlkemErr)
		dk = nil
		mlkemPub = ""
	}

	cfg, err := RegisterDevice(ctx, apiClient, deviceID, pubKey, mlkemPub)
	if err != nil {
		return nil, err
	}

	overlayAddr := cfg.Device.Address
	if overlayAddr == "" {
		overlayAddr = cfg.Config.Address
	}
	if overlayAddr == "" {
		return nil, fmt.Errorf("control plane returned empty device address")
	}

	for _, w := range cfg.Warnings {
		fmt.Fprintf(os.Stderr, "wireguard: %s\n", w)
	}

	tun := NewTunnel(privKey, overlayAddr, 0)

	for _, p := range cfg.Peers {
		pc := PeerConfig{
			PublicKey:  p.PublicKey,
			Endpoint:   p.Endpoint,
			AllowedIPs: p.AllowedIPs,
		}
		if dk != nil && p.MLKEMPublicKey != "" {
			pc.PresharedKey = resolvePSK(ctx, apiClient, dk, deviceID, pubKey, p)
		}
		tun.peers = append(tun.peers, pc)
	}

	if err := tun.Start(); err != nil {
		return nil, fmt.Errorf("start wireguard tunnel (try: sudo prysm mesh connect): %w", err)
	}

	return tun, nil
}

// SetupMeshWireGuardDERP is like SetupMeshWireGuard but routes WireGuard packets
// through the DERP relay instead of raw UDP. This works through NAT/firewalls.
// Still requires sudo for TUN device creation.
// Returns the Tunnel and the DERPBind (caller must wire DERPBind.DeliverPacket
// to the DERP client's WGPacketHandler).
func SetupMeshWireGuardDERP(ctx context.Context, apiClient *api.Client, homeDir, deviceID string, sender DERPSender) (*Tunnel, *DERPBind, error) {
	privKey, pubKey, err := EnsureKeyPair(homeDir)
	if err != nil {
		return nil, nil, fmt.Errorf("ensure wireguard keypair: %w", err)
	}

	dk, mlkemPub, mlkemErr := EnsureMLKEMKeyPair(homeDir)
	if mlkemErr != nil {
		fmt.Fprintf(os.Stderr, "wireguard: ml-kem key setup failed, continuing without PQ: %v\n", mlkemErr)
		dk = nil
		mlkemPub = ""
	}

	cfg, err := RegisterDevice(ctx, apiClient, deviceID, pubKey, mlkemPub)
	if err != nil {
		return nil, nil, err
	}

	overlayAddr := cfg.Device.Address
	if overlayAddr == "" {
		overlayAddr = cfg.Config.Address
	}
	if overlayAddr == "" {
		return nil, nil, fmt.Errorf("control plane returned empty device address")
	}

	for _, w := range cfg.Warnings {
		fmt.Fprintf(os.Stderr, "wireguard: %s\n", w)
	}

	bind := NewDERPBind(sender)
	tun := NewTunnel(privKey, overlayAddr, 0)

	for _, p := range cfg.Peers {
		fmt.Fprintf(os.Stderr, "wireguard: adding peer %s endpoint=%s allowed=%v\n", p.PublicKey[:8], p.Endpoint, p.AllowedIPs)
		pc := PeerConfig{
			PublicKey:  p.PublicKey,
			Endpoint:   p.Endpoint,
			AllowedIPs: p.AllowedIPs,
		}
		if dk != nil && p.MLKEMPublicKey != "" {
			pc.PresharedKey = resolvePSK(ctx, apiClient, dk, deviceID, pubKey, p)
		}
		tun.peers = append(tun.peers, pc)
	}

	if err := tun.StartWithDERPBind(bind); err != nil {
		bind.Close()
		return nil, nil, fmt.Errorf("start wireguard tunnel: %w", err)
	}

	return tun, bind, nil
}

// resolvePSK derives the WireGuard PSK for a peer using bilateral ML-KEM.
// Both sides always encapsulate to each other; the PSK is HKDF(ss_A_to_B || ss_B_to_A)
// ordered by WG pubkey so both compute identical input. Falls back to one-sided PSK
// until the peer's ciphertext arrives. Returns empty on non-fatal failure so the
// tunnel falls back to Curve25519-only security.
func resolvePSK(ctx context.Context, apiClient *api.Client, dk *mlkem.DecapsulationKey768, deviceID, ourWGPub string, peer WGPeer) string {
	if peer.MLKEMPublicKey == "" {
		return ""
	}

	// Always encapsulate to the peer and submit our ciphertext.
	ct, ssOurs, err := Encapsulate(peer.MLKEMPublicKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wireguard: pq encapsulate for %s: %v\n", truncateKey(peer.PublicKey), err)
		return ""
	}
	if submitErr := SubmitMLKEMCiphertext(ctx, apiClient, deviceID, peer.Name, ct); submitErr != nil {
		fmt.Fprintf(os.Stderr, "wireguard: pq ciphertext submit for %s: %v\n", truncateKey(peer.PublicKey), submitErr)
	}

	// Decapsulate the peer's ciphertext if available (bilateral upgrade).
	var ssPeer []byte
	if peer.MLKEMCiphertext != "" {
		ssPeer, err = Decapsulate(dk, peer.MLKEMCiphertext)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wireguard: pq decapsulate for %s: %v\n", truncateKey(peer.PublicKey), err)
			// Continue with one-sided PSK rather than no PSK.
		}
	} else {
		fmt.Fprintf(os.Stderr, "wireguard: pq peer ciphertext not yet available for %s, using one-sided PSK\n", truncateKey(peer.PublicKey))
	}

	psk, err := DeriveBilateralPSK(ssOurs, ssPeer, ourWGPub, peer.PublicKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wireguard: pq derive psk for %s: %v\n", truncateKey(peer.PublicKey), err)
		return ""
	}
	return psk
}
