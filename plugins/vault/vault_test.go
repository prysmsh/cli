package vault

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/prysmsh/cli/internal/plugin"
)

// mockHost implements plugin.HostServices for testing.
type mockHost struct {
	token   string
	userID  uint64
	orgID   uint64
	homeDir string
	logs    []string
}

func (m *mockHost) GetAuthContext(ctx context.Context) (*plugin.AuthContext, error) {
	return &plugin.AuthContext{
		Token:  m.token,
		UserID: m.userID,
		OrgID:  m.orgID,
	}, nil
}

func (m *mockHost) APIRequest(ctx context.Context, method, endpoint string, body []byte) (int, []byte, error) {
	return 200, nil, nil
}

func (m *mockHost) GetConfig(ctx context.Context) (*plugin.HostConfig, error) {
	return &plugin.HostConfig{HomeDir: m.homeDir}, nil
}

func (m *mockHost) Log(ctx context.Context, level plugin.LogLevel, message string) error {
	m.logs = append(m.logs, message)
	return nil
}

func (m *mockHost) PromptInput(ctx context.Context, label string, isSecret bool) (string, error) {
	return "", nil
}

func (m *mockHost) PromptConfirm(ctx context.Context, label string) (bool, error) {
	return true, nil
}

// testVault creates an initialized vault plugin for testing.
func testVault(t *testing.T) (*VaultPlugin, context.Context) {
	t.Helper()
	dir := t.TempDir()
	host := &mockHost{
		token:   "test-token-abc123",
		userID:  1,
		orgID:   100,
		homeDir: dir,
	}

	p := New(host)
	ctx := context.Background()

	// Initialize the vault.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init"}})
	if resp.ExitCode != 0 || resp.Error != "" {
		t.Fatalf("vault init failed: %s", resp.Error)
	}

	return p, ctx
}

// testVaultWithStore returns an initialized vault plugin with open store.
func testVaultWithStore(t *testing.T) (*VaultPlugin, context.Context) {
	t.Helper()
	dir := t.TempDir()
	host := &mockHost{
		token:   "test-token-abc123",
		userID:  1,
		orgID:   100,
		homeDir: dir,
	}

	p := New(host)
	ctx := context.Background()

	// Initialize.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init"}})
	if resp.ExitCode != 0 || resp.Error != "" {
		t.Fatalf("vault init failed: %s", resp.Error)
	}

	// Open vault so tests can access store directly.
	if err := p.openVault(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.closeVault() })

	return p, ctx
}

func TestVaultInitAndStatus(t *testing.T) {
	dir := t.TempDir()
	host := &mockHost{
		token:   "my-session-token",
		userID:  42,
		orgID:   1,
		homeDir: dir,
	}

	p := New(host)
	ctx := context.Background()

	// Status before init should warn.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"status"}})
	if resp.ExitCode != 0 {
		t.Fatal("status before init should succeed (exit 0)")
	}

	// Init.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init"}})
	if resp.ExitCode != 0 || resp.Error != "" {
		t.Fatalf("init failed: %s", resp.Error)
	}

	// Verify DB file exists.
	dbPath := filepath.Join(dir, "vault", "vault.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !store.IsInitialized() {
		store.Close()
		t.Fatal("store should be initialized")
	}
	store.Close() // Must close before next Execute (bbolt exclusive lock).

	// Double init should fail.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init"}})
	if resp.ExitCode != 1 {
		t.Fatal("double init should fail")
	}

	// Status after init should succeed.
	host.logs = nil
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"status"}})
	if resp.ExitCode != 0 {
		t.Fatalf("status after init failed: %s", resp.Error)
	}

	// JSON status.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"status"}, OutputFormat: "json"})
	if resp.Stdout == "" {
		t.Fatal("expected JSON output")
	}
}

func TestVaultFallbackKEK(t *testing.T) {
	dir := t.TempDir()
	host := &mockHost{
		token:   "original-token",
		userID:  1,
		orgID:   100,
		homeDir: dir,
	}

	p := New(host)
	ctx := context.Background()

	// Initialize with original token.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init"}})
	if resp.ExitCode != 0 {
		t.Fatalf("init failed: %s", resp.Error)
	}

	// Store a secret with original token.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/test", "key=value"}})
	if resp.ExitCode != 0 {
		t.Fatalf("put failed: %s", resp.Error)
	}

	// Change the token (simulates session refresh).
	host.token = "new-refreshed-token"

	// The fallback KEK should kick in and we should still be able to read.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get", "secret/test"}})
	if resp.ExitCode != 0 {
		t.Fatalf("get with new token failed (fallback should work): exit=%d", resp.ExitCode)
	}
}

func TestVaultPQCInitAndOpen(t *testing.T) {
	dir := t.TempDir()
	host := &mockHost{
		token:   "pqc-test-token",
		userID:  1,
		orgID:   100,
		homeDir: dir,
	}

	p := New(host)
	ctx := context.Background()

	// Init vault (creates PQC envelope).
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init"}})
	if resp.ExitCode != 0 || resp.Error != "" {
		t.Fatalf("init failed: %s", resp.Error)
	}

	// Verify PQC metadata was stored.
	dbPath := filepath.Join(dir, "vault", "vault.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	pqcKey, _ := store.GetMeta("pqc_private_key")
	kemCT, _ := store.GetMeta("kem_ciphertext")
	pqcDEK, _ := store.GetMeta("pqc_wrapped_dek")
	store.Close()

	if pqcKey == nil {
		t.Fatal("pqc_private_key should be stored")
	}
	if kemCT == nil {
		t.Fatal("kem_ciphertext should be stored")
	}
	if pqcDEK == nil {
		t.Fatal("pqc_wrapped_dek should be stored")
	}

	// Store a secret and verify it round-trips via PQC vault.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/pqc", "quantum=safe"}})
	if resp.ExitCode != 0 {
		t.Fatalf("put failed: %s", resp.Error)
	}

	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get", "secret/pqc"}})
	if resp.ExitCode != 0 {
		t.Fatalf("get failed: exit=%d", resp.ExitCode)
	}
}

func TestVaultPQCStatus(t *testing.T) {
	dir := t.TempDir()
	host := &mockHost{
		token:   "pqc-status-token",
		userID:  1,
		orgID:   100,
		homeDir: dir,
	}

	p := New(host)
	ctx := context.Background()

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"status"}, OutputFormat: "json"})
	if resp.ExitCode != 0 {
		t.Fatalf("status failed: %s", resp.Error)
	}
	if resp.Stdout == "" {
		t.Fatal("expected JSON output")
	}

	var info map[string]interface{}
	if err := json.Unmarshal([]byte(resp.Stdout), &info); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	pqcEnv, ok := info["pqc_envelope"].(string)
	if !ok || pqcEnv != "active (X25519 + Kyber768)" {
		t.Fatalf("expected PQC envelope active, got: %v", info["pqc_envelope"])
	}
}

func TestVaultPQCFallbackKEK(t *testing.T) {
	dir := t.TempDir()
	host := &mockHost{
		token:   "original-pqc-token",
		userID:  1,
		orgID:   100,
		homeDir: dir,
	}

	p := New(host)
	ctx := context.Background()

	// Initialize with PQC.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init"}})
	if resp.ExitCode != 0 {
		t.Fatalf("init failed: %s", resp.Error)
	}

	// Store a secret.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/fb", "key=val"}})
	if resp.ExitCode != 0 {
		t.Fatalf("put failed: %s", resp.Error)
	}

	// Change token (triggers fallback recovery).
	host.token = "new-pqc-token"

	// Should recover via classical fallback and re-wrap PQC envelope.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get", "secret/fb"}})
	if resp.ExitCode != 0 {
		t.Fatalf("get with new token failed (fallback should work): exit=%d", resp.ExitCode)
	}
}

func TestVaultUnknownSubcommand(t *testing.T) {
	p := New(&mockHost{homeDir: t.TempDir(), token: "t", userID: 1, orgID: 1})
	resp := p.Execute(context.Background(), plugin.ExecuteRequest{Args: []string{"nonexistent"}})
	if resp.Error == "" {
		t.Fatal("expected error for unknown subcommand")
	}
}
