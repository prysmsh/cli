package derp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNewClient(t *testing.T) {
	client := NewClient("wss://derp.example.com/derp", "device-123")

	if client.url != "wss://derp.example.com/derp" {
		t.Errorf("url = %q, want wss://derp.example.com/derp", client.url)
	}
	if client.deviceID != "device-123" {
		t.Errorf("deviceID = %q, want device-123", client.deviceID)
	}
	if client.capabilities == nil {
		t.Error("capabilities should not be nil")
	}
	if client.capabilities["platform"] != "cli" {
		t.Errorf("capabilities[platform] = %v, want cli", client.capabilities["platform"])
	}
}

func TestNewClientWithOptions(t *testing.T) {
	headers := make(http.Header)
	headers.Set("X-Custom", "value")

	caps := map[string]interface{}{
		"custom": "capability",
	}

	client := NewClient("wss://derp.example.com", "dev-1",
		WithHeaders(headers),
		WithCapabilities(caps),
		WithLogLevel(LogDebug),
		WithSessionToken("session-token-123"),
	)

	if client.headers.Get("X-Custom") != "value" {
		t.Error("Custom header not set")
	}
	if client.capabilities["custom"] != "capability" {
		t.Error("Custom capability not set")
	}
	if client.logLevel != LogDebug {
		t.Errorf("logLevel = %v, want LogDebug", client.logLevel)
	}
	if client.sessionToken != "session-token-123" {
		t.Errorf("sessionToken = %q, want session-token-123", client.sessionToken)
	}
}

func TestNewClientWithInsecure(t *testing.T) {
	client := NewClient("wss://derp.example.com", "dev-1",
		WithInsecure(true),
	)

	if client.dialer.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig should not be nil with insecure option")
	}
	if !client.dialer.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true")
	}
}

func TestNewClientWithDERPTunnelToken(t *testing.T) {
	client := NewClient("wss://derp.example.com", "dev-1",
		WithDERPTunnelToken("derp-jwt-token"),
	)
	if client.derpTunnelToken != "derp-jwt-token" {
		t.Errorf("derpTunnelToken = %q, want derp-jwt-token", client.derpTunnelToken)
	}
}

func TestNewClientWithTunnelTrafficHandler(t *testing.T) {
	var receivedRouteID string
	var receivedTargetPort int

	handler := func(routeID string, targetPort, externalPort int, data []byte) {
		receivedRouteID = routeID
		receivedTargetPort = targetPort
	}

	client := NewClient("wss://derp.example.com", "dev-1",
		WithTunnelTrafficHandler(handler),
	)

	if client.TunnelTrafficHandler == nil {
		t.Fatal("TunnelTrafficHandler should be set")
	}

	client.TunnelTrafficHandler("route_123", 5432, 30000, nil)
	if receivedRouteID != "route_123" {
		t.Errorf("receivedRouteID = %q, want route_123", receivedRouteID)
	}
	if receivedTargetPort != 5432 {
		t.Errorf("receivedTargetPort = %d, want 5432", receivedTargetPort)
	}
}

func TestSendRouteRequestWithoutConnection(t *testing.T) {
	client := NewClient("wss://derp.example.com", "dev-1")
	_, err := client.SendRouteRequest("1", "device_abc", 30000, 5432, "TCP")
	if err == nil {
		t.Fatal("expected error when sending without connection")
	}
	if !strings.Contains(err.Error(), "connection not established") {
		t.Errorf("expected connection error, got: %v", err)
	}
}

func TestClientRunWithoutDeviceID(t *testing.T) {
	client := NewClient("wss://derp.example.com", "")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := client.Run(ctx)
	if err == nil {
		t.Fatal("Expected error for empty device ID")
	}
	if !strings.Contains(err.Error(), "device id") {
		t.Errorf("Error should mention device id, got: %v", err)
	}
}

func TestClientClose(t *testing.T) {
	client := NewClient("wss://derp.example.com", "dev-1")

	// Close should not panic even without connection
	client.Close()
}

func TestGetString(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"string", "hello", "hello"},
		{"empty string", "", ""},
		{"nil", nil, ""},
		{"int", 123, ""},
		{"bool", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getString(tt.input)
			if got != tt.want {
				t.Errorf("getString(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		wantLen  int
		wantNil  bool
	}{
		{"slice", []interface{}{"a", "b"}, 2, false},
		{"empty slice", []interface{}{}, 0, false},
		{"nil", nil, 0, true},
		{"string", "not a slice", 0, true},
		{"int", 123, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSlice(tt.input)
			if tt.wantNil && got != nil {
				t.Errorf("getSlice(%v) = %v, want nil", tt.input, got)
			}
			if !tt.wantNil && len(got) != tt.wantLen {
				t.Errorf("getSlice(%v) len = %d, want %d", tt.input, len(got), tt.wantLen)
			}
		})
	}
}

func TestParseErrorPayload(t *testing.T) {
	tests := []struct {
		name       string
		data       interface{}
		wantCode   string
		wantDetail string
	}{
		{
			name:       "nil",
			data:       nil,
			wantCode:   "unknown",
			wantDetail: "",
		},
		{
			name: "map with error",
			data: map[string]interface{}{
				"error":  "auth_failed",
				"detail": "invalid token",
			},
			wantCode:   "auth_failed",
			wantDetail: "invalid token",
		},
		{
			name: "map without error",
			data: map[string]interface{}{
				"other": "value",
			},
			wantCode:   "unknown",
			wantDetail: "",
		},
		{
			name:       "byte slice json",
			data:       []byte(`{"error": "byte_error", "detail": "byte detail"}`),
			wantCode:   "byte_error",
			wantDetail: "byte detail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, detail := parseErrorPayload(tt.data)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", detail, tt.wantDetail)
			}
		})
	}
}

func TestSummarizePeer(t *testing.T) {
	peer := map[string]interface{}{
		"id":     "peer-1",
		"status": "online",
	}

	result := summarizePeer(peer)
	if result == "" {
		t.Error("summarizePeer returned empty string")
	}
	if !strings.Contains(result, "peer-1") {
		t.Errorf("summarizePeer should contain peer id, got: %s", result)
	}
}

func TestSummarizeMessage(t *testing.T) {
	msg := map[string]interface{}{
		"type":    "relay",
		"payload": "data",
	}

	result := summarizeMessage(msg)
	if result == "" {
		t.Error("summarizeMessage returned empty string")
	}
}

// Integration test with a mock WebSocket server
func TestClientRunWithMockServer(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	var receivedRegistration map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		// Read registration message
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		json.Unmarshal(msg, &receivedRegistration)

		// Send peer list
		conn.WriteJSON(map[string]interface{}{
			"type":  "peer_list",
			"peers": []interface{}{},
		})

		// Wait for context cancellation
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	client := NewClient(wsURL, "test-device-123",
		WithSessionToken("test-session"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Run in goroutine since it blocks
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Run(ctx)
	}()

	// Wait for timeout or error
	select {
	case err := <-errCh:
		if err != nil && err != context.DeadlineExceeded {
			// Connection errors are expected when context is cancelled
			t.Logf("Run returned: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("Test timed out")
	}

	client.Close()
}

func TestEventTypes(t *testing.T) {
	events := []EventType{
		EventPeerList,
		EventPeerJoined,
		EventPeerLeft,
		EventRelayMessage,
		EventServiceDiscovery,
		EventStatsUpdate,
		EventPong,
		EventError,
		EventUnknown,
	}

	for _, e := range events {
		if string(e) == "" {
			t.Errorf("EventType %v has empty string value", e)
		}
	}
}
