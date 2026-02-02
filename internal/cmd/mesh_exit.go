package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/derp"
)

func newMeshExitCommand() *cobra.Command {
	exitCmd := &cobra.Command{
		Use:   "exit",
		Short: "Manage exit nodes and exit preferences",
	}

	exitCmd.AddCommand(
		newMeshExitEnableCommand(),
		newMeshExitDisableCommand(),
	)

	return exitCmd
}

func newMeshExitEnableCommand() *cobra.Command {
	var nodeRef string

	cmd := &cobra.Command{
		Use:   "enable [node-id|device-id]",
		Short: "Enable a mesh node as an exit node (route traffic through it)",
		Long:  "Enable a mesh node as an exit node. Use node ID (numeric) or device ID. For devices, use the device_id from `prysm mesh peers`.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := nodeRef
			if len(args) > 0 {
				ref = args[0]
			}
			if strings.TrimSpace(ref) == "" {
				return errors.New("node-id or device-id is required")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			// Try numeric node ID first
			if nodeID, err := strconv.ParseInt(ref, 10, 64); err == nil {
				if err := app.API.EnableMeshNodeExit(ctx, nodeID); err != nil {
					return fmt.Errorf("enable exit node: %w", err)
				}
				color.New(color.FgGreen).Printf("✓ Exit node enabled for mesh node %d\n", nodeID)
				return nil
			}

			// Otherwise treat as device_id
			if err := app.API.SetMeshNodeExitByDeviceID(ctx, ref, true); err != nil {
				return fmt.Errorf("enable exit node: %w", err)
			}
			color.New(color.FgGreen).Printf("✓ Exit node enabled for device %s\n", ref)
			return nil
		},
	}

	cmd.Flags().StringVar(&nodeRef, "node", "", "mesh node ID or device ID")
	return cmd
}

func newMeshExitDisableCommand() *cobra.Command {
	var nodeRef string

	cmd := &cobra.Command{
		Use:   "disable [node-id|device-id]",
		Short: "Disable a mesh node as an exit node",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := nodeRef
			if len(args) > 0 {
				ref = args[0]
			}
			if strings.TrimSpace(ref) == "" {
				return errors.New("node-id or device-id is required")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if nodeID, err := strconv.ParseInt(ref, 10, 64); err == nil {
				if err := app.API.DisableMeshNodeExit(ctx, nodeID); err != nil {
					return fmt.Errorf("disable exit node: %w", err)
				}
				color.New(color.FgGreen).Printf("✓ Exit node disabled for mesh node %d\n", nodeID)
				return nil
			}

			if err := app.API.SetMeshNodeExitByDeviceID(ctx, ref, false); err != nil {
				return fmt.Errorf("disable exit node: %w", err)
			}
			color.New(color.FgGreen).Printf("✓ Exit node disabled for device %s\n", ref)
			return nil
		},
	}

	cmd.Flags().StringVar(&nodeRef, "node", "", "mesh node ID or device ID")
	return cmd
}


func newMeshExitPreferenceCommand() *cobra.Command {
	prefCmd := &cobra.Command{
		Use:   "exit-preference",
		Short: "Set which exit node to use for your WireGuard traffic",
	}

	prefCmd.AddCommand(
		newMeshExitPreferenceSetCommand(),
		newMeshExitPreferenceClearCommand(),
	)

	return prefCmd
}

func newMeshExitPreferenceSetCommand() *cobra.Command {
	var deviceID string

	cmd := &cobra.Command{
		Use:   "set <peer-id>",
		Short: "Set exit node preference (route your traffic through this peer)",
		Long:  "Set the exit node for your WireGuard device. Use peer ID from `prysm mesh peers` (exit-enabled nodes).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			peerID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid peer id: %w", err)
			}

			if strings.TrimSpace(deviceID) == "" {
				// Try to get from config or derp
				deviceID, err = getDefaultDeviceID()
				if err != nil {
					return fmt.Errorf("device-id required (run with --device-id or set in config): %w", err)
				}
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			pid := peerID
			if err := app.API.SetWireguardExitPreference(ctx, deviceID, &pid); err != nil {
				return fmt.Errorf("set exit preference: %w", err)
			}
			color.New(color.FgGreen).Printf("✓ Exit preference set to peer %d for device %s\n", peerID, deviceID)
			return nil
		},
	}

	cmd.Flags().StringVar(&deviceID, "device-id", "", "WireGuard device ID (from mesh enroll)")
	return cmd
}

func newMeshExitPreferenceClearCommand() *cobra.Command {
	var deviceID string

	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear exit node preference (use direct connection)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(deviceID) == "" {
				var err error
				deviceID, err = getDefaultDeviceID()
				if err != nil {
					return fmt.Errorf("device-id required (run with --device-id): %w", err)
				}
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.SetWireguardExitPreference(ctx, deviceID, nil); err != nil {
				return fmt.Errorf("clear exit preference: %w", err)
			}
			color.New(color.FgGreen).Printf("✓ Exit preference cleared for device %s\n", deviceID)
			return nil
		},
	}

	cmd.Flags().StringVar(&deviceID, "device-id", "", "WireGuard device ID")
	return cmd
}

func getDefaultDeviceID() (string, error) {
	if id := strings.TrimSpace(os.Getenv("PRYSM_DEVICE_ID")); id != "" {
		return id, nil
	}
	app := MustApp()
	return derp.EnsureDeviceID(app.Config.HomeDir)
}
