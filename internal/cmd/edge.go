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

func newEdgeCommand() *cobra.Command {
	edgeCmd := &cobra.Command{
		Use:   "edge",
		Short: "Manage edge proxy domains, rules, and DNS",
	}

	edgeCmd.AddCommand(
		newEdgeAddCommand(),
		newEdgeRmCommand(),
		newEdgeListCommand(),
		newEdgeStatusCommand(),
		newEdgeUpstreamCommand(),
		newEdgeRuleCommand(),
		newEdgeRulesCommand(),
		newEdgeDNSCommand(),
	)

	return edgeCmd
}

func newEdgeAddCommand() *cobra.Command {
	var upstream string
	var clusterRef string
	var mode string

	cmd := &cobra.Command{
		Use:   "add <domain>",
		Short: "Register a domain for edge proxy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain := args[0]
			if upstream == "" {
				return fmt.Errorf("--upstream is required")
			}

			clusters, err := app.API.ListClusters(ctx)
			if err != nil {
				return fmt.Errorf("list clusters: %w", err)
			}
			cluster, err := findCluster(clusters, clusterRef)
			if err != nil {
				return fmt.Errorf("resolve cluster: %w", err)
			}

			resp, err := app.API.CreateEdgeDomain(ctx, domain, upstream, uint(cluster.ID), mode)
			if err != nil {
				return fmt.Errorf("create edge domain: %w", err)
			}

			fmt.Fprintf(os.Stderr, "%s Domain %s registered\n", style.Success.Render("ok:"), resp.Domain.Domain)
			fmt.Fprintf(os.Stderr, "\nSet your NS records to:\n")
			for _, ns := range resp.NSRecords {
				fmt.Fprintf(os.Stderr, "  %s\n", ns)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&upstream, "upstream", "", "upstream target (e.g. localhost:3000)")
	cmd.Flags().StringVar(&clusterRef, "cluster", "", "cluster name or ID")
	cmd.Flags().StringVar(&mode, "mode", "", "upstream mode: local or mesh (auto-detected if omitted)")
	_ = cmd.MarkFlagRequired("upstream")
	_ = cmd.MarkFlagRequired("cluster")
	return cmd
}

func newEdgeRmCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <domain>",
		Short: "Remove an edge proxy domain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			if err := app.API.DeleteEdgeDomain(ctx, domain.ID); err != nil {
				return fmt.Errorf("delete domain: %w", err)
			}

			fmt.Fprintf(os.Stderr, "%s Domain %s removed\n", style.Success.Render("ok:"), domain.Domain)
			return nil
		},
	}
}

func newEdgeListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all edge proxy domains",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domains, err := app.API.ListEdgeDomains(ctx)
			if err != nil {
				return fmt.Errorf("list domains: %w", err)
			}

			if len(domains) == 0 {
				fmt.Fprintln(os.Stderr, "No edge domains configured. Use `prysm edge add` to get started.")
				return nil
			}

			headers := []string{"DOMAIN", "UPSTREAM", "MODE", "STATUS"}
			data := make([][]string, len(domains))
			for i, d := range domains {
				data[i] = []string{d.Domain, d.UpstreamTarget, d.UpstreamMode, d.Status}
			}
			ui.PrintTable(headers, data)
			return nil
		},
	}
}

func newEdgeStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <domain>",
		Short: "Show edge proxy domain status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			status, err := app.API.GetEdgeDomainStatus(ctx, domain.ID)
			if err != nil {
				return fmt.Errorf("get status: %w", err)
			}

			d := status["domain"].(map[string]interface{})
			fmt.Fprintf(os.Stdout, "Domain:       %s\n", d["domain"])
			fmt.Fprintf(os.Stdout, "Status:       %s\n", d["status"])
			fmt.Fprintf(os.Stdout, "Upstream:     %s\n", d["upstream_target"])
			fmt.Fprintf(os.Stdout, "Mode:         %s\n", d["upstream_mode"])
			if certExp, ok := d["cert_expires_at"]; ok && certExp != nil {
				fmt.Fprintf(os.Stdout, "Cert Expires: %s\n", certExp)
			}
			fmt.Fprintf(os.Stdout, "Active Rules: %.0f\n", status["active_rules"])
			return nil
		},
	}
}

func newEdgeUpstreamCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "upstream <domain> <host:port>",
		Short: "Update upstream target for a domain",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			if err := app.API.UpdateEdgeDomainUpstream(ctx, domain.ID, args[1]); err != nil {
				return fmt.Errorf("update upstream: %w", err)
			}

			fmt.Fprintf(os.Stderr, "%s Upstream for %s updated to %s\n",
				style.Success.Render("ok:"), domain.Domain, args[1])
			return nil
		},
	}
}

// resolveEdgeDomain finds an edge domain by name from the user's org.
func resolveEdgeDomain(ctx context.Context, app *App, name string) (*edgeDomainRef, error) {
	domains, err := app.API.ListEdgeDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}

	lower := strings.ToLower(name)
	for _, d := range domains {
		if strings.ToLower(d.Domain) == lower {
			return &edgeDomainRef{ID: d.ID, Domain: d.Domain}, nil
		}
	}
	return nil, fmt.Errorf("edge domain %q not found", name)
}

type edgeDomainRef struct {
	ID     uint
	Domain string
}
