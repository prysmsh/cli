package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/prysmsh/cli/internal/meshd"
	"github.com/prysmsh/cli/internal/style"
)

func installDaemon(prysmBin string) error {
	unit := fmt.Sprintf(`[Unit]
Description=Prysm Mesh Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s daemon run
Restart=on-failure
RestartSec=5
RuntimeDirectory=prysm
RuntimeDirectoryMode=0755

[Install]
WantedBy=multi-user.target
`, prysmBin)

	if err := os.WriteFile("/etc/systemd/system/prysm-meshd.service", []byte(unit), 0644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}

	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %s: %w", strings.TrimSpace(string(out)), err)
	}

	if out, err := exec.Command("systemctl", "enable", "--now", "prysm-meshd").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable: %s: %w", strings.TrimSpace(string(out)), err)
	}

	fmt.Println(style.Success.Render("Prysm mesh daemon installed and started"))
	fmt.Printf("  Unit:   /etc/systemd/system/prysm-meshd.service\n")
	fmt.Printf("  Socket: %s\n", meshd.SocketPath)
	fmt.Printf("  Log:    journalctl -u prysm-meshd\n")
	return nil
}

func uninstallDaemon() error {
	_ = exec.Command("systemctl", "disable", "--now", "prysm-meshd").Run()

	if err := os.Remove("/etc/systemd/system/prysm-meshd.service"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit: %w", err)
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = os.Remove(meshd.SocketPath)

	fmt.Println(style.Success.Render("Prysm mesh daemon uninstalled"))
	return nil
}
