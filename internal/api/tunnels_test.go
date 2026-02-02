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
