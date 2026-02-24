package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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

func TestSSHOnboardRequiresTarget(t *testing.T) {
	cmd := newSSHCommand()
	_, _, err := executeCommand(cmd, "onboard")
	if err == nil {
		t.Fatalf("expected onboarding command to fail without target")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
		t.Fatalf("expected missing target arg error, got: %v", err)
	}

	if strings.Contains(err.Error(), "required flag(s) \"reason\"") {
		t.Fatalf("ssh onboard should not be treated as ssh target, got: %v", err)
	}
}

func TestSSHOnboardDryRunCommand(t *testing.T) {
	cmd := newSSHCommand()
	stdout, _, err := executeCommand(cmd, "onboard", "root@example-host", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "ssh -t root@example-host prysm") {
		t.Fatalf("expected dry-run ssh command prefix, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "onboard docker") {
		t.Fatalf("expected onboard docker command, got:\n%s", stdout)
	}
}

func TestSSHOnboardDryRunCommandWithCollector(t *testing.T) {
	cmd := newSSHCommand()
	stdout, _, err := executeCommand(cmd, "onboard", "root@example-host", "--collector", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "ssh -t root@example-host prysm") {
		t.Fatalf("expected dry-run ssh command prefix, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "onboard docker --collector") {
		t.Fatalf("expected collector dry-run ssh command, got:\n%s", stdout)
	}
}

func TestSSHTargetHost(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "user and ipv4", target: "alessio@192.168.190.175", want: "192.168.190.175"},
		{name: "host and port", target: "example.com:2222", want: "example.com"},
		{name: "ipv6 with port", target: "root@[2001:db8::1]:2222", want: "2001:db8::1"},
		{name: "plain host", target: "db.internal", want: "db.internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sshTargetHost(tt.target)
			if got != tt.want {
				t.Fatalf("sshTargetHost(%q) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}
}

func TestIsHostKeyVerificationFailure(t *testing.T) {
	if !isHostKeyVerificationFailure("Host key verification failed.\n") {
		t.Fatal("expected host key verification error to be detected")
	}
	if isHostKeyVerificationFailure("permission denied (publickey)\n") {
		t.Fatal("expected non-host-key errors to be ignored")
	}
}

func TestIsRemotePrysmNotFound(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "zsh form", input: "zsh:1: command not found: prysm", want: true},
		{name: "bash form", input: "bash: prysm: command not found", want: true},
		{name: "other error", input: "permission denied (publickey)", want: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := isRemotePrysmNotFound(tt.input)
			if got != tt.want {
				t.Fatalf("isRemotePrysmNotFound(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSSHOnboardRemoteArgs(t *testing.T) {
	got := sshOnboardRemoteArgs("prysm", true, "tok_123")
	want := []string{"prysm", "--token", "tok_123", "onboard", "docker", "--collector"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("sshOnboardRemoteArgs() = %v, want %v", got, want)
	}

	gotNoToken := sshOnboardRemoteArgs("prysm", false, "")
	wantNoToken := []string{"prysm", "onboard", "docker"}
	if strings.Join(gotNoToken, " ") != strings.Join(wantNoToken, " ") {
		t.Fatalf("sshOnboardRemoteArgs() without token = %v, want %v", gotNoToken, wantNoToken)
	}
}

func TestRedactSSHOnboardToken(t *testing.T) {
	in := []string{"-t", "host", "prysm", "--token", "secret", "onboard", "docker"}
	got := redactSSHOnboardToken(in)
	if strings.Join(got, " ") != "-t host prysm --token <redacted> onboard docker" {
		t.Fatalf("redactSSHOnboardToken() = %v", got)
	}
}
