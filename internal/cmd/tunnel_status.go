package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
)

func newTunnelStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show locally-running tunnel daemons and their backend state",
		Long: `Lists tunnel daemons spawned via ` + "`tunnel expose --background`" + `, plus their
process liveness and backend status (when the tunnel ID has been recorded).

A stale record with pid=not-running but still on disk means the daemon
crashed without cleaning up — safe to ignore; the backend reaper will mark
the row expired within a few minutes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			records, err := listDaemonRecords(app.Config.HomeDir)
			if err != nil {
				return fmt.Errorf("list daemon records: %w", err)
			}
			if len(records) == 0 {
				fmt.Println(style.Warning.Render("No background tunnels."))
				fmt.Println(style.MutedStyle.Render("Start one: prysm tunnel expose <port> --background"))
				return nil
			}

			sort.Slice(records, func(i, j int) bool {
				return records[i].Port < records[j].Port
			})

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			// Backend-side lookup is one list call; correlate by tunnel ID so
			// we can show "active" / "expired" / etc. alongside local state.
			backendByID := map[int64]string{}
			if tunnels, err := app.API.ListTunnels(ctx, ""); err == nil {
				for _, t := range tunnels {
					backendByID[t.ID] = t.Status
				}
			}

			fmt.Printf("%-6s %-8s %-10s %-10s %-10s %s\n", "PORT", "PID", "PROCESS", "TUNNEL ID", "BACKEND", "AGE")
			for _, r := range records {
				procState := style.Success.Render("running")
				if !processAlive(r.PID) {
					procState = style.Error.Render("stopped")
				}

				tunnelIDStr := "—"
				backendState := style.MutedStyle.Render("pending")
				if r.TunnelID > 0 {
					tunnelIDStr = fmt.Sprintf("%d", r.TunnelID)
					if state, ok := backendByID[r.TunnelID]; ok {
						backendState = renderBackendState(state)
					} else {
						backendState = style.Error.Render("missing")
					}
				}

				fmt.Printf("%-6d %-8d %-10s %-10s %-10s %s\n",
					r.Port,
					r.PID,
					procState,
					tunnelIDStr,
					backendState,
					time.Since(r.StartedAt).Round(time.Second),
				)
			}
			return nil
		},
	}
}

func renderBackendState(s string) string {
	switch s {
	case "active":
		return style.Success.Render(s)
	case "pending":
		return style.Info.Render(s)
	case "expired", "error", "disabled":
		return style.Error.Render(s)
	default:
		return s
	}
}

func newTunnelLogsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <port>",
		Short: "Print the log file for a background tunnel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var port int
			if _, err := fmt.Sscanf(args[0], "%d", &port); err != nil || port <= 0 {
				return fmt.Errorf("invalid port %q", args[0])
			}

			app := MustApp()
			path := daemonLogPath(app.Config.HomeDir, port)
			f, err := os.Open(path)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no log for port %d at %s", port, path)
				}
				return err
			}
			defer f.Close()
			_, err = io.Copy(os.Stdout, f)
			return err
		},
	}
}
