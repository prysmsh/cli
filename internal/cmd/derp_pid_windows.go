//go:build windows

package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// readDerpPidAndCheckRunning reads %USERPROFILE%\.prysm\derp-connect.pid and
// returns the PID. On Windows we do not verify the process is running;
// we only check that the file exists and parses, and return (pid, true)
// so DERP-only status can still be shown if the user ran mesh connect.
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
	return pid, true
}
