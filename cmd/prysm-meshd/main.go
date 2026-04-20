package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/prysmsh/cli/internal/meshd"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// On Linux, prysm-meshd needs root for TUN device creation.
	// On macOS, the Network Extension handles the tunnel — meshd only needs
	// socket access (provided by the LaunchDaemon running as root).
	if os.Getuid() != 0 && runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, "prysm-meshd must run as root (use systemd or sudo)")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received %s, shutting down", sig)
		cancel()
	}()

	srv := meshd.NewServer(meshd.SocketPath)
	if err := srv.Serve(ctx); err != nil {
		log.Fatalf("meshd: %v", err)
	}
}
