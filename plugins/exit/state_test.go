package exit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadRemoveExitState(t *testing.T) {
	// Use temp dir as PRYSM_HOME.
	tmpDir := t.TempDir()
	t.Setenv("PRYSM_HOME", tmpDir)

	now := time.Now().Truncate(time.Second)
	st := &ExitState{
		PID:        12345,
		ExitPeer:   "peer-abc",
		ListenAddr: "127.0.0.1:1080",
		StartedAt:  now,
	}

	if err := writeExitState(st); err != nil {
		t.Fatalf("writeExitState: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(filepath.Join(tmpDir, exitStateFile)); err != nil {
		t.Fatalf("state file not found: %v", err)
	}

	got, err := readExitState()
	if err != nil {
		t.Fatalf("readExitState: %v", err)
	}
	if got.PID != 12345 {
		t.Errorf("PID = %d, want 12345", got.PID)
	}
	if got.ExitPeer != "peer-abc" {
		t.Errorf("ExitPeer = %q, want peer-abc", got.ExitPeer)
	}
	if got.ListenAddr != "127.0.0.1:1080" {
		t.Errorf("ListenAddr = %q, want 127.0.0.1:1080", got.ListenAddr)
	}
	if !got.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, now)
	}

	removeExitState()

	// Verify file is gone.
	if _, err := os.Stat(filepath.Join(tmpDir, exitStateFile)); !os.IsNotExist(err) {
		t.Error("state file should be removed")
	}
}

func TestReadExitState_NotFound(t *testing.T) {
	t.Setenv("PRYSM_HOME", t.TempDir())
	_, err := readExitState()
	if err == nil {
		t.Error("expected error for missing state file")
	}
}

func TestRemoveExitState_NoFile(t *testing.T) {
	t.Setenv("PRYSM_HOME", t.TempDir())
	// Should not panic.
	removeExitState()
}
