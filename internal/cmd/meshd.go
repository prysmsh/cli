package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/warp-run/prysm-meshd"

	"github.com/warp-run/prysm-cli/internal/daemon"
)

func newMeshdCommand() *cobra.Command {
	var (
		socketPath string
		stateDir   string
	)

	cmd := &cobra.Command{
		Use:   "meshd",
		Short: "Run the mesh networking daemon",
		Long: `Start the Prysm mesh daemon which manages WireGuard tunnel interfaces.

The daemon runs as a background service and accepts control commands via a Unix socket.
It requires elevated privileges to manage network interfaces.

Example:
  sudo prysm meshd --socket /var/run/prysm/meshd.sock

For production use, configure it as a systemd service (see docs/MESH_NETWORKING_QUICKSTART.md).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if socketPath == "" {
				return fmt.Errorf("socket path must be provided")
			}

			// Check if we have necessary privileges
			if os.Geteuid() != 0 {
				log.Println("⚠️  Warning: Running without root privileges. Network interface management may fail.")
				log.Println("   Consider running with sudo: sudo prysm meshd")
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			srv, err := meshd.NewServer(meshd.Config{
				SocketPath: socketPath,
				StateDir:   stateDir,
			})
			if err != nil {
				return err
			}

			go func() {
				sigs := make(chan os.Signal, 1)
				signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
				sig := <-sigs
				log.Printf("Received signal %s, shutting down gracefully...", sig)
				cancel()
				srv.Close()
			}()

			log.Printf("🚀 Prysm mesh daemon starting on %s", socketPath)
			if err := srv.Run(ctx); err != nil {
				return err
			}
			log.Println("✅ Mesh daemon shut down cleanly")
			return nil
		},
	}

	cmd.Flags().StringVar(&socketPath, "socket", daemon.DefaultSocket(), "Unix domain socket path")
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "Optional persistent state directory")

	return cmd
}
