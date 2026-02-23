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

func TestListAccessSessions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/sessions") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"sessions": []map[string]interface{}{
				{
					"session_id": "sess_1",
					"status":     "active",
					"type":       "ssh",
				},
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	sessions, err := client.ListAccessSessions(context.Background(), api.AccessSessionListOptions{
		Status: "active",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListAccessSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if sessions[0].Identifier() != "sess_1" {
		t.Fatalf("Identifier() = %q, want sess_1", sessions[0].Identifier())
	}
}

func TestReplayAccessSessionFallbackToExport(t *testing.T) {
	var replayCalled bool
	var exportCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sessions/sess_1/replay"):
			replayCalled = true
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sessions/sess_1/export"):
			exportCalled = true
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"replay": map[string]interface{}{
					"session_id": "sess_1",
					"format":     "events",
					"events": []map[string]interface{}{
						{"type": "command", "message": "ls -la"},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	replay, err := client.ReplayAccessSession(context.Background(), "sess_1", "events")
	if err != nil {
		t.Fatalf("ReplayAccessSession failed: %v", err)
	}
	if !replayCalled {
		t.Fatalf("replay endpoint should be called first")
	}
	if !exportCalled {
		t.Fatalf("export endpoint should be called as fallback")
	}
	if replay.SessionID != "sess_1" {
		t.Fatalf("session_id = %q, want sess_1", replay.SessionID)
	}
	if len(replay.Events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(replay.Events))
	}
}
