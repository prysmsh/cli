package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Tunnel describes a secure tunnel exposing a device port to authenticated mesh peers.
type Tunnel struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	OrganizationID   int64     `json:"organization_id"`
	TargetDeviceID   string    `json:"target_device_id"`
	Port             int       `json:"port"`
	ExternalPort     int       `json:"external_port"`
	ToPeerDeviceID   string    `json:"to_peer_device_id"`
	Protocol         string    `json:"protocol"`
	Status           string    `json:"status"`
	ExternalURL      string    `json:"external_url"`
	IsPublic         bool      `json:"is_public"`
	PublicSubdomain  string    `json:"public_subdomain,omitempty"`
	CreatedBy        int64     `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// TunnelCreateRequest encapsulates payload for tunnel creation.
type TunnelCreateRequest struct {
	Port           int    `json:"port"`
	Name           string `json:"name,omitempty"`
	TargetDeviceID string `json:"target_device_id"`
	ToPeerDeviceID string `json:"to_peer_device_id,omitempty"`
	ExternalPort   int    `json:"external_port,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	IsPublic       bool   `json:"is_public,omitempty"`
}

// CreateTunnel creates a new tunnel exposing a device port.
func (c *Client) CreateTunnel(ctx context.Context, req TunnelCreateRequest) (*Tunnel, error) {
	var resp struct {
		Tunnel  Tunnel  `json:"tunnel"`
		Error   string  `json:"error"`
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
		Total   int     `json:"total"`
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

// DeleteTunnelByID removes a tunnel by string ID (for CLI args).
func (c *Client) DeleteTunnelByID(ctx context.Context, idStr string) error {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid tunnel id: %w", err)
	}
	return c.DeleteTunnel(ctx, id)
}
