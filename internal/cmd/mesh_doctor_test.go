package cmd

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/prysmsh/cli/internal/api"
)

func TestMeshDoctor_Summary(t *testing.T) {
	clusterID := int64(7)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/mesh/nodes":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"nodes": []api.MeshNode{
					{
						ID:              1,
						ClusterID:       &clusterID,
						DeviceID:        "cluster_7",
						ExitEnabled:     true,
						Status:          "connected",
						AdvertisedCIDRs: []string{"10.233.0.11/32"},
					},
				},
			})
		case "/api/v1/routes":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"routes": []api.Route{
					{ID: 1, Status: "active"},
					{ID: 2, Status: "disabled"},
				},
				"total": 2,
			})
		case "/api/v1/connect/k8s/clusters":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"clusters": []api.Cluster{
					{ID: clusterID, Name: "frank", IsExitRouter: true, WGOverlayCIDR: "10.233.0.11/32"},
				},
				"count": 1,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	})

	srv, reset := setupTestApp(t, handler)
	defer srv.Close()
	defer reset()

	root := newMeshCommand()
	stdout, _, err := executeCommand(root, "doctor", "--fix=false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertContains(t, stdout, "mesh process:")
	assertContains(t, stdout, "mesh nodes: 1 total, 1 exit-enabled connected")
	assertContains(t, stdout, "subnet CIDRs: 1 discovered")
	assertContains(t, stdout, "mesh routes: 2 total, 1 active")
}

func TestMeshDoctor_HandlesAPIErrors(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "boom"})
	})

	srv, reset := setupTestApp(t, handler)
	defer srv.Close()
	defer reset()

	root := newMeshCommand()
	stdout, _, err := executeCommand(root, "doctor", "--fix=false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertContains(t, stdout, "mesh nodes: lookup failed:")
	assertContains(t, stdout, "mesh routes: lookup failed:")
}

func TestMeshDoctor_FixInvokesCleanup(t *testing.T) {
	clusterID := int64(9)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/mesh/nodes":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"nodes": []api.MeshNode{
					{
						ID:              1,
						ClusterID:       &clusterID,
						DeviceID:        "cluster_9",
						ExitEnabled:     true,
						Status:          "connected",
						AdvertisedCIDRs: []string{"10.2.0.0/16", "10.1.0.0/16"},
					},
				},
			})
		case "/api/v1/routes":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"routes": []api.Route{}, "total": 0})
		case "/api/v1/connect/k8s/clusters":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"clusters": []api.Cluster{
					{ID: clusterID, Name: "frank", IsExitRouter: true, WGOverlayCIDR: "10.2.0.0/16"},
				},
				"count": 1,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	})

	srv, reset := setupTestApp(t, handler)
	defer srv.Close()
	defer reset()

	origCleanup := cleanupSubnetStaleRedirects
	defer func() { cleanupSubnetStaleRedirects = origCleanup }()

	var gotCIDRs []string
	called := 0
	cleanupSubnetStaleRedirects = func(cidrs []string) int {
		called++
		gotCIDRs = append([]string(nil), cidrs...)
		return 0
	}

	root := newMeshCommand()
	stdout, _, err := executeCommand(root, "doctor", "--fix=true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if called != 1 {
		t.Fatalf("cleanup called %d times, want 1", called)
	}
	wantCIDRs := []string{"10.1.0.0/16", "10.2.0.0/16"}
	if !reflect.DeepEqual(gotCIDRs, wantCIDRs) {
		t.Fatalf("cleanup cidrs = %#v, want %#v", gotCIDRs, wantCIDRs)
	}
	assertContains(t, stdout, "subnet stale redirects removed: 0")
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected output to contain %q, got:\n%s", want, got)
	}
}
