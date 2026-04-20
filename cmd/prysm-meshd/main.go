package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/prysmsh/cli/internal/meshd"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if os.Getuid() != 0 {
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
