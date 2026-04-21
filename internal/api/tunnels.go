package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Tunnel describes a secure tunnel exposing a device port to authenticated mesh peers.
type Tunnel struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	OrganizationID  int64     `json:"organization_id"`
	TargetDeviceID  string    `json:"target_device_id"`
	Port            int       `json:"port"`
	ExternalPort    int       `json:"external_port"`
	ToPeerDeviceID  string    `json:"to_peer_device_id"`
	Protocol        string    `json:"protocol"`
	Status          string    `json:"status"`
	ExternalURL     string    `json:"external_url"`
	IsPublic        bool      `json:"is_public"`
	PublicSubdomain string    `json:"public_subdomain,omitempty"`
	TargetService   string     `json:"target_service,omitempty"`
	TargetNamespace string     `json:"target_namespace,omitempty"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	CreatedBy       int64      `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// TunnelCreateRequest encapsulates payload for tunnel creation.
type TunnelCreateRequest struct {
	Port            int    `json:"port"`
	Name            string `json:"name,omitempty"`
	TargetDeviceID  string `json:"target_device_id"`
	ToPeerDeviceID  string `json:"to_peer_device_id,omitempty"`
	ExternalPort    int    `json:"external_port,omitempty"`
	Protocol        string `json:"protocol,omitempty"`
	IsPublic        bool   `json:"is_public,omitempty"`
	TargetService   string `json:"target_service,omitempty"`
	TargetNamespace string `json:"target_namespace,omitempty"`
}

// CreateTunnel creates a new tunnel exposing a device port.
func (c *Client) CreateTunnel(ctx context.Context, req TunnelCreateRequest) (*Tunnel, error) {
	var resp struct {
		Tunnel Tunnel `json:"tunnel"`
		Error  string `json:"error"`
	}

	if _, err := c.Do(ctx, "POST", "/tunnels", req, &resp); err != nil {
		return nil, err
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("tunnel creation failed: %s", resp.Error)
	}

	return &resp.Tunnel, nil
}

// ListTunnels returns tunnels for the authenticated organization.
func (c *Client) ListTunnels(ctx context.Context, deviceID string) ([]Tunnel, error) {
	endpoint := "/tunnels"
	if deviceID != "" {
		v := url.Values{}
		v.Set("device_id", deviceID)
		endpoint = endpoint + "?" + v.Encode()
	}

	var resp struct {
		Tunnels []Tunnel `json:"tunnels"`
		Total   int      `json:"total"`
	}

	if _, err := c.Do(ctx, "GET", endpoint, nil, &resp); err != nil {
		return nil, err
	}

	if resp.Tunnels == nil {
		return []Tunnel{}, nil
	}
	return resp.Tunnels, nil
}

// DeleteTunnel removes a tunnel by identifier.
func (c *Client) DeleteTunnel(ctx context.Context, tunnelID int64) error {
	endpoint := fmt.Sprintf("/tunnels/%d", tunnelID)
	_, err := c.Do(ctx, "DELETE", endpoint, nil, nil)
	return err
}

// HeartbeatTunnel refreshes the tunnel's LastHeartbeatAt timestamp. The backend
// reaper expires tunnels that go silent, so the CLI exposing a tunnel is
// expected to call this periodically (every ~30s) while running.
func (c *Client) HeartbeatTunnel(ctx context.Context, tunnelID int64) error {
	endpoint := fmt.Sprintf("/tunnels/%d/heartbeat", tunnelID)
	_, err := c.Do(ctx, "POST", endpoint, nil, nil)
	return err
}

// DeleteTunnelByID removes a tunnel by string ID (for CLI args).
func (c *Client) DeleteTunnelByID(ctx context.Context, idStr string) error {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid tunnel id: %w", err)
	}
	return c.DeleteTunnel(ctx, id)
}

// GetClusterTunnelByName resolves a named ClusterTunnel record for a given cluster device ID.
// It uses ListTunnels filtered by the cluster device and searches by name (case-insensitive).
func (c *Client) GetClusterTunnelByName(ctx context.Context, clusterDeviceID, name string) (*Tunnel, error) {
	tunnels, err := c.ListTunnels(ctx, clusterDeviceID)
	if err != nil {
		return nil, err
	}
	for i := range tunnels {
		if strings.EqualFold(tunnels[i].Name, name) {
			return &tunnels[i], nil
		}
	}
	return nil, fmt.Errorf("no tunnel named %q found for cluster %s", name, clusterDeviceID)
}
