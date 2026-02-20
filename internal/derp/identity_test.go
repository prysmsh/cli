package derp

import (
	"os"
	"path/filepath"
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
