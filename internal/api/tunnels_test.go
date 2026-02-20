package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/warp-run/prysm-cli/internal/api"
)

func TestCreateTunnel(t *testing.T) {
	var captured struct {
		method string
		path   string
		body   map[string]interface{}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path

		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v1/tunnels" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		_ = json.NewDecoder(r.Body).Decode(&captured.body)

		payload := map[string]any{
			"tunnel": map[string]any{
				"id":               1,
				"name":             "postgres",
				"organization_id":  1,
				"target_device_id": "cli-abc123",
				"port":             5432,
				"external_port":    30000,
				"protocol":         "tcp",
				"status":           "active",
				"external_url":     "derp:30000",
				"created_at":       "2024-01-01T00:00:00Z",
				"updated_at":       "2024-01-01T00:00:00Z",
			},
			"message": "Tunnel created successfully",
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("test-token")

	tunnel, err := client.CreateTunnel(context.Background(), api.TunnelCreateRequest{
		Port:           5432,
		Name:           "postgres",
		TargetDeviceID: "cli-abc123",
		Protocol:       "tcp",
	})
	if err != nil {
		t.Fatalf("CreateTunnel error: %v", err)
	}
	if tunnel.ID != 1 {
		t.Errorf("tunnel ID = %d, want 1", tunnel.ID)
	}
	if tunnel.Port != 5432 {
		t.Errorf("tunnel port = %d, want 5432", tunnel.Port)
	}
	if tunnel.TargetDeviceID != "cli-abc123" {
		t.Errorf("target_device_id = %q, want cli-abc123", tunnel.TargetDeviceID)
	}
	if captured.body["port"].(float64) != 5432 {
		t.Errorf("body port = %v, want 5432", captured.body["port"])
	}
}

func TestListTunnels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tunnels" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		payload := map[string]any{
			"tunnels": []map[string]any{
				{
					"id":               1,
					"target_device_id": "cli-abc",
					"port":             5432,
					"external_port":    30000,
					"status":           "active",
				},
			},
			"total": 1,
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	tunnels, err := client.ListTunnels(context.Background(), "cli-abc")
	if err != nil {
		t.Fatalf("ListTunnels error: %v", err)
	}
	if len(tunnels) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(tunnels))
	}
	if tunnels[0].TargetDeviceID != "cli-abc" {
		t.Errorf("target_device_id = %q, want cli-abc", tunnels[0].TargetDeviceID)
	}
}

func TestCreatePublicTunnel(t *testing.T) {
	var captured map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)

		payload := map[string]any{
			"tunnel": map[string]any{
				"id":               2,
				"name":             "webapp",
				"organization_id":  1,
				"target_device_id": "cli-xyz789",
				"port":             8080,
				"external_port":    30001,
				"protocol":         "tcp",
				"status":           "active",
				"external_url":     "https://a1b2c3d4.tunnel.prysm.sh",
				"is_public":        true,
				"public_subdomain": "a1b2c3d4",
				"created_at":       "2024-01-01T00:00:00Z",
				"updated_at":       "2024-01-01T00:00:00Z",
			},
			"message": "Tunnel created successfully",
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("test-token")

	tunnel, err := client.CreateTunnel(context.Background(), api.TunnelCreateRequest{
		Port:           8080,
		Name:           "webapp",
		TargetDeviceID: "cli-xyz789",
		Protocol:       "tcp",
		IsPublic:       true,
	})
	if err != nil {
		t.Fatalf("CreateTunnel error: %v", err)
	}

	// Verify is_public was sent in the request body
	if v, ok := captured["is_public"].(bool); !ok || !v {
		t.Errorf("request body is_public = %v, want true", captured["is_public"])
	}

	// Verify response fields
	if tunnel.ID != 2 {
		t.Errorf("tunnel ID = %d, want 2", tunnel.ID)
	}
	if !tunnel.IsPublic {
		t.Error("tunnel.IsPublic = false, want true")
	}
	if tunnel.PublicSubdomain != "a1b2c3d4" {
		t.Errorf("tunnel.PublicSubdomain = %q, want a1b2c3d4", tunnel.PublicSubdomain)
	}
	if tunnel.ExternalURL != "https://a1b2c3d4.tunnel.prysm.sh" {
		t.Errorf("tunnel.ExternalURL = %q, want https://a1b2c3d4.tunnel.prysm.sh", tunnel.ExternalURL)
	}
}

func TestListTunnelsPublicFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"tunnels": []map[string]any{
				{
					"id":               1,
					"target_device_id": "cli-abc",
					"port":             8080,
					"external_port":    30000,
					"status":           "active",
					"is_public":        true,
					"public_subdomain": "deadbeef",
					"external_url":     "https://deadbeef.tunnel.prysm.sh",
				},
				{
					"id":               2,
					"target_device_id": "cli-def",
					"port":             5432,
					"external_port":    30001,
					"status":           "active",
					"is_public":        false,
				},
			},
			"total": 2,
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	tunnels, err := client.ListTunnels(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTunnels error: %v", err)
	}
	if len(tunnels) != 2 {
		t.Fatalf("expected 2 tunnels, got %d", len(tunnels))
	}

	if !tunnels[0].IsPublic {
		t.Error("tunnels[0].IsPublic = false, want true")
	}
	if tunnels[0].PublicSubdomain != "deadbeef" {
		t.Errorf("tunnels[0].PublicSubdomain = %q, want deadbeef", tunnels[0].PublicSubdomain)
	}
	if tunnels[1].IsPublic {
		t.Error("tunnels[1].IsPublic = true, want false")
	}
}

func TestDeleteTunnel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v1/tunnels/1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Tunnel deleted successfully"})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	err := client.DeleteTunnel(context.Background(), 1)
	if err != nil {
		t.Fatalf("DeleteTunnel error: %v", err)
	}
}
