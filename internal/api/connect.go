package api

import (
	"context"
	"fmt"
	"time"
)

// Cluster represents a Kubernetes cluster registered with Prysm.
type Cluster struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`
	Namespace     string     `json:"namespace"`
	IsExitRouter  bool       `json:"is_exit_router"`
	MeshIP        string     `json:"mesh_ip,omitempty"`
	WGOverlayCIDR string     `json:"wg_overlay_cidr,omitempty"`
	Region        string     `json:"region,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	LastPing      *time.Time `json:"last_ping"`
}

// KubeconfigMaterial contains the encoded kubeconfig from connect response.
type KubeconfigMaterial struct {
	Encoding string `json:"encoding"`
	Value    string `json:"value"`
}

// KubernetesSessionInfo captures session state returned by the API.
type KubernetesSessionInfo struct {
	ID        int64      `json:"id"`
	SessionID string     `json:"session_id"`
	Status    string     `json:"status"`
	StartedAt *time.Time `json:"started_at"`
}

// ClusterConnectResponse is the payload from /connect/k8s.
type ClusterConnectResponse struct {
	Cluster      Cluster                `json:"cluster"`
	Session      KubernetesSessionInfo  `json:"session"`
	Kubeconfig   KubeconfigMaterial     `json:"kubeconfig"`
	Recording    map[string]interface{} `json:"recording"`
	PolicyChecks map[string]interface{} `json:"policy_checks"`
	IssuedAt     time.Time              `json:"issued_at"`
}

type listClustersResponse struct {
	Clusters  []Cluster `json:"clusters"`
	Count     int       `json:"count"`
	Timestamp time.Time `json:"timestamp"`
}

// ListClusters retrieves clusters the authenticated user can access.
func (c *Client) ListClusters(ctx context.Context) ([]Cluster, error) {
	var resp listClustersResponse
	if _, err := c.Do(ctx, "GET", "/connect/k8s/clusters", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Clusters, nil
}

// EnableClusterExitRouter enables a cluster as an exit router (traffic egress node).
func (c *Client) EnableClusterExitRouter(ctx context.Context, clusterID int64) error {
	payload := map[string]interface{}{"enable": true}
	_, err := c.Do(ctx, "POST", fmt.Sprintf("/clusters/%d/exit-router", clusterID), payload, nil)
	return err
}

// DisableClusterExitRouter disables a cluster as an exit router.
func (c *Client) DisableClusterExitRouter(ctx context.Context, clusterID int64) error {
	_, err := c.Do(ctx, "DELETE", fmt.Sprintf("/clusters/%d/exit-router", clusterID), nil, nil)
	return err
}

// ConnectKubernetes issues a short-lived kubeconfig for the requested cluster.
func (c *Client) ConnectKubernetes(ctx context.Context, clusterID int64, namespace, reason string) (*ClusterConnectResponse, error) {
	payload := map[string]interface{}{
		"cluster_id": clusterID,
	}
	if namespace != "" {
		payload["namespace"] = namespace
	}
	if reason != "" {
		payload["reason"] = reason
	}
	// So the backend puts this URL in the kubeconfig (proxy); kubectl will use the same backend the CLI uses.
	if u := c.BasePublicURL(); u != "" {
		payload["backend_public_url"] = u
	}

	var resp ClusterConnectResponse
	if _, err := c.Do(ctx, "POST", "/connect/k8s", payload, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
