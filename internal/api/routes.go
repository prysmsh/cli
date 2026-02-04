package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Route describes a DERP mesh route registered in the control plane.
type Route struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	OrganizationID int64      `json:"organization_id"`
	ClusterID      int64      `json:"cluster_id"`
	ServiceName    string     `json:"service_name"`
	ServicePort    int        `json:"service_port"`
	ExternalPort   int        `json:"external_port"`
	Protocol       string     `json:"protocol"`
	Status         string     `json:"status"`
	ExternalURL    string     `json:"external_url"`
	CreatedBy      int64      `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastUsed       *time.Time `json:"last_used"`
	UsageCount     int        `json:"usage_count"`
	Cluster        *Cluster   `json:"cluster"`
}

// RouteCreateRequest encapsulates payload for route creation.
type RouteCreateRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	ClusterID    int64  `json:"cluster_id"`
	ServiceName  string `json:"service_name"`
	ServicePort  int    `json:"service_port"`
	ExternalPort int    `json:"external_port"`
	Protocol     string `json:"protocol"`
}

// ListRoutes returns mesh routes visible to the authenticated organization.
func (c *Client) ListRoutes(ctx context.Context, clusterID *int64) ([]Route, error) {
	endpoint := "/routes"
	if clusterID != nil {
		v := url.Values{}
		v.Set("cluster_id", strconv.FormatInt(*clusterID, 10))
		endpoint = endpoint + "?" + v.Encode()
	}

	var resp struct {
		Routes []Route `json:"routes"`
		Total  int     `json:"total"`
	}

	if _, err := c.Do(ctx, "GET", endpoint, nil, &resp); err != nil {
		return nil, err
	}

	if resp.Routes == nil {
		return []Route{}, nil
	}
	return resp.Routes, nil
}

// CreateRoute provisions a new mesh route targeting an exit-capable cluster.
func (c *Client) CreateRoute(ctx context.Context, req RouteCreateRequest) (*Route, error) {
	var resp struct {
		Route Route  `json:"route"`
		Error string `json:"error"`
	}

	if _, err := c.Do(ctx, "POST", "/routes", req, &resp); err != nil {
		return nil, err
	}

	return &resp.Route, nil
}

// DeleteRoute removes an existing mesh route by identifier.
func (c *Client) DeleteRoute(ctx context.Context, routeID int64) error {
	endpoint := fmt.Sprintf("/routes/%d", routeID)
	_, err := c.Do(ctx, "DELETE", endpoint, nil, nil)
	return err
}

// SuggestRoutePort asks the control plane for an available external port.
func (c *Client) SuggestRoutePort(ctx context.Context, clusterID *int64) (int, error) {
	endpoint := "/routes/suggest-port"
	if clusterID != nil {
		v := url.Values{}
		v.Set("cluster_id", strconv.FormatInt(*clusterID, 10))
		endpoint = endpoint + "?" + v.Encode()
	}

	var resp struct {
		SuggestedPort int `json:"suggested_port"`
	}

	if _, err := c.Do(ctx, "GET", endpoint, nil, &resp); err != nil {
		return 0, err
	}

	if resp.SuggestedPort == 0 {
		return 0, fmt.Errorf("control plane returned no suggested port")
	}
	return resp.SuggestedPort, nil
}
