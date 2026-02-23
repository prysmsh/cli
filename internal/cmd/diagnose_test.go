package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/config"
	"github.com/prysmsh/cli/internal/session"
)

func TestDiagnoseNetworkJSON(t *testing.T) {
	store := session.NewStore(filepath.Join(t.TempDir(), "session.json"))
	if err := store.Save(&session.Session{
		Token:         "token-1",
		SessionID:     "sess-1",
		Email:         "user@example.com",
		DERPServerURL: "wss://localhost:3478",
		ExpiresAtUnix: time.Now().Add(1 * time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/profile"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"user": map[string]interface{}{"id": 1}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/connect/k8s/clusters"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"clusters": []interface{}{}})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token-1")

	prev := app
	app = &App{
		API:      client,
		Sessions: store,
		Config: &config.Config{
			DERPServerURL: "wss://localhost:3478",
		},
	}
	defer func() { app = prev }()

	cmd := newDiagnoseCommand()
	stdout, _, err := executeCommand(cmd, "network", "--output", "json")
	if err != nil {
		t.Fatalf("diagnose network failed: %v", err)
	}
	if !strings.Contains(stdout, "\"category\": \"network\"") {
		t.Fatalf("expected network category in output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "\"ok\": true") {
		t.Fatalf("expected ok=true in output, got:\n%s", stdout)
	}
}
