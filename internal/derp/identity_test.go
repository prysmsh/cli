package derp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureDeviceIDStable(t *testing.T) {
	dir := t.TempDir()

	id1, err := EnsureDeviceID(dir)
	if err != nil {
		t.Fatalf("EnsureDeviceID returned error: %v", err)
	}
	if id1 == "" {
		t.Fatalf("expected non-empty device ID")
	}

	// Ensure the identifier was persisted to disk.
	if _, err := os.Stat(filepath.Join(dir, "mesh-device-id")); err != nil {
		t.Fatalf("expected mesh-device-id file to exist: %v", err)
	}

	id2, err := EnsureDeviceID(dir)
	if err != nil {
		t.Fatalf("EnsureDeviceID second call returned error: %v", err)
	}

	if id1 != id2 {
		t.Fatalf("expected stable device ID, got %q then %q", id1, id2)
	}
}

func TestEnsureDeviceIDEmptyHome(t *testing.T) {
	_, err := EnsureDeviceID("")
	if err == nil {
		t.Fatal("expected error for empty home dir")
	}
}

func TestEnsureDeviceIDExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mesh-device-id")
	if err := os.WriteFile(path, []byte("existing-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	id, err := EnsureDeviceID(dir)
	if err != nil {
		t.Fatalf("EnsureDeviceID: %v", err)
	}
	if id != "existing-id" {
		t.Errorf("id = %q, want existing-id", id)
	}
}

func TestEnsureDeviceID_WriteFileFails(t *testing.T) {
	dir := t.TempDir()
	// Make mesh-device-id a directory so WriteFile fails
	path := filepath.Join(dir, "mesh-device-id")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := EnsureDeviceID(dir)
	if err == nil {
		t.Fatal("expected error when mesh-device-id is a directory")
	}
	if !strings.Contains(err.Error(), "persist mesh device id") {
		t.Errorf("error = %v", err)
	}
}

func TestEnsureDeviceIDExistingFileEmptyTrimmed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mesh-device-id")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	id, err := EnsureDeviceID(dir)
	if err != nil {
		t.Fatalf("EnsureDeviceID: %v", err)
	}
	if id == "" {
		t.Error("expected new id when file is whitespace-only")
	}
}
