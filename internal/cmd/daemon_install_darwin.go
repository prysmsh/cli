package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/prysmsh/cli/internal/meshd"
	"github.com/prysmsh/cli/internal/style"
)

const (
	launchdLabel    = "sh.prysm.daemon"
	launchdPlistDir = "/Library/LaunchDaemons"
)

func installDaemon(prysmBin string) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>daemon</string>
        <string>run</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>StandardOutPath</key>
    <string>%s/meshd.log</string>
    <key>StandardErrorPath</key>
    <string>%s/meshd.log</string>
    <key>WorkingDirectory</key>
    <string>%s</string>
    <key>SoftResourceLimits</key>
    <dict>
        <key>NumberOfFiles</key>
        <integer>4096</integer>
    </dict>
</dict>
</plist>
`, launchdLabel, prysmBin, daemonLogDir, daemonLogDir, daemonStateDir)

	plistPath := filepath.Join(launchdPlistDir, launchdLabel+".plist")
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	_ = exec.Command("launchctl", "bootout", "system/"+launchdLabel).Run()

	if out, err := exec.Command("launchctl", "bootstrap", "system", plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %s: %w", string(out), err)
	}

	fmt.Println(style.Success.Render("Prysm mesh daemon installed and started"))
	fmt.Printf("  Plist:  %s\n", plistPath)
	fmt.Printf("  Socket: %s\n", meshd.SocketPath)
	fmt.Printf("  Log:    %s/meshd.log\n", daemonLogDir)
	return nil
}

func uninstallDaemon() error {
	_ = exec.Command("launchctl", "bootout", "system/"+launchdLabel).Run()

	plistPath := filepath.Join(launchdPlistDir, launchdLabel+".plist")
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	_ = os.Remove(meshd.SocketPath)

	fmt.Println(style.Success.Render("Prysm mesh daemon uninstalled"))
	return nil
}
