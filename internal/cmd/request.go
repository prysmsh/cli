package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
)

func newRequestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "request",
		Aliases: []string{"requests", "req"},
		Short:   "Create and review access requests",
	}

	cmd.AddCommand(
		newRequestCreateCommand(),
		newRequestListCommand(),
		newRequestShowCommand(),
		newRequestApproveCommand(),
		newRequestDenyCommand(),
	)

	return cmd
}

func newRequestCreateCommand() *cobra.Command {
	var (
		resourceType string
		reason       string
		expiresIn    time.Duration
		auditFields  []string
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "create <resource>",
		Short: "Create a new access request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			resource := strings.TrimSpace(args[0])
			if resource == "" {
				return fmt.Errorf("resource is required")
			}
			if expiresIn <= 0 {
				return fmt.Errorf("--expires-in must be greater than zero")
			}

			parsedAuditFields, err := parseAuditFieldFlags(auditFields)
			if err != nil {
				return err
			}

			exp := time.Now().UTC().Add(expiresIn)
			req := api.AccessRequestCreateRequest{
				Resource:     resource,
				ResourceType: strings.TrimSpace(resourceType),
				Reason:       strings.TrimSpace(reason),
				ExpiresAt:    &exp,
				AuditFields:  parsedAuditFields,
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			created, err := app.API.CreateAccessRequest(ctx, req)
			if err != nil {
				return err
			}

			if wantsJSONOutput(outputFormat) {
				return writeJSON(created)
			}

			fmt.Println(style.Success.Render("Access request created."))
			printAccessRequestDetails(created)
			return nil
		},
	}

	cmd.Flags().StringVar(&resourceType, "resource-type", "ssh", "resource type (for policy evaluation)")
	cmd.Flags().StringVar(&reason, "reason", "", "required reason for requesting access")
	cmd.Flags().DurationVar(&expiresIn, "expires-in", 0, "required lifetime before request auto-expires (e.g. 30m, 2h)")
	cmd.Flags().StringSliceVar(&auditFields, "audit-field", nil, "additional audit fields (key=value); may be repeated")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json)")
	_ = cmd.MarkFlagRequired("reason")
	_ = cmd.MarkFlagRequired("expires-in")

	return cmd
}

func newRequestListCommand() *cobra.Command {
	var (
		status       string
		resourceType string
		mine         bool
		limit        int
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List access requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			requests, err := app.API.ListAccessRequests(ctx, api.AccessRequestListOptions{
				Status:       strings.TrimSpace(status),
				ResourceType: strings.TrimSpace(resourceType),
				Mine:         mine,
				Limit:        limit,
			})
			if err != nil {
				return err
			}

			if wantsJSONOutput(outputFormat) {
				return writeJSON(map[string]interface{}{
					"requests": requests,
					"count":    len(requests),
				})
			}

			if len(requests) == 0 {
				fmt.Println(style.Warning.Render("No access requests found."))
				return nil
			}

			headers := []string{"ID", "STATUS", "TYPE", "RESOURCE", "EXPIRES", "REQUESTED BY"}
			rows := make([][]string, 0, len(requests))
			for _, req := range requests {
				rows = append(rows, []string{
					req.Identifier(),
					colorRequestStatus(req.Status),
					valueOrDash(req.ResourceType),
					valueOrDash(req.Resource),
					formatTime(req.ExpiresAt),
					valueOrDash(req.RequestedBy),
				})
			}
			ui.PrintTable(headers, rows)
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "filter by status (pending, approved, denied, expired)")
	cmd.Flags().StringVar(&resourceType, "resource-type", "", "filter by resource type")
	cmd.Flags().BoolVar(&mine, "mine", false, "show only requests created by the current user")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum number of records to return")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json)")

	return cmd
}

func newRequestShowCommand() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "show <request-id>",
		Short: "Show one access request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			req, err := app.API.GetAccessRequest(ctx, strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}

			if wantsJSONOutput(outputFormat) {
				return writeJSON(req)
			}

			printAccessRequestDetails(req)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json)")
	return cmd
}

func newRequestApproveCommand() *cobra.Command {
	var (
		note         string
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "approve <request-id>",
		Short: "Approve an access request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			req, err := app.API.ApproveAccessRequest(ctx, strings.TrimSpace(args[0]), strings.TrimSpace(note))
			if err != nil {
				return err
			}

			if wantsJSONOutput(outputFormat) {
				return writeJSON(req)
			}

			fmt.Println(style.Success.Render("Access request approved."))
			printAccessRequestDetails(req)
			return nil
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "optional reviewer note for audit trail")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json)")
	return cmd
}

func newRequestDenyCommand() *cobra.Command {
	var (
		reason       string
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "deny <request-id>",
		Short: "Deny an access request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			req, err := app.API.DenyAccessRequest(ctx, strings.TrimSpace(args[0]), strings.TrimSpace(reason))
			if err != nil {
				return err
			}

			if wantsJSONOutput(outputFormat) {
				return writeJSON(req)
			}

			fmt.Println(style.Success.Render("Access request denied."))
			printAccessRequestDetails(req)
			return nil
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "required reason for denying the request")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json)")
	_ = cmd.MarkFlagRequired("reason")

	return cmd
}

func parseAuditFieldFlags(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		parts := strings.SplitN(strings.TrimSpace(entry), "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("invalid --audit-field %q (expected key=value)", entry)
		}
		result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return result, nil
}

func printAccessRequestDetails(req *api.AccessRequest) {
	if req == nil {
		fmt.Println(style.Warning.Render("No request details available."))
		return
	}

	fmt.Printf("ID: %s\n", valueOrDash(req.Identifier()))
	fmt.Printf("Status: %s\n", valueOrDash(req.Status))
	fmt.Printf("Type: %s\n", valueOrDash(req.ResourceType))
	fmt.Printf("Resource: %s\n", valueOrDash(req.Resource))
	fmt.Printf("Reason: %s\n", valueOrDash(req.Reason))
	fmt.Printf("Expires: %s\n", formatTime(req.ExpiresAt))
	fmt.Printf("Requested By: %s\n", valueOrDash(req.RequestedBy))
	fmt.Printf("Reviewed By: %s\n", valueOrDash(req.ReviewedBy))
	fmt.Printf("Reviewed At: %s\n", formatTime(req.ReviewedAt))
	if len(req.AuditFields) > 0 {
		fmt.Println("Audit Fields:")
		for key, value := range req.AuditFields {
			fmt.Printf("  %s=%s\n", key, value)
		}
	}
}

func colorRequestStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "approved", "active":
		return style.Success.Render(status)
	case "pending":
		return style.Warning.Render(status)
	case "denied", "expired", "rejected":
		return style.Error.Render(status)
	default:
		return status
	}
}

func formatTime(ts *time.Time) string {
	if ts == nil || ts.IsZero() {
		return "-"
	}
	return ts.UTC().Format(time.RFC3339)
}

func valueOrDash(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	return v
}
