package onboard

import (
	"context"
	"fmt"
	"testing"

	"github.com/prysmsh/cli/internal/plugin"
)

// mockHost implements plugin.HostServices for testing.
type mockHost struct {
	authCtx    *plugin.AuthContext
	authErr    error
	apiStatus  int
	apiBody    []byte
	apiErr     error
	config     *plugin.HostConfig
	configErr  error
	logs       []string
	promptVal  string
	promptErr  error
	confirmVal bool
	confirmErr error
}

func (m *mockHost) GetAuthContext(_ context.Context) (*plugin.AuthContext, error) {
	return m.authCtx, m.authErr
}
func (m *mockHost) APIRequest(_ context.Context, method, endpoint string, body []byte) (int, []byte, error) {
	return m.apiStatus, m.apiBody, m.apiErr
}
func (m *mockHost) GetConfig(_ context.Context) (*plugin.HostConfig, error) {
	return m.config, m.configErr
}
func (m *mockHost) Log(_ context.Context, level plugin.LogLevel, message string) error {
	m.logs = append(m.logs, message)
	return nil
}
func (m *mockHost) PromptInput(_ context.Context, label string, isSecret bool) (string, error) {
	return m.promptVal, m.promptErr
}
func (m *mockHost) PromptConfirm(_ context.Context, label string) (bool, error) {
	return m.confirmVal, m.confirmErr
}

func TestNew(t *testing.T) {
	h := &mockHost{}
	p := New(h)
	if p == nil {
		t.Fatal("New returned nil")
	}
}

func TestNew_NilHost(t *testing.T) {
	p := New(nil)
	if p == nil {
		t.Fatal("New(nil) returned nil")
	}
}

func TestSetHost(t *testing.T) {
	p := New(nil)
	h := &mockHost{}
	p.SetHost(h)
	if p.host != h {
		t.Error("SetHost did not update host")
	}
}

func TestManifest(t *testing.T) {
	p := New(nil)
	m := p.Manifest()
	if m.Name != "onboard" {
		t.Errorf("Name = %q, want %q", m.Name, "onboard")
	}
	if m.Version != "0.1.0" {
		t.Errorf("Version = %q", m.Version)
	}
	if len(m.Commands) != 1 {
		t.Fatalf("Commands count = %d, want 1", len(m.Commands))
	}
	cmd := m.Commands[0]
	if cmd.Name != "onboard" {
		t.Errorf("Commands[0].Name = %q", cmd.Name)
	}
	if len(cmd.Subcommands) != 4 {
		t.Fatalf("Subcommands count = %d, want 4", len(cmd.Subcommands))
	}
	names := make(map[string]bool)
	for _, sub := range cmd.Subcommands {
		names[sub.Name] = true
	}
	for _, want := range []string{"kube", "docker", "collector", "docker-compose"} {
		if !names[want] {
			t.Errorf("missing subcommand %q", want)
		}
	}
}

func TestManifest_KubeDisableFlagParsing(t *testing.T) {
	p := New(nil)
	m := p.Manifest()
	for _, sub := range m.Commands[0].Subcommands {
		if sub.Name == "kube" && !sub.DisableFlagParsing {
			t.Error("kube subcommand should have DisableFlagParsing=true")
		}
		if sub.Name == "collector" && !sub.DisableFlagParsing {
			t.Error("collector subcommand should have DisableFlagParsing=true")
		}
	}
}

func TestManifest_DockerComposeHidden(t *testing.T) {
	p := New(nil)
	m := p.Manifest()
	for _, sub := range m.Commands[0].Subcommands {
		if sub.Name == "docker-compose" && !sub.Hidden {
			t.Error("docker-compose subcommand should be hidden")
		}
	}
}

func TestExecute_NoArgs_ShowsMenu(t *testing.T) {
	h := &mockHost{
		promptVal: "invalid",
		promptErr: nil,
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{Args: nil})
	if resp.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1 (invalid choice)", resp.ExitCode)
	}
}

func TestExecute_UnknownSubcommand_ShowsMenu(t *testing.T) {
	h := &mockHost{
		promptVal: "invalid",
		promptErr: nil,
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{Args: []string{"unknown"}})
	if resp.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", resp.ExitCode)
	}
}

func TestExecute_MenuPromptError(t *testing.T) {
	h := &mockHost{
		promptErr: fmt.Errorf("stdin closed"),
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{Args: nil})
	if resp.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", resp.ExitCode)
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

func TestParseCollectorFlags_NoFlags(t *testing.T) {
	flags, err := parseCollectorFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags != nil {
		t.Error("expected nil flags (interactive mode)")
	}
}

func TestParseCollectorFlags_EmptyArgs(t *testing.T) {
	flags, err := parseCollectorFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags != nil {
		t.Error("expected nil (no --cluster = interactive)")
	}
}

func TestParseCollectorFlags_WithCluster(t *testing.T) {
	flags, err := parseCollectorFlags([]string{"--cluster", "my-cluster"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags == nil {
		t.Fatal("expected non-nil flags")
	}
	if flags.cluster != "my-cluster" {
		t.Errorf("cluster = %q, want %q", flags.cluster, "my-cluster")
	}
	if flags.namespace != "prysm-system" {
		t.Errorf("namespace = %q, want default %q", flags.namespace, "prysm-system")
	}
}

func TestParseCollectorFlags_AllFlags(t *testing.T) {
	args := []string{
		"--cluster", "prod",
		"--namespace", "custom-ns",
		"--kube-context", "prod-ctx",
		"--chart", "/path/to/chart",
		"--compose-file", "docker-compose.yml",
	}
	flags, err := parseCollectorFlags(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags == nil {
		t.Fatal("expected non-nil flags")
	}
	if flags.cluster != "prod" {
		t.Errorf("cluster = %q", flags.cluster)
	}
	if flags.namespace != "custom-ns" {
		t.Errorf("namespace = %q", flags.namespace)
	}
	if flags.kubeCtx != "prod-ctx" {
		t.Errorf("kubeCtx = %q", flags.kubeCtx)
	}
	if flags.chart != "/path/to/chart" {
		t.Errorf("chart = %q", flags.chart)
	}
	if flags.composeFile != "docker-compose.yml" {
		t.Errorf("composeFile = %q", flags.composeFile)
	}
}

func TestParseCollectorFlags_InvalidFlag(t *testing.T) {
	_, err := parseCollectorFlags([]string{"--invalid-flag"})
	if err == nil {
		t.Error("expected error for invalid flag")
	}
}
