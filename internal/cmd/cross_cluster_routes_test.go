package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
)

// setupTestApp wires a mock HTTP backend into the package-level app so cobra commands work.
// It returns the mock server (caller must defer srv.Close()) and a reset function to restore app.
func setupTestApp(t *testing.T, handler http.Handler) (srv *httptest.Server, reset func()) {
	t.Helper()
	srv = httptest.NewServer(handler)
	client := api.NewClient(srv.URL)
	client.SetToken("test-token")

	prev := app
	app = &App{API: client}
	return srv, func() { app = prev }
}

// executeCommand runs a cobra command tree, capturing real os.Stdout and os.Stderr
// since the commands use fmt.Println / style.Render (not cmd.OutOrStdout).
func executeCommand(root *cobra.Command, args ...string) (stdout string, stderr string, err error) {
	// Capture stdout
	oldOut := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	// Capture stderr
	oldErr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	root.SetArgs(args)
	err = root.Execute()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	outBytes, _ := io.ReadAll(rOut)
	errBytes, _ := io.ReadAll(rErr)
	return string(outBytes), string(errBytes), err
}

// -------------------------------------------------------------------
// Cross-cluster route mock backend
// -------------------------------------------------------------------

type ccrMock struct {
	clusters []api.Cluster
	routes   []map[string]interface{}
	nextID   int
}

func newCCRMock() *ccrMock {
	return &ccrMock{
		clusters: []api.Cluster{
			{ID: 1, Name: "prod-us", Status: "connected"},
			{ID: 2, Name: "staging-eu", Status: "connected"},
			{ID: 3, Name: "dev-local", Status: "disconnected"},
		},
		nextID: 1,
	}
}

func (m *ccrMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch {
	// List clusters
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/connect/k8s/clusters"):
		json.NewEncoder(w).Encode(map[string]interface{}{"clusters": m.clusters})

	// List cross-cluster routes
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/cross-cluster-routes":
		filtered := m.routes
		if cid := r.URL.Query().Get("cluster_id"); cid != "" {
			filtered = nil
			for _, route := range m.routes {
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
		srcID, _ := body["source_cluster_id"].(float64)
		tgtID, _ := body["target_cluster_id"].(float64)
		if srcID == tgtID {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "source and target clusters must be different"})
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)
		route := map[string]interface{}{
			"id":                float64(m.nextID),
			"organization_id":   float64(1),
			"name":              body["name"],
			"source_cluster_id": body["source_cluster_id"],
			"target_cluster_id": body["target_cluster_id"],
			"target_service":    body["target_service"],
			"target_namespace":  body["target_namespace"],
			"target_port":       body["target_port"],
			"local_port":        body["local_port"],
			"protocol":          body["protocol"],
			"status":            "pending",
			"enabled":           true,
			"created_at":        now,
			"updated_at":        now,
			"source_cluster":    map[string]interface{}{"id": body["source_cluster_id"], "name": m.clusterName(int64(srcID))},
			"target_cluster":    map[string]interface{}{"id": body["target_cluster_id"], "name": m.clusterName(int64(tgtID))},
		}
		m.routes = append(m.routes, route)
		m.nextID++
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"route": route, "message": "created"})

	// Toggle
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/toggle"):
		parts := strings.Split(r.URL.Path, "/")
		idStr := parts[len(parts)-2]
		for i, route := range m.routes {
			if fmt.Sprintf("%.0f", route["id"]) == idStr {
				enabled := route["enabled"].(bool)
				m.routes[i]["enabled"] = !enabled
				if !enabled {
					m.routes[i]["status"] = "pending"
				} else {
					m.routes[i]["status"] = "disabled"
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"route": m.routes[i], "message": "toggled"})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})

	// Delete
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/cross-cluster-routes/"):
		parts := strings.Split(r.URL.Path, "/")
		idStr := parts[len(parts)-1]
		for i, route := range m.routes {
			if fmt.Sprintf("%.0f", route["id"]) == idStr {
				m.routes = append(m.routes[:i], m.routes[i+1:]...)
				json.NewEncoder(w).Encode(map[string]string{"message": "deleted"})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})

	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}
}

func (m *ccrMock) clusterName(id int64) string {
	for _, c := range m.clusters {
		if c.ID == id {
			return c.Name
		}
	}
	return fmt.Sprintf("cluster-%d", id)
}

// -------------------------------------------------------------------
// Tests: cross-cluster-routes cobra commands
// -------------------------------------------------------------------

func TestCCR_ListEmpty(t *testing.T) {
	mock := newCCRMock()
	srv, reset := setupTestApp(t, mock)
	defer srv.Close()
	defer reset()

	root := newMeshCommand()
	out, _, err := executeCommand(root, "cross-cluster-routes", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No cross-cluster routes") {
		t.Errorf("expected 'No cross-cluster routes' message, got:\n%s", out)
	}
}

func TestCCR_CreateAndList(t *testing.T) {
	mock := newCCRMock()
	srv, reset := setupTestApp(t, mock)
	defer srv.Close()
	defer reset()

	root := newMeshCommand()

	// Create a route
	out, _, err := executeCommand(root, "cross-cluster-routes", "create",
		"--name", "api-to-db",
		"--source", "prod-us",
		"--target", "staging-eu",
		"--service", "postgres",
		"--namespace", "data",
		"--target-port", "5432",
		"--local-port", "5433",
		"--protocol", "tcp",
	)
	if err != nil {
		t.Fatalf("create error: %v", err)
	}
	if !strings.Contains(out, "created") {
		t.Errorf("expected 'created' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "prod-us") || !strings.Contains(out, "staging-eu") {
		t.Errorf("expected cluster names in output, got:\n%s", out)
	}

	// List should show the route
	root2 := newMeshCommand()
	out, _, err = executeCommand(root2, "cross-cluster-routes", "list")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(out, "api-to-db") {
		t.Errorf("expected route name in list, got:\n%s", out)
	}
	if !strings.Contains(out, "5433") {
		t.Errorf("expected local port in list, got:\n%s", out)
	}
}

func TestCCR_CreateValidation(t *testing.T) {
	mock := newCCRMock()
	srv, reset := setupTestApp(t, mock)
	defer srv.Close()
	defer reset()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing name",
			args:    []string{"cross-cluster-routes", "create", "--source", "prod-us", "--target", "staging-eu", "--service", "svc", "--target-port", "80", "--local-port", "8080"},
			wantErr: "name",
		},
		{
			name:    "same source and target",
			args:    []string{"cross-cluster-routes", "create", "--name", "bad", "--source", "prod-us", "--target", "prod-us", "--service", "svc", "--target-port", "80", "--local-port", "8080"},
			wantErr: "different",
		},
		{
			name:    "invalid port zero",
			args:    []string{"cross-cluster-routes", "create", "--name", "bad", "--source", "prod-us", "--target", "staging-eu", "--service", "svc", "--target-port", "0", "--local-port", "8080"},
			wantErr: "target port",
		},
		{
			name:    "unknown cluster",
			args:    []string{"cross-cluster-routes", "create", "--name", "bad", "--source", "nonexistent", "--target", "staging-eu", "--service", "svc", "--target-port", "80", "--local-port", "8080"},
			wantErr: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newMeshCommand()
			_, _, err := executeCommand(root, tt.args...)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
				t.Errorf("error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestCCR_Toggle(t *testing.T) {
	mock := newCCRMock()
	srv, reset := setupTestApp(t, mock)
	defer srv.Close()
	defer reset()

	// Seed a route
	ctx := context.Background()
	_, err := app.API.CreateCrossClusterRoute(ctx, api.CrossClusterRouteCreateRequest{
		Name: "toggle-me", SourceClusterID: 1, TargetClusterID: 2,
		TargetService: "svc", TargetPort: 80, LocalPort: 9090,
	})
	if err != nil {
		t.Fatalf("seed route: %v", err)
	}

	// Toggle disable
	root := newMeshCommand()
	out, _, err := executeCommand(root, "cross-cluster-routes", "toggle", "1")
	if err != nil {
		t.Fatalf("toggle error: %v", err)
	}
	if !strings.Contains(out, "disabled") {
		t.Errorf("expected 'disabled' in output, got:\n%s", out)
	}

	// Toggle re-enable
	root2 := newMeshCommand()
	out, _, err = executeCommand(root2, "cross-cluster-routes", "toggle", "1")
	if err != nil {
		t.Fatalf("re-toggle error: %v", err)
	}
	if !strings.Contains(out, "enabled") {
		t.Errorf("expected 'enabled' in output, got:\n%s", out)
	}
}

func TestCCR_Delete(t *testing.T) {
	mock := newCCRMock()
	srv, reset := setupTestApp(t, mock)
	defer srv.Close()
	defer reset()

	// Seed
	ctx := context.Background()
	_, err := app.API.CreateCrossClusterRoute(ctx, api.CrossClusterRouteCreateRequest{
		Name: "delete-me", SourceClusterID: 1, TargetClusterID: 2,
		TargetService: "svc", TargetPort: 80, LocalPort: 9091,
	})
	if err != nil {
		t.Fatalf("seed route: %v", err)
	}

	root := newMeshCommand()
	out, _, err := executeCommand(root, "cross-cluster-routes", "delete", "1")
	if err != nil {
		t.Fatalf("delete error: %v", err)
	}
	if !strings.Contains(out, "deleted") {
		t.Errorf("expected 'deleted' in output, got:\n%s", out)
	}

	// Verify empty
	root2 := newMeshCommand()
	out, _, err = executeCommand(root2, "cross-cluster-routes", "list")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(out, "No cross-cluster routes") {
		t.Errorf("expected empty list after delete, got:\n%s", out)
	}
}

func TestCCR_DeleteNotFound(t *testing.T) {
	mock := newCCRMock()
	srv, reset := setupTestApp(t, mock)
	defer srv.Close()
	defer reset()

	root := newMeshCommand()
	_, _, err := executeCommand(root, "cross-cluster-routes", "delete", "999")
	if err == nil {
		t.Fatal("expected error for nonexistent route")
	}
}

func TestCCR_ListWithClusterFilter(t *testing.T) {
	mock := newCCRMock()
	srv, reset := setupTestApp(t, mock)
	defer srv.Close()
	defer reset()

	// Seed a route between cluster 1 and 2
	ctx := context.Background()
	_, err := app.API.CreateCrossClusterRoute(ctx, api.CrossClusterRouteCreateRequest{
		Name: "filtered", SourceClusterID: 1, TargetClusterID: 2,
		TargetService: "svc", TargetPort: 80, LocalPort: 7070,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Filter by prod-us — should show the route
	root := newMeshCommand()
	out, _, err := executeCommand(root, "cross-cluster-routes", "list", "--cluster", "prod-us")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(out, "filtered") {
		t.Errorf("expected route in filtered list, got:\n%s", out)
	}

	// Filter by dev-local — should be empty
	root2 := newMeshCommand()
	out, _, err = executeCommand(root2, "cross-cluster-routes", "list", "--cluster", "dev-local")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(out, "No cross-cluster routes") {
		t.Errorf("expected empty for unrelated cluster, got:\n%s", out)
	}
}

func TestCCR_AliasWorks(t *testing.T) {
	mock := newCCRMock()
	srv, reset := setupTestApp(t, mock)
	defer srv.Close()
	defer reset()

	// "ccr" alias should work
	root := newMeshCommand()
	out, _, err := executeCommand(root, "ccr", "list")
	if err != nil {
		t.Fatalf("alias error: %v", err)
	}
	if !strings.Contains(out, "No cross-cluster routes") {
		t.Errorf("expected output from alias, got:\n%s", out)
	}
}

func TestCCR_FullLifecycle(t *testing.T) {
	mock := newCCRMock()
	srv, reset := setupTestApp(t, mock)
	defer srv.Close()
	defer reset()

	// 1. List — empty
	root := newMeshCommand()
	out, _, err := executeCommand(root, "ccr", "list")
	if err != nil {
		t.Fatalf("step 1: %v", err)
	}
	if !strings.Contains(out, "No cross-cluster routes") {
		t.Fatalf("step 1: expected empty, got:\n%s", out)
	}

	// 2. Create
	root = newMeshCommand()
	out, _, err = executeCommand(root, "ccr", "create",
		"--name", "web-to-api",
		"--source", "prod-us",
		"--target", "staging-eu",
		"--service", "api-gateway",
		"--target-port", "8443",
		"--local-port", "9443",
	)
	if err != nil {
		t.Fatalf("step 2: %v", err)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("step 2: expected created, got:\n%s", out)
	}

	// 3. List — shows route
	root = newMeshCommand()
	out, _, err = executeCommand(root, "ccr", "list")
	if err != nil {
		t.Fatalf("step 3: %v", err)
	}
	if !strings.Contains(out, "web-to-api") {
		t.Fatalf("step 3: expected route, got:\n%s", out)
	}

	// 4. Toggle disable
	root = newMeshCommand()
	out, _, err = executeCommand(root, "ccr", "toggle", "1")
	if err != nil {
		t.Fatalf("step 4: %v", err)
	}
	if !strings.Contains(out, "disabled") {
		t.Fatalf("step 4: expected disabled, got:\n%s", out)
	}

	// 5. Toggle re-enable
	root = newMeshCommand()
	out, _, err = executeCommand(root, "ccr", "toggle", "1")
	if err != nil {
		t.Fatalf("step 5: %v", err)
	}
	if !strings.Contains(out, "enabled") {
		t.Fatalf("step 5: expected enabled, got:\n%s", out)
	}

	// 6. Delete
	root = newMeshCommand()
	out, _, err = executeCommand(root, "ccr", "delete", "1")
	if err != nil {
		t.Fatalf("step 6: %v", err)
	}
	if !strings.Contains(out, "deleted") {
		t.Fatalf("step 6: expected deleted, got:\n%s", out)
	}

	// 7. List — empty again
	root = newMeshCommand()
	out, _, err = executeCommand(root, "ccr", "list")
	if err != nil {
		t.Fatalf("step 7: %v", err)
	}
	if !strings.Contains(out, "No cross-cluster routes") {
		t.Fatalf("step 7: expected empty, got:\n%s", out)
	}
}
