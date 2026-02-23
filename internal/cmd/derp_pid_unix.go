//go:build unix

package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// readDerpPidAndCheckRunning reads ~/.prysm/derp-connect.pid and returns the PID
// and whether that process is still running. Only defined on Unix.
func readDerpPidAndCheckRunning() (pid int, running bool) {
	home := getPrysmHome()
	path := filepath.Join(home, derpConnectPidFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	err = unix.Kill(pid, 0)
	if err == nil || err == unix.EPERM {
		// EPERM means the process exists but we lack permission to signal it.
		return pid, true
	}
	return pid, false
}
