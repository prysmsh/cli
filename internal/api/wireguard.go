package api

import (
	"context"
	"fmt"
	"net/url"
)

type WireguardDevice struct {
	ID            uint64  `json:"id"`
	DeviceID      string  `json:"device_id"`
	PublicKey     string  `json:"public_key"`
	Address       string  `json:"address"`
	Status        string  `json:"status"`
	ConfigVersion uint64  `json:"config_version"`
	VaultPath     string  `json:"vault_path"`
	LastSeenAt    *string `json:"last_seen_at"`
}

type WireguardClientConfig struct {
	Address                string   `json:"address"`
	CIDR                   string   `json:"cidr"`
	DNS                    []string `json:"dns"`
	MTU                    int      `json:"mtu"`
	PersistentKeepaliveSec int      `json:"persistent_keepalive"`
	GeneratedAt            string   `json:"generated_at"`
}

type WireguardPeer struct {
	Name                    string   `json:"name"`
	PublicKey               string   `json:"public_key"`
	Endpoint                string   `json:"endpoint"`
	AllowedIPs              []string `json:"allowed_ips"`
	DERPRegion              string   `json:"derp_region"`
	PersistentKeepaliveSecs int      `json:"persistent_keepalive"`
}

type WireguardConfigResponse struct {
	Device   WireguardDevice       `json:"device"`
	Config   WireguardClientConfig `json:"config"`
	Peers    []WireguardPeer       `json:"peers"`
	Warnings []string              `json:"warnings"`
}

type RegisterWireguardDeviceRequest struct {
	DeviceID     string                 `json:"device_id"`
	PublicKey    string                 `json:"public_key"`
	Capabilities map[string]interface{} `json:"capabilities,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

type RotateWireguardDeviceRequest struct {
	PublicKey string `json:"public_key"`
}

func (c *Client) RegisterWireguardDevice(ctx context.Context, req RegisterWireguardDeviceRequest) (*WireguardConfigResponse, error) {
	var resp WireguardConfigResponse
	if _, err := c.Do(ctx, "POST", "/mesh/wireguard/devices", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetWireguardConfig(ctx context.Context, deviceID string) (*WireguardConfigResponse, error) {
	endpoint := "/mesh/wireguard/config"
	if deviceID != "" {
		endpoint = fmt.Sprintf("%s?device_id=%s", endpoint, url.QueryEscape(deviceID))
	}
	var resp WireguardConfigResponse
	if _, err := c.Do(ctx, "GET", endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) RotateWireguardDevice(ctx context.Context, id uint64, publicKey string) (*WireguardConfigResponse, error) {
	payload := RotateWireguardDeviceRequest{PublicKey: publicKey}
	endpoint := fmt.Sprintf("/mesh/wireguard/devices/%d/rotate", id)
	var resp WireguardConfigResponse
	if _, err := c.Do(ctx, "POST", endpoint, payload, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteWireguardDevice(ctx context.Context, id uint64) error {
	endpoint := fmt.Sprintf("/mesh/wireguard/devices/%d", id)
	_, err := c.Do(ctx, "DELETE", endpoint, nil, nil)
	return err
}
