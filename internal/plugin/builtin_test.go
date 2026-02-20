package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/config"
	"github.com/warp-run/prysm-cli/internal/session"
)

func TestNewBuiltinHostServices(t *testing.T) {
	app := &AppContext{
		Config:   &config.Config{APIBaseURL: "https://api.example.com"},
		Sessions: session.NewStore("/tmp/session.json"),
		Format:   "table",
		Debug:    false,
	}
	h := NewBuiltinHostServices(app)
	if h == nil {
		t.Fatal("NewBuiltinHostServices returned nil")
	}
}

func TestBuiltinHostServices_GetConfig(t *testing.T) {
	app := &AppContext{
		Config: &config.Config{
			APIBaseURL:    "https://api.example.com",
			DERPServerURL: "wss://derp.example.com",
			HomeDir:       "/home/.prysm",
			OutputFormat:  "json",
		},
		Format: "json",
	}
	h := NewBuiltinHostServices(app)

	cfg, err := h.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig err = %v", err)
	}
	if cfg.APIBaseURL != "https://api.example.com" {
		t.Errorf("APIBaseURL = %q", cfg.APIBaseURL)
	}
	if cfg.DERPURL != "wss://derp.example.com" {
		t.Errorf("DERPURL = %q", cfg.DERPURL)
	}
	if cfg.HomeDir != "/home/.prysm" {
		t.Errorf("HomeDir = %q", cfg.HomeDir)
	}
	if cfg.OutputFormat != "json" {
		t.Errorf("OutputFormat = %q", cfg.OutputFormat)
	}
}

func TestBuiltinHostServices_GetAuthContext_NotLoggedIn(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir + "/session.json")
	app := &AppContext{
		Config:   &config.Config{APIBaseURL: "https://api.example.com"},
		Sessions: store,
	}
	h := NewBuiltinHostServices(app)

	_, err := h.GetAuthContext(context.Background())
	if err == nil {
		t.Fatal("GetAuthContext expected error when not logged in")
	}
}

func TestBuiltinHostServices_GetAuthContext_LoggedIn(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir + "/session.json")
	sess := &session.Session{
		Token:        "token123",
		Email:        "u@example.com",
		APIBaseURL:   "https://api.example.com",
		User:        session.SessionUser{ID: 1, Email: "u@example.com"},
		Organization: session.SessionOrg{ID: 100, Name: "Org"},
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}

	app := &AppContext{
		Config:   &config.Config{APIBaseURL: "https://api.example.com"},
		Sessions: store,
	}
	h := NewBuiltinHostServices(app)

	auth, err := h.GetAuthContext(context.Background())
	if err != nil {
		t.Fatalf("GetAuthContext err = %v", err)
	}
	if auth.Token != "token123" {
		t.Errorf("Token = %q", auth.Token)
	}
	if auth.UserEmail != "u@example.com" {
		t.Errorf("UserEmail = %q", auth.UserEmail)
	}
	if auth.OrgID != 100 {
		t.Errorf("OrgID = %d", auth.OrgID)
	}
}

func TestBuiltinHostServices_APIRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	app := &AppContext{
		Config:   &config.Config{APIBaseURL: srv.URL},
		Sessions: session.NewStore("/tmp/session.json"),
		API:      client,
	}
	h := NewBuiltinHostServices(app)

	status, body, err := h.APIRequest(context.Background(), "GET", "/test", nil)
	if err != nil {
		t.Fatalf("APIRequest err = %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d", status)
	}
	var out map[string]bool
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !out["ok"] {
		t.Error("body ok not true")
	}
}

func TestBuiltinHostServices_APIRequest_WithBody(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	app := &AppContext{
		Config:   &config.Config{APIBaseURL: srv.URL},
		Sessions: session.NewStore("/tmp/session.json"),
		API:      client,
	}
	h := NewBuiltinHostServices(app)

	payload := []byte(`{"key":"value"}`)
	_, _, err := h.APIRequest(context.Background(), "POST", "/test", payload)
	if err != nil {
		t.Fatalf("APIRequest err = %v", err)
	}
	// Client may encode with or without trailing newline
	got := string(captured)
	want := string(payload)
	if got != want && got != want+"\n" {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestBuiltinHostServices_Log(t *testing.T) {
	app := &AppContext{Debug: false}
	h := NewBuiltinHostServices(app)
	ctx := context.Background()

	levels := []struct {
		level   LogLevel
		message string
	}{
		{LogLevelInfo, "info msg"},
		{LogLevelSuccess, "success msg"},
		{LogLevelWarning, "warning msg"},
		{LogLevelError, "error msg"},
		{LogLevelPlain, "plain msg"},
	}
	for _, lv := range levels {
		if err := h.Log(ctx, lv.level, lv.message); err != nil {
			t.Errorf("Log(%v): %v", lv.level, err)
		}
	}

	// Debug with Debug=false: no output but no error
	app.Debug = false
	if err := h.Log(ctx, LogLevelDebug, "debug off"); err != nil {
		t.Errorf("Log(Debug): %v", err)
	}
	app.Debug = true
	if err := h.Log(ctx, LogLevelDebug, "debug on"); err != nil {
		t.Errorf("Log(Debug): %v", err)
	}
}

func TestBuiltinHostServices_PromptInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("  typed value  \n")
		w.Close()
	}()

	app := &AppContext{}
	h := NewBuiltinHostServices(app)
	got, err := h.PromptInput(context.Background(), "Label", false)
	if err != nil {
		t.Fatalf("PromptInput: %v", err)
	}
	if got != "typed value" {
		t.Errorf("PromptInput() = %q, want typed value", got)
	}
}

func TestBuiltinHostServices_PromptInputNoInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	w.Close()

	app := &AppContext{}
	h := NewBuiltinHostServices(app)
	_, err = h.PromptInput(context.Background(), "Label", false)
	if err == nil {
		t.Error("PromptInput expected error on EOF")
	}
}

func TestBuiltinHostServices_PromptConfirm(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("yes\n")
		w.Close()
	}()

	app := &AppContext{}
	h := NewBuiltinHostServices(app)
	got, err := h.PromptConfirm(context.Background(), "Continue?")
	if err != nil {
		t.Fatalf("PromptConfirm: %v", err)
	}
	if !got {
		t.Error("PromptConfirm() = false, want true")
	}
}

func TestBuiltinHostServices_PromptConfirmNo(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("n\n")
		w.Close()
	}()

	app := &AppContext{}
	h := NewBuiltinHostServices(app)
	got, err := h.PromptConfirm(context.Background(), "Continue?")
	if err != nil {
		t.Fatalf("PromptConfirm: %v", err)
	}
	if got {
		t.Error("PromptConfirm() = true, want false")
	}
}

func TestBuiltinHostServices_PromptConfirmEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	w.Close() // EOF immediately, no input

	app := &AppContext{}
	h := NewBuiltinHostServices(app)
	got, err := h.PromptConfirm(context.Background(), "Continue?")
	if err != nil {
		t.Fatalf("PromptConfirm on EOF: %v", err)
	}
	if got {
		t.Error("PromptConfirm() on EOF = true, want false")
	}
}

func TestBuiltinHostServices_doAPIRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	app := &AppContext{
		Config:   &config.Config{APIBaseURL: srv.URL},
		Sessions: session.NewStore("/tmp/session.json"),
		API:      client,
	}
	h := NewBuiltinHostServices(app)

	resp, err := h.doAPIRaw(context.Background(), "GET", "/", nil)
	if err != nil {
		t.Fatalf("doAPIRaw: %v", err)
	}
	if resp == nil {
		t.Fatal("resp is nil")
	}
	resp.Body.Close()
}

func TestBuiltinHostServices_APIRequest_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code": "BAD", "message": "bad request"}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	app := &AppContext{
		Config:   &config.Config{APIBaseURL: srv.URL},
		Sessions: session.NewStore("/tmp/session.json"),
		API:      client,
	}
	h := NewBuiltinHostServices(app)

	status, body, err := h.APIRequest(context.Background(), "GET", "/test", nil)
	if err != nil {
		t.Fatalf("APIRequest should return status/body on API error, err = %v", err)
	}
	if status != http.StatusBadRequest {
		t.Errorf("status = %d", status)
	}
	if len(body) == 0 {
		t.Error("body should be non-empty")
	}
}
