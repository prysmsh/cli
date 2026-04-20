package wg

import (
	"context"
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
	Name       string   `json:"name"`
	PublicKey  string   `json:"public_key"`
	Endpoint   string   `json:"endpoint"`
	AllowedIPs []string `json:"allowed_ips"`
	DERPRegion string   `json:"derp_region,omitempty"`
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
func RegisterDevice(ctx context.Context, apiClient *api.Client, deviceID, publicKey string) (*WGConfig, error) {
	payload := map[string]string{
		"device_id":  deviceID,
		"public_key": publicKey,
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
	cfg, err := RegisterDevice(ctx, apiClient, deviceID, pubKey)
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
		tun.peers = append(tun.peers, PeerConfig{
			PublicKey:  p.PublicKey,
			Endpoint:  p.Endpoint,
			AllowedIPs: p.AllowedIPs,
		})
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

	cfg, err := RegisterDevice(ctx, apiClient, deviceID, pubKey)
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
		tun.peers = append(tun.peers, PeerConfig{
			PublicKey:  p.PublicKey,
			Endpoint:  p.Endpoint,
			AllowedIPs: p.AllowedIPs,
		})
	}

	if err := tun.StartWithDERPBind(bind); err != nil {
		bind.Close()
		return nil, nil, fmt.Errorf("start wireguard tunnel: %w", err)
	}

	return tun, bind, nil
}
