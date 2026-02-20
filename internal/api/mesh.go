package api

import (
	"context"
	"fmt"
	"time"
)

// MeshNode represents a peer in the DERP mesh network.
type MeshNode struct {
	ID             int64                  `json:"id"`
	OrganizationID int64                  `json:"organization_id"`
	ClusterID      *int64                 `json:"cluster_id"`
	UserID         *int64                 `json:"user_id"`
	DeviceID       string                 `json:"device_id"`
	PeerType       string                 `json:"peer_type"`
	Status         string                 `json:"status"`
	ExitEnabled    bool                   `json:"exit_enabled"`
	ExitPriority   int                    `json:"exit_priority"`
	ExitRegions    []string               `json:"exit_regions"`
	ExitNotes      string                 `json:"exit_notes"`
	LastPing       *time.Time             `json:"last_ping"`
	LastHealth     map[string]interface{} `json:"last_health"`
	Capabilities   map[string]interface{} `json:"capabilities"`
	UpdatedAt      time.Time              `json:"updated_at"`
	CreatedAt      time.Time              `json:"created_at"`
	DERPClientID   string                 `json:"derp_client_id"`
	WGAddress      string                 `json:"wg_address,omitempty"`
}

type meshListResponse struct {
	Nodes []MeshNode `json:"nodes"`
}

// RegisterMeshNode registers or updates a mesh peer.
func (c *Client) RegisterMeshNode(ctx context.Context, payload map[string]interface{}) (*MeshNode, error) {
	var resp struct {
		Message string   `json:"message"`
		Node    MeshNode `json:"node"`
	}
	if _, err := c.Do(ctx, "POST", "/mesh/nodes/register", payload, &resp); err != nil {
		return nil, err
	}
	return &resp.Node, nil
}

// ListMeshNodes retrieves mesh peers for the authenticated organization.
func (c *Client) ListMeshNodes(ctx context.Context) ([]MeshNode, error) {
	var resp meshListResponse
	if _, err := c.Do(ctx, "GET", "/mesh/nodes", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Nodes, nil
}

// PingMeshNode sends a keepalive ping for a mesh peer. Call periodically (e.g. every 60s)
// while connected so the backend marks the peer as connected. When the client stops pinging,
// the backend will mark it disconnected after the stale threshold.
func (c *Client) PingMeshNode(ctx context.Context, deviceID string) error {
	payload := map[string]string{"device_id": deviceID}
	_, err := c.Do(ctx, "POST", "/mesh/nodes/ping", payload, nil)
	return err
}

// EnableMeshNodeExit enables a mesh node (by ID) as an exit node.
func (c *Client) EnableMeshNodeExit(ctx context.Context, nodeID int64) error {
	payload := map[string]interface{}{"enable": true}
	_, err := c.Do(ctx, "POST", fmt.Sprintf("/mesh/nodes/%d/exit", nodeID), payload, nil)
	return err
}

// DisableMeshNodeExit disables a mesh node as an exit node.
func (c *Client) DisableMeshNodeExit(ctx context.Context, nodeID int64) error {
	_, err := c.Do(ctx, "DELETE", fmt.Sprintf("/mesh/nodes/%d/exit", nodeID), nil, nil)
	return err
}

// SetMeshNodeExitByDeviceID enables or disables a mesh node (by device_id) as an exit node.
func (c *Client) SetMeshNodeExitByDeviceID(ctx context.Context, deviceID string, enable bool) error {
	payload := map[string]interface{}{"enable": enable}
	_, err := c.Do(ctx, "PUT", fmt.Sprintf("/mesh/nodes/by-device/%s/exit", deviceID), payload, nil)
	return err
}

