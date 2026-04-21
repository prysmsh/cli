package cmd

import (
	"os"
	"os/exec"
	"strings"
)

const trayAppPath = "/Applications/PrysmTray.app"

func launchTrayApp() {
	if _, err := os.Stat(trayAppPath); err != nil {
		return // not installed
	}
	// Check if already running.
	out, _ := exec.Command("pgrep", "-f", "PrysmTray").Output()
	if strings.TrimSpace(string(out)) != "" {
		return // already running
	}
	_ = exec.Command("open", "-a", trayAppPath).Start()
}
