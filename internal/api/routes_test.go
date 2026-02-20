package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/warp-run/prysm-cli/internal/api"
)

func TestListRoutesWithClusterFilter(t *testing.T) {
	var captured struct {
		method string
		path   string
		query  string
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.query = r.URL.RawQuery

		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v1/routes" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("cluster_id") != "42" {
			t.Fatalf("expected cluster filter")
		}

		payload := map[string]any{
			"routes": []map[string]any{
				{
					"id":            7,
					"name":          "edge-http",
					"cluster_id":    42,
					"service_name":  "internal-api",
					"service_port":  443,
					"external_port": 30443,
					"protocol":      "TCP",
					"status":        "active",
					"external_url":  "derp.example.com:30443",
					"created_at":    "2024-01-01T00:00:00Z",
					"updated_at":    "2024-01-01T00:00:00Z",
				},
			},
			"total": 1,
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	clusterID := int64(42)
	routes, err := client.ListRoutes(context.Background(), &clusterID)
	if err != nil {
		t.Fatalf("ListRoutes error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].ClusterID != clusterID {
		t.Fatalf("expected cluster id %d, got %d", clusterID, routes[0].ClusterID)
	}
	if captured.query != "cluster_id=42" {
		t.Fatalf("unexpected query: %s", captured.query)
	}
}

func TestCreateRoute_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"route name exists","code":"CONFLICT"}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	_, err := client.CreateRoute(context.Background(), api.RouteCreateRequest{
		Name: "dup", ClusterID: 1, ServiceName: "svc", ServicePort: 80, ExternalPort: 30080, Protocol: "TCP",
	})
	if err == nil {
		t.Fatal("expected error from CreateRoute")
	}
}

func TestCreateRoute_ResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"route": map[string]any{
				"id":            1,
				"name":          "r1",
				"description":  "desc",
				"cluster_id":    10,
				"service_name": "svc",
				"service_port": 80,
				"external_port": 30080,
				"protocol":      "TCP",
				"status":       "active",
				"external_url": "derp:30080",
				"created_at":   "2024-01-01T00:00:00Z",
				"updated_at":   "2024-01-01T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	route, err := client.CreateRoute(context.Background(), api.RouteCreateRequest{
		Name: "r1", Description: "desc", ClusterID: 10,
		ServiceName: "svc", ServicePort: 80, ExternalPort: 30080, Protocol: "TCP",
	})
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	if route.Name != "r1" || route.ClusterID != 10 {
		t.Errorf("route = %+v", route)
	}
}

func TestCreateAndDeleteRoute(t *testing.T) {
	type receivedRequest struct {
		Method string
		Path   string
		Body   map[string]any
	}

	requests := make(chan receivedRequest, 2)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		requests <- receivedRequest{Method: r.Method, Path: r.URL.Path, Body: body}

		switch r.Method {
		case http.MethodPost:
			if r.URL.Path != "/api/v1/routes" {
				t.Fatalf("unexpected create path: %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"route": map[string]any{
					"id":            9,
					"name":          body["name"],
					"cluster_id":    body["cluster_id"],
					"service_name":  body["service_name"],
					"service_port":  body["service_port"],
					"external_port": body["external_port"],
					"protocol":      body["protocol"],
					"status":        "pending",
					"created_at":    "2024-01-01T00:00:00Z",
					"updated_at":    "2024-01-01T00:00:00Z",
				},
			})
		case http.MethodDelete:
			if r.URL.Path != "/api/v1/routes/9" {
				t.Fatalf("unexpected delete path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)

	route, err := client.CreateRoute(context.Background(), api.RouteCreateRequest{
		Name:         "edge",
		ClusterID:    99,
		ServiceName:  "svc",
		ServicePort:  8080,
		ExternalPort: 30080,
		Protocol:     "TCP",
	})
	if err != nil {
		t.Fatalf("CreateRoute error: %v", err)
	}
	if route.ID != 9 {
		t.Fatalf("unexpected route id: %d", route.ID)
	}

	if err := client.DeleteRoute(context.Background(), 9); err != nil {
		t.Fatalf("DeleteRoute error: %v", err)
	}

	var sawPost, sawDelete bool
	for i := 0; i < 2; i++ {
		req := <-requests
		switch req.Method {
		case http.MethodPost:
			sawPost = true
			if req.Body["cluster_id"] != float64(99) {
				t.Fatalf("unexpected cluster id payload: %v", req.Body["cluster_id"])
			}
		case http.MethodDelete:
			sawDelete = true
		}
	}
	if !sawPost || !sawDelete {
		t.Fatalf("missing requests: post=%v delete=%v", sawPost, sawDelete)
	}
}

func TestSuggestRoutePort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/routes/suggest-port" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("cluster_id") != "55" {
			t.Fatalf("missing cluster query")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"suggested_port": 30123})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	clusterID := int64(55)
	port, err := client.SuggestRoutePort(context.Background(), &clusterID)
	if err != nil {
		t.Fatalf("SuggestRoutePort error: %v", err)
	}
	if port != 30123 {
		t.Fatalf("expected port 30123, got %d", port)
	}
}

func TestListRoutesWithoutClusterID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/routes" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("expected no query, got %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"routes": []map[string]any{}, "total": 0})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	routes, err := client.ListRoutes(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRoutes error: %v", err)
	}
	if len(routes) != 0 {
		t.Fatalf("expected 0 routes, got %d", len(routes))
	}
}

func TestSuggestRoutePortWithoutClusterID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Fatalf("expected no query, got %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"suggested_port": 30000})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	port, err := client.SuggestRoutePort(context.Background(), nil)
	if err != nil {
		t.Fatalf("SuggestRoutePort error: %v", err)
	}
	if port != 30000 {
		t.Fatalf("expected port 30000, got %d", port)
	}
}

func TestSuggestRoutePortZeroError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"suggested_port": 0})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	_, err := client.SuggestRoutePort(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when suggested_port is 0")
	}
	if !strings.Contains(err.Error(), "no suggested port") {
		t.Errorf("error = %v", err)
	}
}
