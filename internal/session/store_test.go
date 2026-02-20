package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	store := NewStore(path)

	sess := &Session{
		Token:         "test-token-abc123",
		RefreshToken:  "refresh-token-xyz",
		Email:         "user@example.com",
		SessionID:     "sess-123",
		CSRFToken:     "csrf-token",
		ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
		User: SessionUser{
			ID:         42,
			Name:       "Test User",
			Email:      "user@example.com",
			Role:       "admin",
			MFAEnabled: true,
		},
		Organization: SessionOrg{
			ID:   100,
			Name: "Test Org",
		},
		APIBaseURL:    "https://api.example.com",
		ComplianceURL: "https://compliance.example.com",
		DERPServerURL: "wss://derp.example.com",
		OutputFormat:  "json",
	}

	if err := store.Save(sess); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("Expected file permission 0600, got %o", info.Mode().Perm())
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("Expected session, got nil")
	}

	if loaded.Token != sess.Token {
		t.Errorf("Token mismatch: got %q, want %q", loaded.Token, sess.Token)
	}
	if loaded.Email != sess.Email {
		t.Errorf("Email mismatch: got %q, want %q", loaded.Email, sess.Email)
	}
	if loaded.User.ID != sess.User.ID {
		t.Errorf("User.ID mismatch: got %d, want %d", loaded.User.ID, sess.User.ID)
	}
	if loaded.Organization.Name != sess.Organization.Name {
		t.Errorf("Organization.Name mismatch: got %q, want %q", loaded.Organization.Name, sess.Organization.Name)
	}
}

func TestStoreLoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	store := NewStore(path)

	sess, err := store.Load()
	if err != nil {
		t.Fatalf("Load should not error for non-existent file: %v", err)
	}
	if sess != nil {
		t.Errorf("Expected nil session for non-existent file, got %v", sess)
	}
}

func TestStoreClear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	store := NewStore(path)

	sess := &Session{
		Token: "test-token",
		Email: "user@example.com",
	}

	if err := store.Save(sess); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Session file should exist after save")
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Session file should not exist after clear")
	}
}

func TestStoreClearNonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	store := NewStore(path)

	// Clear should not error if file doesn't exist
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear should not error for non-existent file: %v", err)
	}
}

func TestStoreSaveNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	store := NewStore(path)

	err := store.Save(nil)
	if err == nil {
		t.Fatal("Expected error when saving nil session")
	}
}

func TestStorePath(t *testing.T) {
	path := "/some/path/session.json"
	store := NewStore(path)

	if got := store.Path(); got != path {
		t.Errorf("Path() = %q, want %q", got, path)
	}
}

func TestSessionExpiresAt(t *testing.T) {
	tests := []struct {
		name    string
		session *Session
		wantZero bool
	}{
		{
			name:     "nil session",
			session:  nil,
			wantZero: true,
		},
		{
			name:     "zero expires_at",
			session:  &Session{ExpiresAtUnix: 0},
			wantZero: true,
		},
		{
			name:    "valid expires_at",
			session: &Session{ExpiresAtUnix: time.Now().Add(time.Hour).Unix()},
			wantZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.session.ExpiresAt()
			if tt.wantZero && !got.IsZero() {
				t.Errorf("ExpiresAt() should be zero time, got %v", got)
			}
			if !tt.wantZero && got.IsZero() {
				t.Error("ExpiresAt() should not be zero time")
			}
		})
	}
}

func TestSessionExpiresAtWithTTLOverride(t *testing.T) {
	savedAt := time.Now()
	sess := &Session{
		SavedAt:       savedAt,
		ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
		TTLOverride:   30 * time.Minute,
	}

	expected := savedAt.Add(30 * time.Minute)
	got := sess.ExpiresAt()

	// Allow 1 second tolerance
	if got.Sub(expected).Abs() > time.Second {
		t.Errorf("ExpiresAt() with TTLOverride = %v, want ~%v", got, expected)
	}
}

func TestSessionIsExpired(t *testing.T) {
	tests := []struct {
		name     string
		session  *Session
		window   time.Duration
		want     bool
	}{
		{
			name: "not expired",
			session: &Session{
				ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
			},
			window: 0,
			want:   false,
		},
		{
			name: "expired",
			session: &Session{
				ExpiresAtUnix: time.Now().Add(-time.Hour).Unix(),
			},
			window: 0,
			want:   true,
		},
		{
			name: "within window",
			session: &Session{
				ExpiresAtUnix: time.Now().Add(5 * time.Minute).Unix(),
			},
			window: 10 * time.Minute,
			want:   true,
		},
		{
			name: "outside window",
			session: &Session{
				ExpiresAtUnix: time.Now().Add(15 * time.Minute).Unix(),
			},
			window: 10 * time.Minute,
			want:   false,
		},
		{
			name: "negative window treated as zero",
			session: &Session{
				ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
			},
			window: -10 * time.Minute,
			want:   false,
		},
		{
			name:    "zero expiry returns not expired",
			session: &Session{ExpiresAtUnix: 0},
			window:  0,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.session.IsExpired(tt.window)
			if got != tt.want {
				t.Errorf("IsExpired(%v) = %v, want %v", tt.window, got, tt.want)
			}
		})
	}
}

func TestStoreAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	store := NewStore(path)

	// Save first session
	sess1 := &Session{Token: "token1", Email: "user1@example.com"}
	if err := store.Save(sess1); err != nil {
		t.Fatalf("Save sess1 failed: %v", err)
	}

	// Save second session (should atomically replace)
	sess2 := &Session{Token: "token2", Email: "user2@example.com"}
	if err := store.Save(sess2); err != nil {
		t.Fatalf("Save sess2 failed: %v", err)
	}

	// Verify temp file is cleaned up
	tempPath := path + ".tmp"
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("Temp file should not exist after successful save")
	}

	// Verify correct session is loaded
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Token != "token2" {
		t.Errorf("Expected token2, got %s", loaded.Token)
	}
}

func TestStoreCreateDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested", "dir", "session.json")

	store := NewStore(nested)

	sess := &Session{Token: "test-token", Email: "user@example.com"}
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save with nested dir failed: %v", err)
	}

	// Verify directory was created
	parentDir := filepath.Dir(nested)
	info, err := os.Stat(parentDir)
	if err != nil {
		t.Fatalf("Parent dir stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("Expected parent to be a directory")
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("Expected dir permission 0700, got %o", info.Mode().Perm())
	}
}

func TestStoreLoadCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	// Write invalid JSON
	if err := os.WriteFile(path, []byte("not valid json{"), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	store := NewStore(path)
	_, err := store.Load()
	if err == nil {
		t.Fatal("Expected error loading corrupted JSON")
	}
}

func TestStoreSave_MkdirAllFails(t *testing.T) {
	// Parent is a file, so MkdirAll(parent) fails
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "parent")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parentFile, "session.json")
	store := NewStore(path)

	err := store.Save(&Session{Token: "t", Email: "e@e.com"})
	if err == nil {
		t.Fatal("expected error when parent is not a directory")
	}
	if !strings.Contains(err.Error(), "ensure session directory") {
		t.Errorf("error = %v", err)
	}
}

func TestStoreLoadWithZeroSavedAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	// Session JSON without saved_at (zero value) — backfill from file ModTime
	body := `{"token":"t","email":"u@e.com","user":{"id":1,"name":"u","email":"u@e.com","role":"user","mfa_enabled":false},"organization":{"id":1,"name":"o"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store := NewStore(path)
	sess, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if sess == nil {
		t.Fatal("expected session")
	}
	if sess.SavedAt.IsZero() {
		t.Error("SavedAt should be backfilled from file ModTime")
	}
}
