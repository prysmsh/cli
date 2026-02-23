package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/prysmsh/cli/internal/plugin"
	onboardplugin "github.com/prysmsh/cli/plugins/onboard"
)

func TestRequestCreateRequiresReason(t *testing.T) {
	cmd := newRequestCommand()
	_, _, err := executeCommand(cmd, "create", "server-prod", "--expires-in", "1h")
	if err == nil {
		t.Fatalf("expected error when --reason is missing")
	}
	if !strings.Contains(err.Error(), "required flag(s) \"reason\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRequestCreateJSONOutput(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/access/requests") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request": map[string]interface{}{
					"id":            "req_100",
					"status":        "pending",
					"resource":      "server-prod",
					"resource_type": "ssh",
					"reason":        "breakfix",
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	})

	srv, reset := setupTestApp(t, handler)
	defer srv.Close()
	defer reset()

	cmd := newRequestCommand()
	stdout, _, err := executeCommand(cmd,
		"create", "server-prod",
		"--reason", "breakfix",
		"--expires-in", "1h",
		"--output", "json",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "\"status\": \"pending\"") {
		t.Fatalf("expected JSON output with pending status, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "\"resource\": \"server-prod\"") {
		t.Fatalf("expected JSON output with resource, got:\n%s", stdout)
	}
}

func TestConnectSSHDryRun(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/connect/ssh") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"session": map[string]interface{}{
					"session_id": "ssh_sess_1",
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
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	})

	srv, reset := setupTestApp(t, handler)
	defer srv.Close()
	defer reset()

	cmd := newConnectCommand()
	stdout, _, err := executeCommand(cmd,
		"ssh", "alice@db.internal",
		"--reason", "incident-response",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Policy checks passed (dry-run)") {
		t.Fatalf("expected dry-run success message, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "ssh -p 2222 alice@db.internal") {
		t.Fatalf("expected rendered ssh command, got:\n%s", stdout)
	}
}

func TestSSHCommandDryRun(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/connect/ssh") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"session": map[string]interface{}{
					"session_id": "ssh_sess_2",
					"status":     "active",
				},
				"connection": map[string]interface{}{
					"host": "ops.internal",
					"user": "alice",
					"port": 22,
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	})

	srv, reset := setupTestApp(t, handler)
	defer srv.Close()
	defer reset()

	cmd := newSSHCommand()
	stdout, _, err := executeCommand(cmd,
		"alice@ops.internal",
		"--reason", "ops-check",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Policy checks passed (dry-run)") {
		t.Fatalf("expected dry-run success message, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "ssh -p 22 alice@ops.internal") {
		t.Fatalf("expected rendered ssh command, got:\n%s", stdout)
	}
}

type sshOnboardHostStub struct{}

func (s *sshOnboardHostStub) GetAuthContext(context.Context) (*plugin.AuthContext, error) {
	return nil, errors.New("stub auth error")
}
func (s *sshOnboardHostStub) APIRequest(context.Context, string, string, []byte) (int, []byte, error) {
	return 0, nil, errors.New("unexpected API request")
}
func (s *sshOnboardHostStub) GetConfig(context.Context) (*plugin.HostConfig, error) {
	return nil, errors.New("unexpected GetConfig")
}
func (s *sshOnboardHostStub) Log(context.Context, plugin.LogLevel, string) error { return nil }
func (s *sshOnboardHostStub) PromptInput(context.Context, string, bool) (string, error) {
	return "", errors.New("unexpected PromptInput")
}
func (s *sshOnboardHostStub) PromptConfirm(context.Context, string) (bool, error) {
	return false, errors.New("unexpected PromptConfirm")
}

func TestSSHOnboardDispatchesToOnboardPlugin(t *testing.T) {
	prev := onboardPlugin
	onboardPlugin = onboardplugin.New(&sshOnboardHostStub{})
	t.Cleanup(func() { onboardPlugin = prev })

	cmd := newSSHCommand()
	_, _, err := executeCommand(cmd, "onboard")
	if err == nil {
		t.Fatalf("expected onboarding command to return stub error")
	}
	if !strings.Contains(err.Error(), "stub auth error") {
		t.Fatalf("expected onboard plugin error, got: %v", err)
	}
	if strings.Contains(err.Error(), "required flag(s) \"reason\"") {
		t.Fatalf("ssh onboard should not be treated as ssh target, got: %v", err)
	}
}
