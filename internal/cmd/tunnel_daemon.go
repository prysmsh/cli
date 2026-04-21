package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// daemonRecord is the JSON blob the expose daemon writes to ~/.prysm/tunnels/<port>.json
// so that `prysm tunnel status` / `prysm tunnel logs` can correlate a local
// process with the backend tunnel row.
type daemonRecord struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	TunnelID  int64     `json:"tunnel_id,omitempty"`
	StartedAt time.Time `json:"started_at"`
	LogPath   string    `json:"log_path"`
}

func daemonDir(homeDir string) string {
	return filepath.Join(homeDir, "tunnels")
}

func daemonRecordPath(homeDir string, port int) string {
	return filepath.Join(daemonDir(homeDir), fmt.Sprintf("%d.json", port))
}

func daemonLogPath(homeDir string, port int) string {
	return filepath.Join(homeDir, "logs", fmt.Sprintf("tunnel-%d.log", port))
}

func writeDaemonRecord(homeDir string, rec daemonRecord) error {
	if err := os.MkdirAll(daemonDir(homeDir), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(daemonRecordPath(homeDir, rec.Port), data, 0o600)
}

func updateDaemonTunnelID(homeDir string, port int, tunnelID int64) error {
	rec, err := readDaemonRecord(homeDir, port)
	if err != nil {
		return err
	}
	rec.TunnelID = tunnelID
	return writeDaemonRecord(homeDir, *rec)
}

func readDaemonRecord(homeDir string, port int) (*daemonRecord, error) {
	data, err := os.ReadFile(daemonRecordPath(homeDir, port))
	if err != nil {
		return nil, err
	}
	var rec daemonRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func listDaemonRecords(homeDir string) ([]daemonRecord, error) {
	entries, err := os.ReadDir(daemonDir(homeDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]daemonRecord, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		rec, err := readDaemonRecord(homeDir, port)
		if err != nil {
			continue
		}
		out = append(out, *rec)
	}
	return out, nil
}

func deleteDaemonRecord(homeDir string, port int) error {
	err := os.Remove(daemonRecordPath(homeDir, port))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// processAlive returns true when a process with the given pid exists and can
// receive signals. Uses signal 0 (no-op) which is the portable way to probe
// process liveness on POSIX without actually affecting the target.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; Signal 0 is the real check.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
