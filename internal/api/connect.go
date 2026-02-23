package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
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

// DisconnectCluster marks the cluster as disconnected (required before delete if status is connected).
func (c *Client) DisconnectCluster(ctx context.Context, clusterID int64) error {
	_, err := c.Do(ctx, "POST", fmt.Sprintf("/clusters/%d/disconnect", clusterID), nil, nil)
	return err
}

// DeleteCluster removes the cluster and its related data. Cluster must be disconnected first.
func (c *Client) DeleteCluster(ctx context.Context, clusterID int64) error {
	resp, err := c.Do(ctx, "DELETE", fmt.Sprintf("/clusters/%d", clusterID), nil, nil)
	if err != nil {
		return err
	}
	if resp != nil && resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// ComponentConfigUpdate represents desired component image versions for a cluster.
type ComponentConfigUpdate struct {
	EBPFImage      string `json:"ebpf_image,omitempty"`
	LogImage       string `json:"log_image,omitempty"`
	CNIImage       string `json:"cni_image,omitempty"`
	FluentBitImage string `json:"fluentbit_image,omitempty"`
}

// UpdateComponentConfig pushes desired component image versions for a cluster.
func (c *Client) UpdateComponentConfig(ctx context.Context, clusterID int64, req ComponentConfigUpdate) error {
	_, err := c.Do(ctx, "PUT", fmt.Sprintf("/clusters/%d/components", clusterID), req, nil)
	return err
}

// GetComponentConfig retrieves current component image overrides for a cluster.
func (c *Client) GetComponentConfig(ctx context.Context, clusterID int64) (*ComponentConfigUpdate, error) {
	var resp ComponentConfigUpdate
	if _, err := c.Do(ctx, "GET", fmt.Sprintf("/clusters/%d/components", clusterID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// clusterIDFlexible unmarshals cluster_id from JSON as either string or number (backend may send either).
type clusterIDFlexible string

func (c *clusterIDFlexible) UnmarshalJSON(b []byte) error {
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*c = clusterIDFlexible(s)
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*c = clusterIDFlexible(strconv.FormatInt(int64(n), 10))
	return nil
}

func (c clusterIDFlexible) String() string { return string(c) }

// ZeroTrustConfig is the Zero Trust / mesh config for a cluster.
type ZeroTrustConfig struct {
	ClusterID         clusterIDFlexible `json:"cluster_id"`
	Enabled           bool              `json:"enabled"`
	CNITargetPort     string            `json:"cni_target_port"`
	ExcludeNamespaces string            `json:"exclude_namespaces"`
	CNIImage          string            `json:"cni_image"`
}

// ZeroTrustStatus is the current CNI/mesh status reported by the agent.
type ZeroTrustStatus struct {
	ClusterID          clusterIDFlexible `json:"cluster_id"`
	CNIReady           bool              `json:"cni_ready"`
	CNIPods            int               `json:"cni_pods"`
	CNIPodsReady       int               `json:"cni_pods_ready"`
	EnrolledNamespaces int               `json:"enrolled_namespaces"`
	EnrolledPods       int               `json:"enrolled_pods"`
	Version            string            `json:"version"`
}

// ZeroTrustConfigsResponse is the response from GET /zero-trust/configs.
type ZeroTrustConfigsResponse struct {
	Configs  []ZeroTrustConfig  `json:"configs"`
	Statuses []ZeroTrustStatus  `json:"statuses"`
}

// GetZeroTrustConfigs returns Zero Trust configs and statuses for the org (used to check mesh onboarding).
func (c *Client) GetZeroTrustConfigs(ctx context.Context) (*ZeroTrustConfigsResponse, error) {
	var resp ZeroTrustConfigsResponse
	if _, err := c.Do(ctx, "GET", "/zero-trust/configs", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// MeshTopologyDiagnostics is the _diagnostics object returned when topology is empty (for mesh-debug).
type MeshTopologyDiagnostics struct {
	TotalEventsInStore   int                      `json:"total_events_in_store"`
	ClustersWithData     []string                 `json:"clusters_with_data"`
	PerClusterEventCount map[string]int64         `json:"per_cluster_event_count"`
	RegisteredClusters   []map[string]interface{} `json:"registered_clusters"`
	ClusterFilter        *string                  `json:"cluster_filter"`
	WindowHint           string                   `json:"window_hint"`
}

// MeshTopologyResponse is the response from GET /zero-trust/topology.
type MeshTopologyResponse struct {
	Nodes       []map[string]interface{}  `json:"nodes"`
	Edges       []map[string]interface{} `json:"edges"`
	Diagnostics *MeshTopologyDiagnostics `json:"_diagnostics,omitempty"`
}

// GetMeshTopology returns mesh topology (nodes/edges) and diagnostics when empty. since: e.g. "48h", "24h".
func (c *Client) GetMeshTopology(ctx context.Context, since string) (*MeshTopologyResponse, error) {
	endpoint := "/zero-trust/topology"
	if since != "" {
		endpoint = "/zero-trust/topology?since=" + since
	}
	var resp MeshTopologyResponse
	if _, err := c.Do(ctx, "GET", endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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
