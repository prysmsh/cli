package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
)

func newSessionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Inspect and replay recorded access sessions",
	}

	cmd.AddCommand(
		newSessionsListCommand(),
		newSessionsShowCommand(),
		newSessionsReplayCommand(),
	)

	return cmd
}

func newSessionsListCommand() *cobra.Command {
	var (
		status       string
		resourceType string
		sessionType  string
		user         string
		limit        int
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List access sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			sessions, err := app.API.ListAccessSessions(ctx, api.AccessSessionListOptions{
				Status:       strings.TrimSpace(status),
				ResourceType: strings.TrimSpace(resourceType),
				Type:         strings.TrimSpace(sessionType),
				User:         strings.TrimSpace(user),
				Limit:        limit,
			})
			if err != nil {
				return err
			}

			if wantsJSONOutput(outputFormat) {
				return writeJSON(map[string]interface{}{
					"sessions": sessions,
					"count":    len(sessions),
				})
			}

			if len(sessions) == 0 {
				fmt.Println(style.Warning.Render("No sessions found."))
				return nil
			}

			headers := []string{"ID", "STATUS", "TYPE", "RESOURCE", "USER", "STARTED", "DURATION"}
			rows := make([][]string, 0, len(sessions))
			for _, s := range sessions {
				rows = append(rows, []string{
					valueOrDash(s.Identifier()),
					colorRequestStatus(s.Status),
					valueOrDash(firstNonEmpty(s.Type, s.Protocol)),
					valueOrDash(s.Resource),
					valueOrDash(s.User),
					formatTime(s.StartedAt),
					formatDurationSeconds(s.DurationSeconds),
				})
			}
			ui.PrintTable(headers, rows)
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "filter by status")
	cmd.Flags().StringVar(&resourceType, "resource-type", "", "filter by resource type")
	cmd.Flags().StringVar(&sessionType, "type", "", "filter by session type/protocol")
	cmd.Flags().StringVar(&user, "user", "", "filter by username/email")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum records to return")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json)")
	return cmd
}

func newSessionsShowCommand() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show details for one access session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			session, err := app.API.GetAccessSession(ctx, strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}

			if wantsJSONOutput(outputFormat) {
				return writeJSON(session)
			}

			printSessionDetails(session)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json)")
	return cmd
}

func newSessionsReplayCommand() *cobra.Command {
	var (
		exportPath   string
		replayFormat string
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "replay <session-id>",
		Short: "Replay or export a recorded access session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			replay, err := app.API.ReplayAccessSession(ctx, strings.TrimSpace(args[0]), strings.TrimSpace(replayFormat))
			if err != nil {
				return err
			}

			if exportPath != "" {
				path := exportPath
				if !filepath.IsAbs(path) {
					if abs, absErr := filepath.Abs(path); absErr == nil {
						path = abs
					}
				}
				payloadErr := writeReplayExport(path, replay)
				if payloadErr != nil {
					return payloadErr
				}
				fmt.Println(style.Success.Render(fmt.Sprintf("Replay exported to %s", path)))
				return nil
			}

			if wantsJSONOutput(outputFormat) {
				return writeJSON(replay)
			}

			printReplayDetails(replay)
			return nil
		},
	}

	cmd.Flags().StringVar(&exportPath, "export", "", "write replay payload to file (JSON)")
	cmd.Flags().StringVar(&replayFormat, "format", "", "request replay format (e.g. events, transcript)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json)")
	return cmd
}

func printSessionDetails(session *api.AccessSession) {
	if session == nil {
		fmt.Println(style.Warning.Render("No session details available."))
		return
	}

	fmt.Printf("ID: %s\n", valueOrDash(session.Identifier()))
	fmt.Printf("Status: %s\n", valueOrDash(session.Status))
	fmt.Printf("Type: %s\n", valueOrDash(firstNonEmpty(session.Type, session.Protocol)))
	fmt.Printf("Resource: %s\n", valueOrDash(session.Resource))
	fmt.Printf("Resource Type: %s\n", valueOrDash(session.ResourceType))
	fmt.Printf("User: %s\n", valueOrDash(session.User))
	fmt.Printf("Reason: %s\n", valueOrDash(session.Reason))
	fmt.Printf("Request ID: %s\n", valueOrDash(session.RequestID))
	fmt.Printf("Started: %s\n", formatTime(session.StartedAt))
	fmt.Printf("Ended: %s\n", formatTime(session.EndedAt))
	fmt.Printf("Duration: %s\n", formatDurationSeconds(session.DurationSeconds))
}

func printReplayDetails(replay *api.SessionReplay) {
	if replay == nil {
		fmt.Println(style.Warning.Render("Replay data not available."))
		return
	}

	if replay.SessionID != "" {
		fmt.Printf("Session ID: %s\n", replay.SessionID)
	}
	if replay.Format != "" {
		fmt.Printf("Format: %s\n", replay.Format)
	}
	if replay.GeneratedAt != nil && !replay.GeneratedAt.IsZero() {
		fmt.Printf("Generated: %s\n", replay.GeneratedAt.UTC().Format(time.RFC3339))
	}
	if replay.DownloadURL != "" {
		fmt.Printf("Download URL: %s\n", replay.DownloadURL)
	}

	if strings.TrimSpace(replay.Transcript) != "" {
		fmt.Println()
		fmt.Println(style.Bold.Render("Transcript"))
		fmt.Println(strings.TrimSpace(replay.Transcript))
		return
	}

	if len(replay.Events) == 0 {
		fmt.Println(style.Warning.Render("No replay events available for this session."))
		return
	}

	headers := []string{"TIME", "ACTOR", "TYPE", "MESSAGE"}
	rows := make([][]string, 0, len(replay.Events))
	for _, event := range replay.Events {
		rows = append(rows, []string{
			formatTime(event.Timestamp),
			valueOrDash(event.Actor),
			valueOrDash(event.Type),
			valueOrDash(firstNonEmpty(event.Message, event.Command)),
		})
	}
	fmt.Println()
	ui.PrintTable(headers, rows)
}

func writeReplayExport(path string, replay *api.SessionReplay) error {
	if replay == nil {
		return fmt.Errorf("replay payload is empty")
	}
	payload, err := marshalIndentedJSON(replay)
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

func marshalIndentedJSON(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func formatDurationSeconds(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}
	return (time.Duration(seconds) * time.Second).String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
