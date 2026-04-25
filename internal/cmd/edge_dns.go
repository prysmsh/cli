package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
)

func newEdgeDNSCommand() *cobra.Command {
	dnsCmd := &cobra.Command{
		Use:   "dns",
		Short: "Manage DNS records for edge domains",
	}

	dnsCmd.AddCommand(
		newEdgeDNSAddCommand(),
		newEdgeDNSListCommand(),
		newEdgeDNSRmCommand(),
	)

	return dnsCmd
}

func newEdgeDNSAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add <domain> <type> <value>",
		Short: "Add a DNS record",
		Long:  "Add a DNS record to an edge domain. Supported types: A, AAAA, CNAME, MX, TXT",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			record, err := app.API.AddEdgeDNSRecord(ctx, domain.ID, strings.ToUpper(args[1]), args[2])
			if err != nil {
				return fmt.Errorf("add DNS record: %w", err)
			}

			fmt.Fprintf(os.Stderr, "%s %s record added to %s\n",
				style.Success.Render("ok:"), record.Type, domain.Domain)
			return nil
		},
	}
}

func newEdgeDNSListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list <domain>",
		Short: "List DNS records for a domain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			records, err := app.API.ListEdgeDNSRecords(ctx, domain.ID)
			if err != nil {
				return fmt.Errorf("list DNS records: %w", err)
			}

			if len(records) == 0 {
				fmt.Fprintln(os.Stderr, "No DNS records configured.")
				return nil
			}

			headers := []string{"ID", "TYPE", "VALUE"}
			data := make([][]string, len(records))
			for i, r := range records {
				data[i] = []string{r.ID, r.Type, r.Value}
			}
			ui.PrintTable(headers, data)
			return nil
		},
	}
}

func newEdgeDNSRmCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <domain> <record-id>",
		Short: "Remove a DNS record",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			if err := app.API.DeleteEdgeDNSRecord(ctx, domain.ID, args[1]); err != nil {
				return fmt.Errorf("delete DNS record: %w", err)
			}

			fmt.Fprintf(os.Stderr, "%s DNS record removed from %s\n",
				style.Success.Render("ok:"), domain.Domain)
			return nil
		},
	}
}
