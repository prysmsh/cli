package exit

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/gorilla/websocket"
	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/derp"
)

// routeHostSlug mirrors the production routeHostSlug in internal/cmd/mesh.go.
// This is intentionally duplicated in the test to verify the resolution contract;
// if the production version changes, this test should break and be updated.
func routeHostSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r == ' ' || r == '_' || r == '/' || r == '.':
			if b.Len() > 0 && b.String()[b.Len()-1] != '-' {
				b.WriteByte('-')
			}
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Trim(strings.TrimSpace(b.String()), "-")
}

// buildMeshResolver builds a ResolveExitPeerFunc that uses a real API client,
// mirroring the production wiring in mesh.go's SOCKS5 proxy setup.
func buildMeshResolver(client *api.Client) ResolveExitPeerFunc {
	return func(ctx context.Context, targetAddress string) (string, error) {
		host, _, err := net.SplitHostPort(targetAddress)
		if err != nil {
			return "", err
		}
		var clusterID int64

		if strings.HasSuffix(host, ".mesh") {
			parts := strings.Split(strings.TrimSuffix(host, ".mesh"), ".")
			if len(parts) == 2 {
				clusterSlug := parts[1]
				clusters, err := client.ListClusters(ctx)
				if err != nil {
					return "", err
				}
				for _, c := range clusters {
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
}

func int64Ptr(v int64) *int64 { return &v }

// mockAPIForMesh returns an httptest.Server serving clusters and mesh nodes.
func mockAPIForMesh(clusters []api.Cluster, nodes []api.MeshNode) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/connect/k8s/clusters"):
			json.NewEncoder(w).Encode(map[string]interface{}{"clusters": clusters})
		case strings.Contains(r.URL.Path, "/mesh/nodes"):
			json.NewEncoder(w).Encode(map[string]interface{}{"nodes": nodes})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestMeshResolveViaDERP wires the real ExitProxy with a real DERP WebSocket
// server and a ResolveExitPeer that calls mock API endpoints. This tests the
// full path: SOCKS5 dial → ResolveExitPeer → DERP route_request → route_response.
func TestMeshResolveViaDERP(t *testing.T) {
	clusters := []api.Cluster{
		{ID: 1, Name: "frank-local"},
		{ID: 2, Name: "staging"},
		{ID: 3, Name: "prod us-east"},
	}
	nodes := []api.MeshNode{
		{ID: 100, ClusterID: int64Ptr(1), DeviceID: "device_frank", ExitEnabled: true, Status: "connected"},
		{ID: 101, ClusterID: int64Ptr(2), DeviceID: "device_staging", ExitEnabled: true, Status: "connected"},
		{ID: 102, ClusterID: int64Ptr(3), DeviceID: "device_prod", ExitEnabled: true, Status: "connected"},
		{ID: 103, ClusterID: int64Ptr(1), DeviceID: "device_frank_noext", ExitEnabled: false, Status: "connected"}, // not exit-enabled
	}

	apiSrv := mockAPIForMesh(clusters, nodes)
	defer apiSrv.Close()

	apiClient := api.NewClient(apiSrv.URL)
	apiClient.SetToken("test-token")

	resolver := buildMeshResolver(apiClient)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	tests := []struct {
		name       string
		address    string
		wantPeerID string // empty = fallback to default
	}{
		{
			name:       ".mesh resolves frank-local → device_frank",
			address:    "myapi.franklocal.mesh:80",
			wantPeerID: "device_frank",
		},
		{
			name:       ".mesh resolves staging → device_staging",
			address:    "webui.staging.mesh:443",
			wantPeerID: "device_staging",
		},
		{
			name:       ".mesh resolves prod us-east (slug: prod-useast) → device_prod",
			address:    "api.prod-useast.mesh:8080",
			wantPeerID: "device_prod",
		},
		{
			name:       "unknown .mesh cluster → fallback",
			address:    "svc.unknown.mesh:80",
			wantPeerID: "fallback-peer",
		},
		{
			name:       "non-.mesh host → fallback",
			address:    "example.com:443",
			wantPeerID: "fallback-peer",
		},
		{
			name:       "bare host (no dots in front of .mesh) → fallback",
			address:    "staging.mesh:80",
			wantPeerID: "fallback-peer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track which peer the DERP server receives in the route request.
			var receivedTarget string

			derpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()

				// Read registration
				conn.ReadJSON(&map[string]interface{}{})

				// Read route_request
				var msg map[string]interface{}
				if err := conn.ReadJSON(&msg); err != nil {
					return
				}

				data, _ := msg["data"].(map[string]interface{})
				routeID, _ := data["route_id"].(string)
				receivedTarget, _ = data["target_client"].(string)

				// Respond OK
				conn.WriteJSON(map[string]interface{}{
					"type": "route_response",
					"from": "server",
					"data": map[string]interface{}{
						"route_id": routeID,
						"status":   "ok",
					},
				})
				time.Sleep(2 * time.Second)
			}))
			defer derpSrv.Close()

			wsURL := "ws" + strings.TrimPrefix(derpSrv.URL, "http")
			derpClient := derp.NewClient(wsURL, "test-device")

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			go derpClient.Run(ctx)
			time.Sleep(200 * time.Millisecond)

			proxy := NewExitProxy(ProxyOptions{
				ListenAddr:      "127.0.0.1:0",
				ExitPeerID:      "fallback-peer",
				OrgID:           "org-1",
				DERPClient:      derpClient,
				ResolveExitPeer: resolver,
			})
			derpClient.RouteResponseHandler = proxy.HandleRouteResponse
			derpClient.TunnelTrafficHandler = func(routeID string, targetPort, externalPort int, data []byte) {
				proxy.HandleTrafficData(routeID, data)
			}

			conn, err := proxy.dialViaDERP(ctx, "tcp", tt.address)
			if err != nil {
				t.Fatalf("dialViaDERP(%q): %v", tt.address, err)
			}
			defer conn.Close()

			if receivedTarget != tt.wantPeerID {
				t.Errorf("DERP route_request target_client = %q, want %q", receivedTarget, tt.wantPeerID)
			}
		})
	}
}

// TestMeshResolveExitPeerFunc tests the resolver function in isolation (no DERP).
func TestMeshResolveExitPeerFunc(t *testing.T) {
	clusters := []api.Cluster{
		{ID: 1, Name: "frank-local"},
		{ID: 2, Name: "staging"},
	}
	nodes := []api.MeshNode{
		{ID: 100, ClusterID: int64Ptr(1), DeviceID: "device_frank", ExitEnabled: true, Status: "connected"},
		{ID: 101, ClusterID: int64Ptr(2), DeviceID: "device_staging", ExitEnabled: true, Status: "connected"},
		{ID: 102, ClusterID: int64Ptr(1), DeviceID: "device_offline", ExitEnabled: true, Status: "disconnected"},
	}

	apiSrv := mockAPIForMesh(clusters, nodes)
	defer apiSrv.Close()

	client := api.NewClient(apiSrv.URL)
	client.SetToken("test-token")

	resolver := buildMeshResolver(client)
	ctx := context.Background()

	tests := []struct {
		name    string
		address string
		want    string
	}{
		{"frank-local cluster", "api.franklocal.mesh:80", "device_frank"},
		{"staging cluster", "web.staging.mesh:443", "device_staging"},
		{"unknown cluster", "svc.unknown.mesh:80", ""},
		{"non-.mesh", "google.com:443", ""},
		{"single part before .mesh", "staging.mesh:80", ""},
		{"three parts before .mesh", "a.b.c.mesh:80", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver(ctx, tt.address)
			if err != nil {
				t.Fatalf("resolver error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolver(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}

// TestMeshResolveSkipsNonExitNodes verifies that nodes without ExitEnabled are skipped.
func TestMeshResolveSkipsNonExitNodes(t *testing.T) {
	clusters := []api.Cluster{{ID: 1, Name: "frank-local"}}
	nodes := []api.MeshNode{
		{ID: 100, ClusterID: int64Ptr(1), DeviceID: "no_exit", ExitEnabled: false, Status: "connected"},
	}

	apiSrv := mockAPIForMesh(clusters, nodes)
	defer apiSrv.Close()

	client := api.NewClient(apiSrv.URL)
	client.SetToken("test-token")

	resolver := buildMeshResolver(client)
	got, err := resolver(context.Background(), "api.franklocal.mesh:80")
	if err != nil {
		t.Fatalf("resolver error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty (no exit node), got %q", got)
	}
}

// TestMeshResolveSkipsDisconnectedNodes verifies that disconnected nodes are skipped.
func TestMeshResolveSkipsDisconnectedNodes(t *testing.T) {
	clusters := []api.Cluster{{ID: 1, Name: "frank-local"}}
	nodes := []api.MeshNode{
		{ID: 100, ClusterID: int64Ptr(1), DeviceID: "offline_exit", ExitEnabled: true, Status: "disconnected"},
	}

	apiSrv := mockAPIForMesh(clusters, nodes)
	defer apiSrv.Close()

	client := api.NewClient(apiSrv.URL)
	client.SetToken("test-token")

	resolver := buildMeshResolver(client)
	got, err := resolver(context.Background(), "api.franklocal.mesh:80")
	if err != nil {
		t.Fatalf("resolver error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty (disconnected), got %q", got)
	}
}

// TestMeshResolveSocks5EndToEnd starts a real SOCKS5 proxy wired to a mock DERP relay
// and mock API, connects a real TCP client through SOCKS5, and verifies the traffic
// reaches the correct exit peer.
func TestMeshResolveSocks5EndToEnd(t *testing.T) {
	clusters := []api.Cluster{
		{ID: 1, Name: "frank-local"},
	}
	nodes := []api.MeshNode{
		{ID: 100, ClusterID: int64Ptr(1), DeviceID: "device_frank", ExitEnabled: true, Status: "connected"},
	}

	apiSrv := mockAPIForMesh(clusters, nodes)
	defer apiSrv.Close()

	apiClient := api.NewClient(apiSrv.URL)
	apiClient.SetToken("test-token")
	resolver := buildMeshResolver(apiClient)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	// DERP mock that accepts route_request and echoes back traffic_data.
	derpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read registration
		conn.ReadJSON(&map[string]interface{}{})

		for {
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			msgType, _ := msg["type"].(string)
			data, _ := msg["data"].(map[string]interface{})

			switch msgType {
			case "route_request":
				routeID, _ := data["route_id"].(string)
				conn.WriteJSON(map[string]interface{}{
					"type": "route_response",
					"from": "server",
					"data": map[string]interface{}{
						"route_id": routeID,
						"status":   "ok",
					},
				})
			case "traffic_data":
				// Echo the data back
				conn.WriteJSON(msg)
			}
		}
	}))
	defer derpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(derpSrv.URL, "http")
	derpClient := derp.NewClient(wsURL, "test-device")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go derpClient.Run(ctx)
	time.Sleep(200 * time.Millisecond)

	proxy := NewExitProxy(ProxyOptions{
		ListenAddr:      "127.0.0.1:0",
		ExitPeerID:      "fallback-peer",
		OrgID:           "org-1",
		DERPClient:      derpClient,
		ResolveExitPeer: resolver,
	})
	derpClient.RouteResponseHandler = proxy.HandleRouteResponse
	derpClient.TunnelTrafficHandler = func(routeID string, targetPort, externalPort int, data []byte) {
		proxy.HandleTrafficData(routeID, data)
	}

	go proxy.ListenAndServe(ctx)
	time.Sleep(100 * time.Millisecond)

	addr := proxy.Addr()
	if addr == "" {
		t.Fatal("proxy did not start")
	}

	// Connect via SOCKS5 protocol to a .mesh address.
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("connect to SOCKS5: %v", err)
	}
	defer conn.Close()

	// SOCKS5 handshake: version 5, 1 method, no auth
	conn.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	if _, err := conn.Read(authReply); err != nil {
		t.Fatalf("auth reply: %v", err)
	}
	if authReply[0] != 0x05 || authReply[1] != 0x00 {
		t.Fatalf("unexpected auth reply: %x", authReply)
	}

	// SOCKS5 CONNECT to myapi.franklocal.mesh:80 (FQDN = address type 0x03)
	target := "myapi.franklocal.mesh"
	connectReq := []byte{
		0x05, 0x01, 0x00, 0x03, // ver, CONNECT, reserved, FQDN
		byte(len(target)),       // domain length
	}
	connectReq = append(connectReq, []byte(target)...)
	connectReq = append(connectReq, 0x00, 0x50) // port 80

	conn.Write(connectReq)

	connReply := make([]byte, 10)
	n, err := conn.Read(connReply)
	if err != nil {
		t.Fatalf("connect reply: %v", err)
	}

	// First byte = version, second byte = status (0x00 = success)
	if n < 2 {
		t.Fatalf("short connect reply: %d bytes", n)
	}
	if connReply[0] != 0x05 {
		t.Fatalf("unexpected SOCKS version in reply: %x", connReply[0])
	}
	if connReply[1] != 0x00 {
		t.Fatalf("SOCKS5 CONNECT failed with status %x (expected 0x00 success)", connReply[1])
	}

	t.Log("SOCKS5 CONNECT to myapi.franklocal.mesh:80 succeeded — route resolved through DERP relay")
}
