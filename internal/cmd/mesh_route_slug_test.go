package cmd

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prysmsh/cli/internal/api"
)

func TestRouteHostSlug(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"simple", "my-api", "myapi"}, // hyphen is not a separator, so dropped
		{"spaces", "My API", "my-api"},
		{"mixed case", "MyAPI", "myapi"},
		{"underscores", "my_api", "my-api"},
		{"slashes", "team/api", "team-api"},
		{"dots", "api.v1", "api-v1"},
		{"leading trailing space", "  foo  ", "foo"},
		{"numbers", "api2", "api2"},
		{"multiple spaces", "my   api", "my-api"},
		{"cluster name with hyphen", "frank-local", "franklocal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := routeHostSlug(tt.in)
			if got != tt.want {
				t.Errorf("routeHostSlug(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestResolveExitPeer_HostnamePatterns tests the exit-peer resolution logic used by
// the SOCKS5 proxy. Only the slug-based <route>.<cluster>.mesh path is supported.
func TestResolveExitPeer_HostnamePatterns(t *testing.T) {
	// Mock backend: serves clusters and mesh nodes.
	clusters := []api.Cluster{
		{ID: 1, Name: "frank-local"},
		{ID: 2, Name: "staging"},
	}
	meshNodes := []api.MeshNode{
		{ID: 100, ClusterID: int64Ptr(1), DeviceID: "device_abc", ExitEnabled: true, Status: "connected"},
		{ID: 101, ClusterID: int64Ptr(2), DeviceID: "device_def", ExitEnabled: true, Status: "connected"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/connect/k8s/clusters"):
			json.NewEncoder(w).Encode(map[string]interface{}{"clusters": clusters})
		case strings.Contains(r.URL.Path, "/mesh/nodes"):
			json.NewEncoder(w).Encode(map[string]interface{}{"nodes": meshNodes})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("test-token")

	// resolveExitPeer mirrors the slug-only resolution in mesh.go.
	resolveExitPeer := func(ctx context.Context, targetAddress string) (string, error) {
		host, _, err := net.SplitHostPort(targetAddress)
		if err != nil {
			return "", err
		}
		var clusterID int64

		if strings.HasSuffix(host, ".mesh") {
			parts := strings.Split(strings.TrimSuffix(host, ".mesh"), ".")
			if len(parts) == 2 {
				clusterSlug := parts[1]
				clusterList, err := client.ListClusters(ctx)
				if err != nil {
					return "", err
				}
				for _, c := range clusterList {
					if routeHostSlug(c.Name) == clusterSlug {
						clusterID = c.ID
						break
					}
				}
			}
		}

		if clusterID == 0 {
			return "", nil
		}
		nodes, err := client.ListMeshNodes(ctx)
		if err != nil {
			return "", err
		}
		for _, n := range nodes {
			if n.ClusterID != nil && *n.ClusterID == clusterID && n.ExitEnabled && n.Status == "connected" {
				return n.DeviceID, nil
			}
		}
		return "", nil
	}

	tests := []struct {
		name    string
		address string
		want    string // expected device ID (empty = not found)
	}{
		{
			name:    ".mesh path resolves cluster slug franklocal",
			address: "apifrank.franklocal.mesh:80",
			want:    "device_abc",
		},
		{
			name:    ".mesh path resolves cluster slug staging",
			address: "webui.staging.mesh:443",
			want:    "device_def",
		},
		{
			name:    "unknown .mesh cluster returns empty",
			address: "unknown.unknown.mesh:80",
			want:    "",
		},
		{
			name:    "non-.mesh host returns empty",
			address: "example.com:8080",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveExitPeer(context.Background(), tt.address)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveExitPeer(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestBuildRouteTargetByPeerAndPort(t *testing.T) {
	bindings := []meshRouteBinding{
		{PeerID: "peer-a", Port: 443, Target: "api-a.cluster-a.mesh:443"},
		{PeerID: "peer-a", Port: 8080, Target: "api-a.cluster-a.mesh:8080"},
		{PeerID: "peer-a", Port: 443, Target: "api-a.cluster-a.mesh:443"}, // duplicate
		{PeerID: "peer-b", Port: 3000, Target: "api-b.cluster-b.mesh:3000"},
		{PeerID: "peer-b", Port: 0, Target: "ignored"}, // ignored
		{PeerID: "", Port: 1234, Target: "ignored"},    // ignored
		{PeerID: "peer-c", Port: 9090, Target: ""},     // ignored
	}

	got := buildRouteTargetByPeerAndPort(bindings)

	target, ok := routeTargetForPeerPort(got, "peer-a", 443)
	if !ok || target != "api-a.cluster-a.mesh:443" {
		t.Fatalf("peer-a:443 target = %q ok=%v", target, ok)
	}
	target, ok = routeTargetForPeerPort(got, "peer-a", 8080)
	if !ok || target != "api-a.cluster-a.mesh:8080" {
		t.Fatalf("peer-a:8080 target = %q ok=%v", target, ok)
	}
	if _, ok := routeTargetForPeerPort(got, "peer-a", 3000); ok {
		t.Fatal("peer-a:3000 should not resolve")
	}
	target, ok = routeTargetForPeerPort(got, "peer-b", 3000)
	if !ok || target != "api-b.cluster-b.mesh:3000" {
		t.Fatalf("peer-b:3000 target = %q ok=%v", target, ok)
	}
	if _, ok := routeTargetForPeerPort(got, "peer-b", 0); ok {
		t.Fatal("peer-b:0 should not resolve")
	}
	if _, ok := routeTargetForPeerPort(got, "unknown-peer", 443); ok {
		t.Fatal("unknown-peer should not resolve any port")
	}
}
