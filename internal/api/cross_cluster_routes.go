package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// CrossClusterRoute describes a route that bridges two clusters via DERP relay.
type CrossClusterRoute struct {
	ID               int64     `json:"id"`
	OrganizationID   int64     `json:"organization_id"`
	Name             string    `json:"name"`
	SourceClusterID  int64     `json:"source_cluster_id"`
	TargetClusterID  int64     `json:"target_cluster_id"`
	TargetService    string    `json:"target_service"`
	TargetNamespace  string    `json:"target_namespace"`
	TargetPort       int       `json:"target_port"`
	LocalPort        int       `json:"local_port"`
	Protocol         string    `json:"protocol"`
	Status           string    `json:"status"`
	ConnectionMethod string    `json:"connection_method"`
	Enabled          bool      `json:"enabled"`
	CreatedBy        int64     `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	SourceCluster *Cluster `json:"source_cluster,omitempty"`
	TargetCluster *Cluster `json:"target_cluster,omitempty"`
}

// CrossClusterRouteCreateRequest encapsulates payload for cross-cluster route creation.
type CrossClusterRouteCreateRequest struct {
	Name            string `json:"name"`
	SourceClusterID int64  `json:"source_cluster_id"`
	TargetClusterID int64  `json:"target_cluster_id"`
	TargetService   string `json:"target_service"`
	TargetNamespace string `json:"target_namespace,omitempty"`
	TargetPort      int    `json:"target_port"`
	LocalPort       int    `json:"local_port"`
	Protocol        string `json:"protocol,omitempty"`
}

// ListCrossClusterRoutes returns cross-cluster routes for the authenticated organization.
func (c *Client) ListCrossClusterRoutes(ctx context.Context, clusterID *int64) ([]CrossClusterRoute, error) {
	endpoint := "/cross-cluster-routes"
	if clusterID != nil {
		v := url.Values{}
		v.Set("cluster_id", strconv.FormatInt(*clusterID, 10))
		endpoint = endpoint + "?" + v.Encode()
	}

	var resp struct {
		Routes []CrossClusterRoute `json:"routes"`
		Total  int                 `json:"total"`
	}

	if _, err := c.Do(ctx, "GET", endpoint, nil, &resp); err != nil {
		return nil, err
	}

	if resp.Routes == nil {
		return []CrossClusterRoute{}, nil
	}
	return resp.Routes, nil
}

// CreateCrossClusterRoute provisions a new cross-cluster route.
func (c *Client) CreateCrossClusterRoute(ctx context.Context, req CrossClusterRouteCreateRequest) (*CrossClusterRoute, error) {
	var resp struct {
		Route   CrossClusterRoute `json:"route"`
		Message string            `json:"message"`
		Error   string            `json:"error"`
	}

	if _, err := c.Do(ctx, "POST", "/cross-cluster-routes", req, &resp); err != nil {
		return nil, err
	}

	return &resp.Route, nil
}

// DeleteCrossClusterRoute removes an existing cross-cluster route by identifier.
func (c *Client) DeleteCrossClusterRoute(ctx context.Context, routeID int64) error {
	endpoint := fmt.Sprintf("/cross-cluster-routes/%d", routeID)
	_, err := c.Do(ctx, "DELETE", endpoint, nil, nil)
	return err
}

// ToggleCrossClusterRoute enables or disables a cross-cluster route.
func (c *Client) ToggleCrossClusterRoute(ctx context.Context, routeID int64) (*CrossClusterRoute, error) {
	endpoint := fmt.Sprintf("/cross-cluster-routes/%d/toggle", routeID)
	var resp struct {
		Route   CrossClusterRoute `json:"route"`
		Message string            `json:"message"`
	}
	if _, err := c.Do(ctx, "PUT", endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Route, nil
}
