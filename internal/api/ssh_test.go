package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prysmsh/cli/internal/api"
)

func TestConnectSSH(t *testing.T) {
	var payload map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/connect/ssh") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"session": map[string]interface{}{
				"session_id": "ssh_123",
				"status":     "active",
			},
			"connection": map[string]interface{}{
				"host": "db.internal",
				"user": "alice",
				"port": 2222,
			},
			"policy_checks": map[string]interface{}{
				"rbac": "pass",
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	resp, err := client.ConnectSSH(context.Background(), api.SSHConnectRequest{
		Target:  "alice@db.internal",
		Reason:  "breakfix",
		Port:    2222,
		Command: []string{"uptime"},
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("ConnectSSH failed: %v", err)
	}
	if payload["target"] != "alice@db.internal" {
		t.Fatalf("target = %v, want alice@db.internal", payload["target"])
	}
	if payload["reason"] != "breakfix" {
		t.Fatalf("reason = %v, want breakfix", payload["reason"])
	}
	if payload["dry_run"] != true {
		t.Fatalf("dry_run = %v, want true", payload["dry_run"])
	}
	if resp.Session.SessionID != "ssh_123" {
		t.Fatalf("session_id = %q, want ssh_123", resp.Session.SessionID)
	}
	if resp.Connection.Host != "db.internal" {
		t.Fatalf("host = %q, want db.internal", resp.Connection.Host)
	}
	if resp.Connection.Port != 2222 {
		t.Fatalf("port = %d, want 2222", resp.Connection.Port)
	}
}
