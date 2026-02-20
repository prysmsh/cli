package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	pluginv1 "github.com/warp-run/prysm-cli/proto/plugin/v1"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/config"
	"github.com/warp-run/prysm-cli/internal/session"
)

func TestGRPCHostServer_GetAuthContext(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir + "/s.json")
	_ = store.Save(&session.Session{
		Token: "tok", Email: "u@e.com",
		User: session.SessionUser{ID: 1, Email: "u@e.com"},
		Organization: session.SessionOrg{ID: 1, Name: "o"},
	})
	host := NewBuiltinHostServices(&AppContext{
		Config:   &config.Config{APIBaseURL: "https://api.example.com"},
		Sessions: store,
	})
	srv := NewGRPCHostServer(host)

	resp, err := srv.GetAuthContext(context.Background(), &pluginv1.GetAuthContextRequest{})
	if err != nil {
		t.Fatalf("GetAuthContext: %v", err)
	}
	if resp.Token != "tok" {
		t.Errorf("Token = %q", resp.Token)
	}
}

func TestGRPCHostServer_GetAuthContext_NotLoggedIn(t *testing.T) {
	host := NewBuiltinHostServices(&AppContext{
		Config:   &config.Config{APIBaseURL: "https://api.example.com"},
		Sessions: session.NewStore(t.TempDir() + "/s.json"),
	})
	srv := NewGRPCHostServer(host)

	_, err := srv.GetAuthContext(context.Background(), &pluginv1.GetAuthContextRequest{})
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
}

func TestGRPCHostServer_GetConfig(t *testing.T) {
	host := NewBuiltinHostServices(&AppContext{
		Config: &config.Config{
			APIBaseURL: "https://api.example.com",
			DERPServerURL: "wss://derp.example.com",
			HomeDir: "/home", OutputFormat: "json",
		},
	})
	srv := NewGRPCHostServer(host)

	resp, err := srv.GetConfig(context.Background(), &pluginv1.GetConfigRequest{})
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if resp.ApiBaseUrl != "https://api.example.com" {
		t.Errorf("ApiBaseUrl = %q", resp.ApiBaseUrl)
	}
}

func TestGRPCHostServer_Log(t *testing.T) {
	host := NewBuiltinHostServices(&AppContext{})
	srv := NewGRPCHostServer(host)

	_, err := srv.Log(context.Background(), &pluginv1.LogRequest{
		Level:   pluginv1.LogLevel_LOG_LEVEL_INFO,
		Message: "test",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
}

func TestGRPCHostServer_PromptInput(t *testing.T) {
	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	go func() { w.WriteString("answer\n"); w.Close() }()

	host := NewBuiltinHostServices(&AppContext{})
	srv := NewGRPCHostServer(host)

	resp, err := srv.PromptInput(context.Background(), &pluginv1.PromptInputRequest{
		Label: "Q", IsSecret: false,
	})
	if err != nil {
		t.Fatalf("PromptInput: %v", err)
	}
	if resp.Value != "answer" {
		t.Errorf("Value = %q", resp.Value)
	}
}

func TestGRPCHostServer_PromptConfirm(t *testing.T) {
	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	go func() { w.WriteString("y\n"); w.Close() }()

	host := NewBuiltinHostServices(&AppContext{})
	srv := NewGRPCHostServer(host)

	resp, err := srv.PromptConfirm(context.Background(), &pluginv1.PromptConfirmRequest{Label: "Ok?"})
	if err != nil {
		t.Fatalf("PromptConfirm: %v", err)
	}
	if !resp.Confirmed {
		t.Error("Confirmed = false")
	}
}

func TestGRPCHostServer_APIRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	host := NewBuiltinHostServices(&AppContext{
		Config: &config.Config{APIBaseURL: srv.URL},
		API:    api.NewClient(srv.URL),
	})
	grpcSrv := NewGRPCHostServer(host)

	resp, err := grpcSrv.APIRequest(context.Background(), &pluginv1.APIRequestRequest{
		Method: "GET", Endpoint: "/", Body: nil,
	})
	if err != nil {
		t.Fatalf("APIRequest: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
}
