package main

import (
	"fmt"
	"os"

	"github.com/prysmsh/cli/internal/cmd"
	"github.com/prysmsh/cli/internal/style"
)

func init() {
	// Force Go's pure-Go DNS resolver instead of the cgo (system) resolver.
	// On macOS, the system resolver routes through Tailscale MagicDNS which
	// can be unreliable for external domains.
	if os.Getenv("GODEBUG") == "" {
		os.Setenv("GODEBUG", "netdns=go")
	}
}

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, style.Error.Render("Error: "+err.Error()))
		os.Exit(1)
	}
}
