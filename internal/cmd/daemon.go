package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/prysmsh/cli/internal/meshd"
	"github.com/spf13/cobra"
)

const (
	daemonLogDir   = "/var/log/prysm"
	daemonRunDir   = "/var/run/prysm"
	daemonStateDir = "/var/lib/prysm"
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

	prysmBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(prysmBin); err == nil {
		prysmBin = resolved
	}

	for _, dir := range []string{daemonLogDir, daemonRunDir, daemonStateDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	return installDaemon(prysmBin)
}

func runDaemonUninstall(cmd *cobra.Command, args []string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("daemon uninstall requires root — run with sudo")
	}
	return uninstallDaemon()
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
