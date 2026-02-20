package derp

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestHandleMessage_PeerList(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleMessage(map[string]interface{}{
		"type":  "peer_list",
		"peers": []interface{}{map[string]interface{}{"id": "p1"}, map[string]interface{}{"id": "p2"}},
	})
}

func TestHandleMessage_PeerJoined(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleMessage(map[string]interface{}{
		"type": "peer_joined",
		"peer": map[string]interface{}{"id": "peer-1", "status": "online"},
	})
}

func TestHandleMessage_PeerLeft(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleMessage(map[string]interface{}{
		"type":    "peer_left",
		"peer_id": "peer-1",
	})
}

func TestHandleMessage_ServiceDiscovery(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleMessage(map[string]interface{}{"type": "service_discovery"})
}

func TestHandleMessage_RelayMessage(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleMessage(map[string]interface{}{
		"type":    "relay_message",
		"message": map[string]interface{}{"payload": "data"},
	})
}

func TestHandleMessage_StatsUpdate(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleMessage(map[string]interface{}{"type": "stats_update"})
}

func TestHandleMessage_Pong(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1", WithLogLevel(LogDebug))
	c.handleMessage(map[string]interface{}{"type": "pong"})
}

func TestHandleMessage_Error(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleMessage(map[string]interface{}{
		"type": "error",
		"data": map[string]interface{}{"error": "auth_failed", "detail": "invalid token"},
	})
	c.handleMessage(map[string]interface{}{
		"type": "error",
		"data": map[string]interface{}{"error": "unknown"},
	})
}

func TestHandleMessage_ErrorWithByteSlice(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"error": "byte_err", "detail": "byte detail"})
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleMessage(map[string]interface{}{"type": "error", "data": payload})
}

func TestHandleMessage_ErrorWithBase64String(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"error": "b64_err", "detail": "b64 detail"})
	b64 := base64.StdEncoding.EncodeToString(payload)
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleMessage(map[string]interface{}{"type": "error", "data": b64})
}

func TestHandleMessage_RouteSetup(t *testing.T) {
	var routeID string
	c := NewClient("wss://derp.example.com", "dev-1", WithTunnelTrafficHandler(
		func(rid string, _, _ int, _ []byte) { routeID = rid },
	))
	c.handleMessage(map[string]interface{}{
		"type": "route_setup",
		"from": "server",
		"data": map[string]interface{}{
			"route_id": "r1",
			"target_port": 5432,
			"external_port": 30000,
		},
	})
	if routeID != "r1" {
		t.Errorf("routeID = %q, want r1", routeID)
	}
}

func TestHandleMessage_RouteSetupNoHandler(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1", WithLogLevel(LogDebug))
	c.handleMessage(map[string]interface{}{
		"type": "route_setup",
		"from": "server",
		"data": map[string]interface{}{
			"route_id": "r1",
			"target_port": 5432,
			"external_port": 30000,
		},
	})
}

func TestHandleMessage_RouteResponse(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1", WithLogLevel(LogDebug))
	c.handleMessage(map[string]interface{}{"type": "route_response"})
}

func TestHandleMessage_TrafficData(t *testing.T) {
	var received []byte
	c := NewClient("wss://derp.example.com", "dev-1", WithTunnelTrafficHandler(
		func(_ string, _, _ int, data []byte) { received = data },
	))
	c.handleMessage(map[string]interface{}{
		"type": "traffic_data",
		"data": map[string]interface{}{"route_id": "r1", "data": []byte("payload")},
	})
	if string(received) != "payload" {
		t.Errorf("data = %q", received)
	}
}

func TestHandleMessage_TrafficDataString(t *testing.T) {
	payload := json.RawMessage(`{"route_id":"r1","data":"aGVsbG8="}`)
	c := NewClient("wss://derp.example.com", "dev-1", WithTunnelTrafficHandler(
		func(_ string, _, _ int, data []byte) { _ = data },
	))
	c.handleMessage(map[string]interface{}{"type": "traffic_data", "data": payload})
}

func TestHandleMessage_Unknown(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1", WithLogLevel(LogDebug))
	c.handleMessage(map[string]interface{}{"type": "unknown_type", "x": 1})
}

func TestHandleRouteSetup_NilData(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleRouteSetup(map[string]interface{}{"type": "route_setup"})
}

func TestHandleRouteSetup_InvalidJSON(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1", WithLogLevel(LogDebug))
	c.handleRouteSetup(map[string]interface{}{"type": "route_setup", "data": "not valid json {"})
}

func TestHandleRouteSetup_DataAsString(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1", WithLogLevel(LogDebug))
	c.handleRouteSetup(map[string]interface{}{
		"type": "route_setup",
		"from": "srv",
		"data": `{"route_id":"r2","target_port":22,"external_port":30001}`,
	})
}

func TestHandleTrafficData_NilData(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1")
	c.handleTrafficData(map[string]interface{}{"type": "traffic_data"})
}

func TestHandleTrafficData_InvalidJSON(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1", WithLogLevel(LogDebug))
	c.handleTrafficData(map[string]interface{}{"type": "traffic_data", "data": "not json"})
}

func TestHandleTrafficData_DataAsString(t *testing.T) {
	c := NewClient("wss://derp.example.com", "dev-1", WithTunnelTrafficHandler(
		func(_ string, _, _ int, data []byte) { _ = data },
	))
	c.handleTrafficData(map[string]interface{}{
		"type": "traffic_data",
		"data": `{"route_id":"r1","data":"aGk="}`, // "hi" base64
	})
}

func TestParseErrorPayload_InvalidBase64(t *testing.T) {
	// Invalid base64 decodes to empty/failed; Unmarshal fails so we get unknown + string(raw)
	code, _ := parseErrorPayload("!!!not-base64!!!")
	if code != "unknown" {
		t.Errorf("code = %q", code)
	}
}

func TestSummarizePeer_MarshalError(t *testing.T) {
	// Channel cannot be JSON marshaled
	got := summarizePeer(map[string]interface{}{"ch": make(chan int)})
	if got == "" {
		t.Error("summarizePeer should return fallback when marshal fails")
	}
}

func TestSummarizeMessage_MarshalError(t *testing.T) {
	got := summarizeMessage(map[string]interface{}{"ch": make(chan int)})
	if got == "" {
		t.Error("summarizeMessage should return fallback when marshal fails")
	}
}

type testStringer string

func (s testStringer) String() string { return string(s) }

func TestGetStringStringer(t *testing.T) {
	got := getString(testStringer("stringer"))
	if got != "stringer" {
		t.Errorf("getString(Stringer) = %q, want stringer", got)
	}
}

func TestParseErrorPayloadString(t *testing.T) {
	raw := []byte(`{"error":"s_err","detail":"s detail"}`)
	b64 := base64.StdEncoding.EncodeToString(raw)
	code, detail := parseErrorPayload(b64)
	if code != "s_err" || detail != "s detail" {
		t.Errorf("code=%q detail=%q", code, detail)
	}
}
