package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prysmsh/cli/internal/api"
)

// TestCrossClusterRoutes_E2E exercises the full lifecycle: list(empty) → create → list → toggle → delete → list(empty).
func TestCrossClusterRoutes_E2E(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var routeStore []map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		// List
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/cross-cluster-routes":
			filtered := routeStore
			if cid := r.URL.Query().Get("cluster_id"); cid != "" {
				filtered = nil
				for _, route := range routeStore {
					src := fmt.Sprintf("%.0f", route["source_cluster_id"])
					tgt := fmt.Sprintf("%.0f", route["target_cluster_id"])
					if src == cid || tgt == cid {
						filtered = append(filtered, route)
					}
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"routes": filtered, "total": len(filtered)})

		// Create
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/cross-cluster-routes":
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}

			// Validate required fields
			if body["name"] == nil || body["name"] == "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
				return
			}
			srcID, _ := body["source_cluster_id"].(float64)
			tgtID, _ := body["target_cluster_id"].(float64)
			if srcID == tgtID {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "source and target clusters must be different"})
				return
			}

			// Check local_port conflict
			localPort, _ := body["local_port"].(float64)
			for _, existing := range routeStore {
				if existing["source_cluster_id"] == body["source_cluster_id"] &&
					existing["local_port"] == body["local_port"] &&
					existing["enabled"] == true {
					w.WriteHeader(http.StatusConflict)
					json.NewEncoder(w).Encode(map[string]string{
						"error": fmt.Sprintf("local_port %.0f already in use on source cluster", localPort),
					})
					return
				}
			}

			route := map[string]interface{}{
				"id":                float64(len(routeStore) + 1),
				"organization_id":  float64(1),
				"name":             body["name"],
				"source_cluster_id": body["source_cluster_id"],
				"target_cluster_id": body["target_cluster_id"],
				"target_service":   body["target_service"],
				"target_namespace": body["target_namespace"],
				"target_port":      body["target_port"],
				"local_port":       body["local_port"],
				"protocol":         body["protocol"],
				"status":           "pending",
				"enabled":          true,
				"created_at":       now.Format(time.RFC3339),
				"updated_at":       now.Format(time.RFC3339),
				"source_cluster":   map[string]interface{}{"id": body["source_cluster_id"], "name": "cluster-a"},
				"target_cluster":   map[string]interface{}{"id": body["target_cluster_id"], "name": "cluster-b"},
			}
			routeStore = append(routeStore, route)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"route":   route,
				"message": "Cross-cluster route created successfully",
			})

		// Toggle
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/toggle"):
			parts := strings.Split(r.URL.Path, "/")
			idStr := parts[len(parts)-2]
			found := false
			for i, route := range routeStore {
				if fmt.Sprintf("%.0f", route["id"]) == idStr {
					enabled := route["enabled"].(bool)
					routeStore[i]["enabled"] = !enabled
					if !enabled {
						routeStore[i]["status"] = "pending"
					} else {
						routeStore[i]["status"] = "disabled"
					}
					json.NewEncoder(w).Encode(map[string]interface{}{
						"route":   routeStore[i],
						"message": "toggled",
					})
					found = true
					break
				}
			}
			if !found {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "Cross-cluster route not found"})
			}

		// Delete
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/cross-cluster-routes/"):
			parts := strings.Split(r.URL.Path, "/")
			idStr := parts[len(parts)-1]
			found := false
			for i, route := range routeStore {
				if fmt.Sprintf("%.0f", route["id"]) == idStr {
					routeStore = append(routeStore[:i], routeStore[i+1:]...)
					json.NewEncoder(w).Encode(map[string]string{"message": "Cross-cluster route deleted successfully"})
					found = true
					break
				}
			}
			if !found {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "Cross-cluster route not found"})
			}

		default:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("test-token")
	ctx := context.Background()

	// Step 1: List empty
	t.Run("list empty", func(t *testing.T) {
		routes, err := client.ListCrossClusterRoutes(ctx, nil)
		if err != nil {
			t.Fatalf("ListCrossClusterRoutes: %v", err)
		}
		if len(routes) != 0 {
			t.Fatalf("expected 0 routes, got %d", len(routes))
		}
	})

	// Step 2: Create route
	var createdID int64
	t.Run("create route", func(t *testing.T) {
		route, err := client.CreateCrossClusterRoute(ctx, api.CrossClusterRouteCreateRequest{
			Name:            "api-to-db",
			SourceClusterID: 1,
			TargetClusterID: 2,
			TargetService:   "postgres",
			TargetNamespace: "data",
			TargetPort:      5432,
			LocalPort:       5433,
			Protocol:        "tcp",
		})
		if err != nil {
			t.Fatalf("CreateCrossClusterRoute: %v", err)
		}
		if route.Name != "api-to-db" {
			t.Errorf("name = %q, want %q", route.Name, "api-to-db")
		}
		if route.Status != "pending" {
			t.Errorf("status = %q, want %q", route.Status, "pending")
		}
		if !route.Enabled {
			t.Error("expected enabled = true")
		}
		if route.SourceCluster == nil || route.SourceCluster.Name != "cluster-a" {
			t.Error("expected source_cluster to be populated")
		}
		createdID = route.ID
	})

	// Step 3: List returns the created route
	t.Run("list returns created", func(t *testing.T) {
		routes, err := client.ListCrossClusterRoutes(ctx, nil)
		if err != nil {
			t.Fatalf("ListCrossClusterRoutes: %v", err)
		}
		if len(routes) != 1 {
			t.Fatalf("expected 1 route, got %d", len(routes))
		}
		if routes[0].ID != createdID {
			t.Errorf("route id = %d, want %d", routes[0].ID, createdID)
		}
	})

	// Step 4: List with cluster filter
	t.Run("list with cluster filter", func(t *testing.T) {
		cid := int64(1)
		routes, err := client.ListCrossClusterRoutes(ctx, &cid)
		if err != nil {
			t.Fatalf("ListCrossClusterRoutes: %v", err)
		}
		if len(routes) != 1 {
			t.Fatalf("expected 1 route for cluster 1, got %d", len(routes))
		}

		// Unrelated cluster should see nothing
		unrelated := int64(999)
		routes, err = client.ListCrossClusterRoutes(ctx, &unrelated)
		if err != nil {
			t.Fatalf("ListCrossClusterRoutes: %v", err)
		}
		if len(routes) != 0 {
			t.Fatalf("expected 0 routes for unrelated cluster, got %d", len(routes))
		}
	})

	// Step 5: Toggle disable
	t.Run("toggle disable", func(t *testing.T) {
		route, err := client.ToggleCrossClusterRoute(ctx, createdID)
		if err != nil {
			t.Fatalf("ToggleCrossClusterRoute: %v", err)
		}
		if route.Enabled {
			t.Error("expected enabled = false after toggle")
		}
		if route.Status != "disabled" {
			t.Errorf("status = %q, want %q", route.Status, "disabled")
		}
	})

	// Step 6: Toggle re-enable
	t.Run("toggle re-enable", func(t *testing.T) {
		route, err := client.ToggleCrossClusterRoute(ctx, createdID)
		if err != nil {
			t.Fatalf("ToggleCrossClusterRoute: %v", err)
		}
		if !route.Enabled {
			t.Error("expected enabled = true after re-toggle")
		}
		if route.Status != "pending" {
			t.Errorf("status = %q, want %q", route.Status, "pending")
		}
	})

	// Step 7: Delete
	t.Run("delete route", func(t *testing.T) {
		if err := client.DeleteCrossClusterRoute(ctx, createdID); err != nil {
			t.Fatalf("DeleteCrossClusterRoute: %v", err)
		}
	})

	// Step 8: List empty again
	t.Run("list empty after delete", func(t *testing.T) {
		routes, err := client.ListCrossClusterRoutes(ctx, nil)
		if err != nil {
			t.Fatalf("ListCrossClusterRoutes: %v", err)
		}
		if len(routes) != 0 {
			t.Fatalf("expected 0 routes after delete, got %d", len(routes))
		}
	})
}

// TestCrossClusterRoutes_CreateValidation verifies client-server validation errors.
func TestCrossClusterRoutes_CreateValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		if body["name"] == nil || body["name"] == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
			return
		}
		src, _ := body["source_cluster_id"].(float64)
		tgt, _ := body["target_cluster_id"].(float64)
		if src == tgt {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "source and target clusters must be different"})
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"route": map[string]interface{}{
				"id": 1, "name": body["name"], "status": "pending",
				"created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("tok")
	ctx := context.Background()

	t.Run("same source and target rejected", func(t *testing.T) {
		_, err := client.CreateCrossClusterRoute(ctx, api.CrossClusterRouteCreateRequest{
			Name:            "bad-route",
			SourceClusterID: 5,
			TargetClusterID: 5,
			TargetService:   "svc",
			TargetPort:      80,
			LocalPort:       8080,
		})
		if err == nil {
			t.Fatal("expected error for same source/target")
		}
		if !strings.Contains(err.Error(), "same") && !strings.Contains(err.Error(), "different") {
			t.Errorf("error = %v, want mention of same/different clusters", err)
		}
	})
}

// TestCrossClusterRoutes_DeleteNotFound verifies 404 on non-existent route.
func TestCrossClusterRoutes_DeleteNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Cross-cluster route not found"})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("tok")

	err := client.DeleteCrossClusterRoute(context.Background(), 99999)
	if err == nil {
		t.Fatal("expected error for nonexistent route")
	}
}

// TestCrossClusterRoutes_ToggleNotFound verifies 404 on non-existent route toggle.
func TestCrossClusterRoutes_ToggleNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Cross-cluster route not found"})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("tok")

	_, err := client.ToggleCrossClusterRoute(context.Background(), 99999)
	if err == nil {
		t.Fatal("expected error for nonexistent route")
	}
}

// TestCrossClusterRoutes_ListNilRoutes ensures nil routes response returns empty slice.
func TestCrossClusterRoutes_ListNilRoutes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"routes": nil, "total": 0})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	routes, err := client.ListCrossClusterRoutes(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListCrossClusterRoutes: %v", err)
	}
	if routes == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(routes) != 0 {
		t.Errorf("len = %d, want 0", len(routes))
	}
}

// TestCrossClusterRoutes_ConflictPort verifies port conflict detection.
func TestCrossClusterRoutes_ConflictPort(t *testing.T) {
	created := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			if created {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{"error": "local_port 9090 already in use on source cluster"})
				return
			}
			created = true
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"route": map[string]interface{}{
					"id": 1, "name": "first", "status": "pending", "local_port": 9090,
					"created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z",
				},
			})
		}
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("tok")
	ctx := context.Background()

	// First create succeeds
	_, err := client.CreateCrossClusterRoute(ctx, api.CrossClusterRouteCreateRequest{
		Name: "first", SourceClusterID: 1, TargetClusterID: 2,
		TargetService: "svc", TargetPort: 80, LocalPort: 9090,
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	// Second create with same local_port should fail
	_, err = client.CreateCrossClusterRoute(ctx, api.CrossClusterRouteCreateRequest{
		Name: "dup", SourceClusterID: 1, TargetClusterID: 2,
		TargetService: "other", TargetPort: 443, LocalPort: 9090,
	})
	if err == nil {
		t.Fatal("expected conflict error for duplicate local_port")
	}
	if !strings.Contains(err.Error(), "local_port") && !strings.Contains(err.Error(), "9090") {
		t.Errorf("error = %v, want mention of local_port conflict", err)
	}
}

// TestCrossClusterRoutes_CreateResponseFields verifies all returned fields are parsed correctly.
func TestCrossClusterRoutes_CreateResponseFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"route": map[string]interface{}{
				"id":                42,
				"organization_id":   1,
				"name":              "edge-link",
				"source_cluster_id": 10,
				"target_cluster_id": 20,
				"target_service":    "api-gateway",
				"target_namespace":  "production",
				"target_port":       8443,
				"local_port":        9443,
				"protocol":          "tcp",
				"status":            "pending",
				"connection_method": "",
				"enabled":           true,
				"created_by":        5,
				"created_at":        "2025-06-15T10:30:00Z",
				"updated_at":        "2025-06-15T10:30:00Z",
				"source_cluster": map[string]interface{}{
					"id":   10,
					"name": "edge-us-east",
				},
				"target_cluster": map[string]interface{}{
					"id":   20,
					"name": "core-eu-west",
				},
			},
			"message": "Cross-cluster route created successfully",
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("tok")

	route, err := client.CreateCrossClusterRoute(context.Background(), api.CrossClusterRouteCreateRequest{
		Name:            "edge-link",
		SourceClusterID: 10,
		TargetClusterID: 20,
		TargetService:   "api-gateway",
		TargetNamespace: "production",
		TargetPort:      8443,
		LocalPort:       9443,
		Protocol:        "tcp",
	})
	if err != nil {
		t.Fatalf("CreateCrossClusterRoute: %v", err)
	}

	if route.ID != 42 {
		t.Errorf("ID = %d, want 42", route.ID)
	}
	if route.Name != "edge-link" {
		t.Errorf("Name = %q, want %q", route.Name, "edge-link")
	}
	if route.SourceClusterID != 10 {
		t.Errorf("SourceClusterID = %d, want 10", route.SourceClusterID)
	}
	if route.TargetClusterID != 20 {
		t.Errorf("TargetClusterID = %d, want 20", route.TargetClusterID)
	}
	if route.TargetService != "api-gateway" {
		t.Errorf("TargetService = %q, want %q", route.TargetService, "api-gateway")
	}
	if route.TargetNamespace != "production" {
		t.Errorf("TargetNamespace = %q, want %q", route.TargetNamespace, "production")
	}
	if route.TargetPort != 8443 {
		t.Errorf("TargetPort = %d, want 8443", route.TargetPort)
	}
	if route.LocalPort != 9443 {
		t.Errorf("LocalPort = %d, want 9443", route.LocalPort)
	}
	if route.Protocol != "tcp" {
		t.Errorf("Protocol = %q, want %q", route.Protocol, "tcp")
	}
	if route.Status != "pending" {
		t.Errorf("Status = %q, want %q", route.Status, "pending")
	}
	if !route.Enabled {
		t.Error("Enabled = false, want true")
	}
	if route.SourceCluster == nil {
		t.Fatal("SourceCluster is nil")
	}
	if route.SourceCluster.Name != "edge-us-east" {
		t.Errorf("SourceCluster.Name = %q, want %q", route.SourceCluster.Name, "edge-us-east")
	}
	if route.TargetCluster == nil {
		t.Fatal("TargetCluster is nil")
	}
	if route.TargetCluster.Name != "core-eu-west" {
		t.Errorf("TargetCluster.Name = %q, want %q", route.TargetCluster.Name, "core-eu-west")
	}
}
