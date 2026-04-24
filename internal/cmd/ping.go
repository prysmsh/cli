package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
)

func newPingCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ping <host>",
		Short: "Ping a cluster, mesh peer, or overlay IP through the WireGuard mesh",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			target := args[0]

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			overlayIP := target
			if net.ParseIP(target) == nil {
				resolved, err := resolveOverlayIP(ctx, app, target)
				if err != nil {
					return err
				}
				overlayIP = resolved
				fmt.Fprintf(os.Stderr, "%s resolved %s -> %s\n", style.Info.Render("info:"), target, overlayIP)
			}

			if !hasWireGuardInterface() {
				return fmt.Errorf("no WireGuard interface found — run %s first", style.Bold.Render("prysm mesh connect"))
			}

			pingCmd := exec.CommandContext(cmd.Context(), "ping", "-c", "4", overlayIP)
			pingCmd.Stdout = os.Stdout
			pingCmd.Stderr = os.Stderr
			return pingCmd.Run()
		},
	}

	return cmd
}

// resolveClusterOverlayIP finds a cluster by name and returns its WireGuard overlay CIDR host address.
func resolveClusterOverlayIP(ctx context.Context, app *App, name string) (string, error) {
	clusters, err := app.API.ListClusters(ctx)
	if err != nil {
		return "", fmt.Errorf("list clusters: %w", err)
	}

	cluster, err := findCluster(clusters, name)
	if err != nil {
		return "", err
	}

	return resolveClusterNodeIP(ctx, app, uint(cluster.ID), fmt.Sprintf("%d", cluster.ID))
}

// resolveClusterNodeIP finds the WireGuard overlay address for a cluster via mesh nodes.
func resolveClusterNodeIP(ctx context.Context, app *App, clusterID uint, clusterPublicID string) (string, error) {
	nodes, err := app.API.ListMeshNodes(ctx)
	if err != nil {
		return "", fmt.Errorf("list mesh nodes: %w", err)
	}

	cid := int64(clusterID)
	for _, n := range nodes {
		if n.PeerType != "cluster" || n.WGAddress == "" {
			continue
		}
		if n.DeviceID == clusterPublicID || (n.ClusterID != nil && *n.ClusterID == cid) {
			ip := strings.Split(n.WGAddress, "/")[0]
			return ip, nil
		}
	}

	return "", fmt.Errorf("no WireGuard address found for cluster %s — is the agent running?", clusterPublicID)
}

// resolveOverlayIP resolves a name to a WireGuard overlay IP by searching mesh nodes.
// It matches against device IDs (with optional -cli suffix) and prefers connected nodes.
func resolveOverlayIP(ctx context.Context, app *App, name string) (string, error) {
	nodes, err := app.API.ListMeshNodes(ctx)
	if err != nil {
		return "", fmt.Errorf("list mesh nodes: %w", err)
	}

	lower := strings.ToLower(name)
	// First pass: find a connected mesh peer matching the name.
	for _, n := range nodes {
		if n.WGAddress == "" || n.Status != "connected" {
			continue
		}
		did := strings.ToLower(n.DeviceID)
		if did == lower || did == lower+"-cli" {
			return strings.Split(n.WGAddress, "/")[0], nil
		}
	}

	// Try cluster lookup (matches cluster name, resolves via mesh nodes).
	if resolved, err := resolveClusterOverlayIP(ctx, app, name); err == nil {
		return resolved, nil
	}

	// Last resort: any mesh peer matching the name, even if disconnected.
	for _, n := range nodes {
		if n.WGAddress == "" {
			continue
		}
		did := strings.ToLower(n.DeviceID)
		if did == lower || did == lower+"-cli" {
			return strings.Split(n.WGAddress, "/")[0], nil
		}
	}

	return "", fmt.Errorf("no mesh peer or cluster found matching %q — check %s", name, style.Bold.Render("prysm mesh peers"))
}

// hasWireGuardInterface checks whether any WireGuard utun interface exists.
func hasWireGuardInterface() bool {
	// Check for UAPI socket (created by both embedded and external wireguard-go).
	if matches, err := os.ReadDir("/var/run/wireguard"); err == nil {
		for _, m := range matches {
			if strings.HasSuffix(m.Name(), ".sock") {
				return true
			}
		}
	}
	// Fallback: check if any utun interface has a wireguard-go socket.
	out, err := exec.Command("ifconfig", "-l").Output()
	if err != nil {
		return false
	}
	for _, iface := range strings.Fields(string(out)) {
		if strings.HasPrefix(iface, "utun") {
			return true
		}
	}
	return false
}
