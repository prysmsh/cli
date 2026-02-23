package exit

import (
	"context"

	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prysmsh/cli/internal/derp"
)

func TestHandleRouteResponse(t *testing.T) {
	p := &ExitProxy{
		routes:  make(map[string]*derpConn),
		pending: make(map[string]chan string),
	}

	t.Run("delivers status to pending channel", func(t *testing.T) {
		ch := make(chan string, 1)
		p.pendingMu.Lock()
		p.pending["route_1"] = ch
		p.pendingMu.Unlock()

		p.HandleRouteResponse("route_1", "ok")

		select {
		case status := <-ch:
			if status != "ok" {
				t.Errorf("status = %q, want ok", status)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Channel should be removed from pending.
		p.pendingMu.Lock()
		_, exists := p.pending["route_1"]
		p.pendingMu.Unlock()
		if exists {
			t.Error("pending entry should be removed after delivery")
		}
	})

	t.Run("no-op for unknown route", func(t *testing.T) {
		// Should not panic.
		p.HandleRouteResponse("unknown_route", "ok")
	})
}

func TestHandleTrafficData(t *testing.T) {
	p := &ExitProxy{
		routes:  make(map[string]*derpConn),
		pending: make(map[string]chan string),
	}

	t.Run("nil data is ignored", func(t *testing.T) {
		p.HandleTrafficData("route_1", nil)
		// No panic.
	})

	t.Run("feeds data to derpConn", func(t *testing.T) {
		dc := &derpConn{
			routeID: "route_2",
			inbound: make(chan []byte, 256),
		}
		p.mu.Lock()
		p.routes["route_2"] = dc
		p.mu.Unlock()

		payload := []byte("hello from relay")
		p.HandleTrafficData("route_2", payload)

		select {
		case data := <-dc.inbound:
			if string(data) != "hello from relay" {
				t.Errorf("data = %q, want %q", data, "hello from relay")
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for data")
		}
	})

	t.Run("unknown route is silently ignored", func(t *testing.T) {
		p.HandleTrafficData("nonexistent", []byte("data"))
		// No panic.
	})
}

func TestDialViaDERP(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	t.Run("resolves exit peer and returns derpConn on success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			// Read registration message.
			conn.ReadJSON(&map[string]interface{}{})

			// Read route_request.
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			// Extract route_id from the request.
			data, _ := msg["data"].(map[string]interface{})
			routeID, _ := data["route_id"].(string)

			// Send route_response.
			conn.WriteJSON(map[string]interface{}{
				"type": "route_response",
				"from": "server",
				"data": map[string]interface{}{
					"route_id": routeID,
					"status":   "ok",
				},
			})

			// Keep connection alive for test.
			time.Sleep(2 * time.Second)
		}))
		defer srv.Close()

		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
		client := derp.NewClient(wsURL, "test-device")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go client.Run(ctx)
		time.Sleep(200 * time.Millisecond) // wait for connection

		resolvedPeer := ""
		proxy := NewExitProxy(ProxyOptions{
			ListenAddr: "127.0.0.1:0",
			ExitPeerID: "fallback-peer",
			OrgID:      "org-1",
			DERPClient: client,
			ResolveExitPeer: func(ctx context.Context, addr string) (string, error) {
				resolvedPeer = addr
				return "exit-peer-1", nil
			},
		})

		// Wire up handlers like mesh connect does.
		client.RouteResponseHandler = proxy.HandleRouteResponse
		client.TunnelTrafficHandler = func(routeID string, targetPort, externalPort int, data []byte) {
			proxy.HandleTrafficData(routeID, data)
		}

		conn, err := proxy.dialViaDERP(ctx, "tcp", "apifrank.frank.mesh:80")
		if err != nil {
			t.Fatalf("dialViaDERP: %v", err)
		}
		defer conn.Close()

		if resolvedPeer != "apifrank.frank.mesh:80" {
			t.Errorf("ResolveExitPeer called with %q, want %q", resolvedPeer, "apifrank.frank.mesh:80")
		}

		// Verify we got a derpConn.
		dc, ok := conn.(*derpConn)
		if !ok {
			t.Fatalf("expected *derpConn, got %T", conn)
		}
		if dc.routeID == "" {
			t.Error("derpConn routeID should not be empty")
		}
	})

	t.Run("timeout when no response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			// Read registration but never respond to route_request.
			conn.ReadJSON(&map[string]interface{}{})
			conn.ReadJSON(&map[string]interface{}{})
			time.Sleep(30 * time.Second)
		}))
		defer srv.Close()

		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
		client := derp.NewClient(wsURL, "test-device-timeout")

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		go client.Run(ctx)
		time.Sleep(200 * time.Millisecond)

		proxy := NewExitProxy(ProxyOptions{
			ListenAddr: "127.0.0.1:0",
			ExitPeerID: "exit-peer-1",
			OrgID:      "org-1",
			DERPClient: client,
		})

		// dialViaDERP has a 15s internal timeout.
		_, err := proxy.dialViaDERP(ctx, "tcp", "example.com:443")
		if err == nil {
			t.Fatal("expected timeout error")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Errorf("error = %q, want timeout", err)
		}
	})

	t.Run("error response from relay", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			conn.ReadJSON(&map[string]interface{}{})
			var msg map[string]interface{}
			conn.ReadJSON(&msg)

			data, _ := msg["data"].(map[string]interface{})
			routeID, _ := data["route_id"].(string)

			conn.WriteJSON(map[string]interface{}{
				"type": "route_response",
				"from": "server",
				"data": map[string]interface{}{
					"route_id": routeID,
					"status":   "failed",
					"error":    "mesh route not found",
				},
			})
			time.Sleep(2 * time.Second)
		}))
		defer srv.Close()

		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
		client := derp.NewClient(wsURL, "test-device-fail")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go client.Run(ctx)
		time.Sleep(200 * time.Millisecond)

		proxy := NewExitProxy(ProxyOptions{
			ListenAddr: "127.0.0.1:0",
			ExitPeerID: "exit-peer-1",
			OrgID:      "org-1",
			DERPClient: client,
		})
		client.RouteResponseHandler = proxy.HandleRouteResponse

		_, err := proxy.dialViaDERP(ctx, "tcp", "unknown.unknown.mesh:80")
		if err == nil {
			t.Fatal("expected error for rejected route")
		}
		if !strings.Contains(err.Error(), "rejected") {
			t.Errorf("error = %q, want rejection", err)
		}
	})
}
