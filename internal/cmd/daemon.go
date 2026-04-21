package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/prysmsh/cli/internal/meshd"
	"github.com/prysmsh/cli/internal/style"
	"github.com/spf13/cobra"
)

const (
	launchdLabel    = "sh.prysm.daemon"
	launchdPlistDir = "/Library/LaunchDaemons"
	daemonLogDir    = "/var/log/prysm"
	daemonRunDir    = "/var/run/prysm"
	daemonStateDir  = "/var/lib/prysm"
)

func newDaemonCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the Prysm mesh daemon",
		Long:  "The mesh daemon manages the WireGuard tunnel in the background. The CLI and tray app communicate with it via a Unix socket.",
	}

	runCmd := &cobra.Command{
		Use:    "run",
		Short:  "Run the daemon process (used by launchd)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath, _ := cmd.Flags().GetString("socket")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				sigs := make(chan os.Signal, 1)
				signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
				sig := <-sigs
				fmt.Fprintf(os.Stderr, "meshd: received %s, shutting down\n", sig)
				cancel()
			}()

			srv := meshd.NewServer(socketPath)
			return srv.Serve(ctx)
		},
	}
	runCmd.Flags().String("socket", meshd.SocketPath, "Unix domain socket path")

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install and start the mesh daemon",
		RunE:  runDaemonInstall,
	}

	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the mesh daemon",
		RunE:  runDaemonUninstall,
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE:  runDaemonStatus,
	}

	cmd.AddCommand(runCmd, installCmd, uninstallCmd, statusCmd)
	return cmd
}

func runDaemonInstall(cmd *cobra.Command, args []string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("daemon install requires root — run with sudo")
	}

	prysmBin, err := exec.LookPath("prysm")
	if err != nil {
		return fmt.Errorf("prysm binary not found in PATH: %w", err)
	}
	prysmBin, _ = filepath.Abs(prysmBin)
	// Resolve symlinks to get the actual binary path (survives upgrades via homebrew).
	if resolved, err := filepath.EvalSymlinks(prysmBin); err == nil {
		prysmBin = resolved
	}

	// Create directories.
	for _, dir := range []string{daemonLogDir, daemonRunDir, daemonStateDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	// Write launchd plist.
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

	// Unload first in case it's already loaded (ignore errors).
	_ = exec.Command("launchctl", "bootout", "system/"+launchdLabel).Run()

	// Load the daemon.
	if out, err := exec.Command("launchctl", "bootstrap", "system", plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %s: %w", string(out), err)
	}

	fmt.Println(style.Success.Render("Prysm mesh daemon installed and started"))
	fmt.Printf("  Plist:  %s\n", plistPath)
	fmt.Printf("  Socket: %s\n", meshd.SocketPath)
	fmt.Printf("  Log:    %s/meshd.log\n", daemonLogDir)
	return nil
}

func runDaemonUninstall(cmd *cobra.Command, args []string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("daemon uninstall requires root — run with sudo")
	}

	_ = exec.Command("launchctl", "bootout", "system/"+launchdLabel).Run()

	plistPath := filepath.Join(launchdPlistDir, launchdLabel+".plist")
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	// Clean up socket.
	_ = os.Remove(meshd.SocketPath)

	fmt.Println(style.Success.Render("Prysm mesh daemon uninstalled"))
	return nil
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	if !meshd.IsRunning() {
		fmt.Println("Daemon: not running")
		return nil
	}

	resp, err := meshd.GetStatus()
	if err != nil {
		return fmt.Errorf("query daemon: %w", err)
	}

	fmt.Printf("Daemon:    running\n")
	fmt.Printf("Status:    %s\n", resp.Status)
	if resp.OverlayIP != "" {
		fmt.Printf("Overlay:   %s\n", resp.OverlayIP)
	}
	if resp.Interface != "" {
		fmt.Printf("Interface: %s\n", resp.Interface)
	}
	if resp.PeerCount > 0 {
		fmt.Printf("Peers:     %d\n", resp.PeerCount)
	}
	if resp.Uptime > 0 {
		fmt.Printf("Uptime:    %ds\n", resp.Uptime)
	}
	return nil
}
