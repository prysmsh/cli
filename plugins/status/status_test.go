package status

import (
	"context"
	"encoding/json"
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
		t.Error("SetHost did not set host")
	}
}

func TestManifest(t *testing.T) {
	p := New(nil)
	m := p.Manifest()
	if m.Name != "status" {
		t.Errorf("Name = %q, want %q", m.Name, "status")
	}
	if m.Version != "0.1.0" {
		t.Errorf("Version = %q", m.Version)
	}
	if len(m.Commands) != 1 {
		t.Fatalf("Commands count = %d, want 1", len(m.Commands))
	}
	if m.Commands[0].Name != "status" {
		t.Errorf("Commands[0].Name = %q", m.Commands[0].Name)
	}
}

func TestExecute_NotLoggedIn(t *testing.T) {
	h := &mockHost{
		authErr: fmt.Errorf("not logged in"),
		config:  &plugin.HostConfig{APIBaseURL: "https://api.example.com"},
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{})
	if resp.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", resp.ExitCode)
	}
	if resp.Error != "not logged in" {
		t.Errorf("Error = %q", resp.Error)
	}
}

func TestExecute_NotLoggedIn_JSON(t *testing.T) {
	h := &mockHost{
		authErr: fmt.Errorf("not logged in"),
		config:  &plugin.HostConfig{APIBaseURL: "https://api.example.com"},
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{OutputFormat: "json"})
	if resp.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", resp.ExitCode)
	}
	var out StatusOutput
	if err := json.Unmarshal([]byte(resp.Stdout), &out); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if out.LoggedIn {
		t.Error("expected LoggedIn = false")
	}
	if out.APIBaseURL != "https://api.example.com" {
		t.Errorf("APIBaseURL = %q", out.APIBaseURL)
	}
}

func TestExecute_APIError(t *testing.T) {
	h := &mockHost{
		authCtx: &plugin.AuthContext{
			UserEmail:  "user@test.com",
			OrgID:      1,
			OrgName:    "Test Org",
			APIBaseURL: "https://api.example.com",
		},
		apiErr: fmt.Errorf("connection refused"),
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{})
	if resp.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", resp.ExitCode)
	}
}

func TestExecute_APIError_JSON(t *testing.T) {
	h := &mockHost{
		authCtx: &plugin.AuthContext{
			UserEmail:  "user@test.com",
			OrgID:      1,
			OrgName:    "Test Org",
			APIBaseURL: "https://api.example.com",
		},
		apiErr: fmt.Errorf("timeout"),
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{OutputFormat: "json"})
	if resp.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", resp.ExitCode)
	}
	var out StatusOutput
	if err := json.Unmarshal([]byte(resp.Stdout), &out); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !out.LoggedIn {
		t.Error("expected LoggedIn = true")
	}
	if out.APIReachable {
		t.Error("expected APIReachable = false")
	}
}

func TestExecute_APIBadStatus(t *testing.T) {
	h := &mockHost{
		authCtx: &plugin.AuthContext{
			UserEmail:  "user@test.com",
			OrgID:      1,
			OrgName:    "Test Org",
			APIBaseURL: "https://api.example.com",
		},
		apiStatus: 502,
		apiBody:   []byte(`{"error":"bad gateway"}`),
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{})
	if resp.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", resp.ExitCode)
	}
}

func TestExecute_Success(t *testing.T) {
	profileJSON, _ := json.Marshal(map[string]interface{}{
		"user": map[string]interface{}{
			"name":  "Alice",
			"email": "alice@test.com",
		},
	})
	h := &mockHost{
		authCtx: &plugin.AuthContext{
			UserEmail:  "alice@test.com",
			OrgID:      42,
			OrgName:    "Test Org",
			APIBaseURL: "https://api.example.com",
		},
		apiStatus: 200,
		apiBody:   profileJSON,
		config:    &plugin.HostConfig{DERPURL: "wss://derp.example.com"},
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{})
	if resp.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", resp.ExitCode)
	}
	if resp.Error != "" {
		t.Errorf("Error = %q", resp.Error)
	}
}

func TestExecute_Success_JSON(t *testing.T) {
	profileJSON, _ := json.Marshal(map[string]interface{}{
		"user": map[string]interface{}{
			"name":  "Alice",
			"email": "alice@test.com",
		},
	})
	h := &mockHost{
		authCtx: &plugin.AuthContext{
			UserEmail:  "alice@test.com",
			OrgID:      42,
			OrgName:    "Test Org",
			APIBaseURL: "https://api.example.com",
		},
		apiStatus: 200,
		apiBody:   profileJSON,
		config:    &plugin.HostConfig{DERPURL: "wss://derp.example.com"},
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{OutputFormat: "json"})
	if resp.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", resp.ExitCode)
	}

	var out StatusOutput
	if err := json.Unmarshal([]byte(resp.Stdout), &out); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !out.LoggedIn {
		t.Error("expected LoggedIn = true")
	}
	if !out.APIReachable {
		t.Error("expected APIReachable = true")
	}
	if out.UserEmail != "alice@test.com" {
		t.Errorf("UserEmail = %q", out.UserEmail)
	}
	if out.UserName != "Alice" {
		t.Errorf("UserName = %q", out.UserName)
	}
	if out.OrgID != 42 {
		t.Errorf("OrgID = %d", out.OrgID)
	}
	if out.DERPURL != "wss://derp.example.com" {
		t.Errorf("DERPURL = %q", out.DERPURL)
	}
}

func TestExecute_Success_NoDERP(t *testing.T) {
	profileJSON, _ := json.Marshal(map[string]interface{}{
		"user": map[string]interface{}{"name": "Bob", "email": "bob@test.com"},
	})
	h := &mockHost{
		authCtx:   &plugin.AuthContext{UserEmail: "bob@test.com", OrgID: 1, OrgName: "Org", APIBaseURL: "https://api.example.com"},
		apiStatus: 200,
		apiBody:   profileJSON,
		config:    &plugin.HostConfig{},
	}
	p := New(h)
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{OutputFormat: "json"})

	var out StatusOutput
	if err := json.Unmarshal([]byte(resp.Stdout), &out); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if out.DERPURL != "" {
		t.Errorf("DERPURL should be empty, got %q", out.DERPURL)
	}
}
