package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func newMeshExitCommand() *cobra.Command {
	exitCmd := &cobra.Command{
		Use:   "exit",
		Short: "Manage exit nodes",
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
