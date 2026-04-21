package cmd

import (
	"os"
	"testing"
	"time"
)

func TestDaemonRecord_RoundTrip(t *testing.T) {
	home := t.TempDir()

	rec := daemonRecord{
		PID:       12345,
		Port:      8080,
		StartedAt: time.Now().UTC().Truncate(time.Second),
		LogPath:   daemonLogPath(home, 8080),
	}
	if err := writeDaemonRecord(home, rec); err != nil {
		t.Fatal(err)
	}

	got, err := readDaemonRecord(home, 8080)
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != rec.PID || got.Port != rec.Port || got.LogPath != rec.LogPath {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", got, rec)
	}
}

func TestDaemonRecord_UpdateTunnelID(t *testing.T) {
	home := t.TempDir()

	if err := writeDaemonRecord(home, daemonRecord{PID: 1, Port: 3000, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := updateDaemonTunnelID(home, 3000, 42); err != nil {
		t.Fatal(err)
	}
	got, err := readDaemonRecord(home, 3000)
	if err != nil {
		t.Fatal(err)
	}
	if got.TunnelID != 42 {
		t.Fatalf("want tunnel_id=42, got %d", got.TunnelID)
	}
}

func TestListDaemonRecords_EmptyAndPopulated(t *testing.T) {
	home := t.TempDir()

	got, err := listDaemonRecords(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %d records", len(got))
	}

	_ = writeDaemonRecord(home, daemonRecord{PID: 10, Port: 1000, StartedAt: time.Now()})
	_ = writeDaemonRecord(home, daemonRecord{PID: 20, Port: 2000, StartedAt: time.Now()})

	got, err = listDaemonRecords(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 records, got %d", len(got))
	}
}

func TestListDaemonRecords_IgnoresGarbageFiles(t *testing.T) {
	home := t.TempDir()
	dir := daemonDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// These should be skipped, not errored.
	_ = os.WriteFile(dir+"/notanumber.json", []byte("{}"), 0o600)
	_ = os.WriteFile(dir+"/README.md", []byte("hi"), 0o600)
	_ = writeDaemonRecord(home, daemonRecord{PID: 1, Port: 8000, StartedAt: time.Now()})

	got, err := listDaemonRecords(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
}

func TestProcessAlive_CurrentAndFake(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("expected own pid to be alive")
	}
	// PID 0 is definitely not a runnable process.
	if processAlive(0) {
		t.Fatal("pid 0 should be reported as dead")
	}
}

func TestDeleteDaemonRecord_IdempotentOnMissing(t *testing.T) {
	home := t.TempDir()
	// No file exists — must not error.
	if err := deleteDaemonRecord(home, 9999); err != nil {
		t.Fatalf("delete of missing record errored: %v", err)
	}
}
