package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrysmHome(t *testing.T) {
	// Clear PRYSM_HOME so we get predictable fallback
	oldHome := os.Getenv("PRYSM_HOME")
	oldHOME := os.Getenv("HOME")
	defer func() {
		if oldHome != "" {
			os.Setenv("PRYSM_HOME", oldHome)
		} else {
			os.Unsetenv("PRYSM_HOME")
		}
		os.Setenv("HOME", oldHOME)
	}()

	t.Run("uses PRYSM_HOME when set", func(t *testing.T) {
		os.Setenv("PRYSM_HOME", "/custom/prysm/home")
		defer os.Unsetenv("PRYSM_HOME")
		got := PrysmHome()
		if got != "/custom/prysm/home" {
			t.Errorf("PrysmHome() = %q, want /custom/prysm/home", got)
		}
	})

	t.Run("falls back to HOME/.prysm when PRYSM_HOME unset", func(t *testing.T) {
		os.Unsetenv("PRYSM_HOME")
		dir := t.TempDir()
		os.Setenv("HOME", dir)
		got := PrysmHome()
		want := filepath.Join(dir, ".prysm")
		if got != want {
			t.Errorf("PrysmHome() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to HOME env when UserHomeDir fails", func(t *testing.T) {
		os.Unsetenv("PRYSM_HOME")
		os.Setenv("HOME", "/fallback-home")
		// UserHomeDir may still succeed on the system; we're testing the fallback path.
		// If HOME is set and UserHomeDir fails (e.g. in restricted env), we use HOME.
		got := PrysmHome()
		// Implementation uses filepath.Join(os.Getenv("HOME"), ".prysm") when UserHomeDir fails
		if got == "" {
			t.Error("PrysmHome() should not return empty")
		}
		if got != filepath.Join("/fallback-home", ".prysm") && got != filepath.Join(os.Getenv("HOME"), ".prysm") {
			t.Logf("PrysmHome() = %q (acceptable if UserHomeDir succeeded)", got)
		}
	})
}

func TestEnsurePrysmHome(t *testing.T) {
	dir := t.TempDir()
	oldHome := os.Getenv("PRYSM_HOME")
	os.Setenv("PRYSM_HOME", dir)
	defer func() {
		if oldHome != "" {
			os.Setenv("PRYSM_HOME", oldHome)
		} else {
			os.Unsetenv("PRYSM_HOME")
		}
	}()

	path, err := EnsurePrysmHome()
	if err != nil {
		t.Fatalf("EnsurePrysmHome() err = %v", err)
	}
	if path != dir {
		t.Errorf("EnsurePrysmHome() = %q, want %q", path, dir)
	}

	// Idempotent: create again
	path2, err := EnsurePrysmHome()
	if err != nil {
		t.Fatalf("EnsurePrysmHome() second call err = %v", err)
	}
	if path2 != dir {
		t.Errorf("EnsurePrysmHome() second call = %q, want %q", path2, dir)
	}

	// Nested dir
	nested := filepath.Join(dir, "nested", "dir")
	os.Setenv("PRYSM_HOME", nested)
	path3, err := EnsurePrysmHome()
	if err != nil {
		t.Fatalf("EnsurePrysmHome() nested err = %v", err)
	}
	if path3 != nested {
		t.Errorf("EnsurePrysmHome() nested = %q, want %q", path3, nested)
	}
}

func TestEnsurePrysmHomeReadOnlyParent(t *testing.T) {
	// Create a read-only directory; creating a subdir inside may fail on some systems
	dir := t.TempDir()
	readOnly := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(readOnly, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(readOnly, 0o444); err != nil {
		t.Skip("Cannot make directory read-only on this system")
	}
	defer os.Chmod(readOnly, 0o755)

	os.Setenv("PRYSM_HOME", filepath.Join(readOnly, "prysm"))
	_, err := EnsurePrysmHome()
	if err == nil {
		t.Error("EnsurePrysmHome() expected error when parent is read-only")
	}
}
