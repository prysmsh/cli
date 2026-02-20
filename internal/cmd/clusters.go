package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/util"
)

func newClustersCommand() *cobra.Command {
	clustersCmd := &cobra.Command{
		Use:     "clusters",
		Aliases: []string{"cluster", "cl"},
		Short:   "Manage Kubernetes clusters",
	}

	clustersCmd.AddCommand(
		newClustersListCommand(),
		newClustersStatusCommand(),
		newClustersTokenCommand(),
		newClustersExitCommand(),
	)

	return clustersCmd
}

func newClustersListCommand() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			var data struct {
				Clusters []struct {
					ID             uint   `json:"id"`
					Name           string `json:"name"`
					Status         string `json:"status"`
					Region         string `json:"region"`
					KubeVersion    string `json:"kube_version"`
					NodeCount      int    `json:"node_count"`
					PodCount       int    `json:"pod_count"`
					ServiceCount   int    `json:"service_count"`
					NamespaceCount int    `json:"namespace_count"`
					LastSeen       string `json:"last_seen"`
				} `json:"clusters"`
			}
			resp, err := app.API.Do(ctx, "GET", "clusters", nil, &data)
			if err != nil {
				return fmt.Errorf("list clusters: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("list clusters: %s", resp.Status)
			}

			if len(data.Clusters) == 0 {
				fmt.Println("No clusters registered. Deploy an agent to register a cluster.")
				return nil
			}

			// Print header
			bold := color.New(color.Bold)
			bold.Printf("%-4s %-30s %-12s %-10s %-6s %-6s %-6s\n", "ID", "NAME", "STATUS", "REGION", "NODES", "PODS", "SVCS")
			fmt.Println(strings.Repeat("-", 80))

			for _, c := range data.Clusters {
				statusColor := color.FgGreen
				if c.Status != "connected" {
					statusColor = color.FgRed
				}
				fmt.Printf("%-4d %-30s ", c.ID, truncate(c.Name, 30))
				color.New(statusColor).Printf("%-12s ", c.Status)
				fmt.Printf("%-10s %-6d %-6d %-6d\n", c.Region, c.NodeCount, c.PodCount, c.ServiceCount)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json)")
	return cmd
}

func newClustersStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status [cluster-id]",
		Short: "Show detailed status of a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			clusterID := args[0]
			if err := util.SafePathSegment(clusterID); err != nil {
				return fmt.Errorf("invalid cluster ID: %w", err)
			}

			var data struct {
				Cluster struct {
					ID             uint    `json:"id"`
					Name           string  `json:"name"`
					Status         string  `json:"status"`
					Region         string  `json:"region"`
					KubeVersion    string  `json:"kube_version"`
					NodeCount      int     `json:"node_count"`
					PodCount       int     `json:"pod_count"`
					ServiceCount   int     `json:"service_count"`
					NamespaceCount int     `json:"namespace_count"`
					CPUUsage       float64 `json:"cpu_usage"`
					MemoryUsage    float64 `json:"memory_usage"`
					LastSeen       string  `json:"last_seen"`
					CreatedAt      string  `json:"created_at"`
				} `json:"cluster"`
			}
			resp, err := app.API.Do(ctx, "GET", fmt.Sprintf("clusters/%s", clusterID), nil, &data)
			if err != nil {
				return fmt.Errorf("get cluster: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("get cluster: %s", resp.Status)
			}

			c := data.Cluster
			bold := color.New(color.Bold)

			bold.Printf("Cluster: %s\n", c.Name)
			fmt.Println(strings.Repeat("-", 40))
			fmt.Printf("ID:           %d\n", c.ID)
			fmt.Printf("Status:       ")
			if c.Status == "connected" {
				color.Green("%s\n", c.Status)
			} else {
				color.Red("%s\n", c.Status)
			}
			fmt.Printf("Region:       %s\n", c.Region)
			fmt.Printf("K8s Version:  %s\n", c.KubeVersion)
			fmt.Printf("Last Seen:    %s\n", c.LastSeen)
			fmt.Println()
			bold.Println("Resources:")
			fmt.Printf("  Nodes:      %d\n", c.NodeCount)
			fmt.Printf("  Pods:       %d\n", c.PodCount)
			fmt.Printf("  Services:   %d\n", c.ServiceCount)
			fmt.Printf("  Namespaces: %d\n", c.NamespaceCount)
			fmt.Println()
			bold.Println("Usage:")
			fmt.Printf("  CPU:        %.1f%%\n", c.CPUUsage)
			fmt.Printf("  Memory:     %.1f%%\n", c.MemoryUsage)

			return nil
		},
	}
}

func newClustersTokenCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "token [cluster-id]",
		Short: "Get or create agent token for a cluster",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			endpoint := "agent-tokens"
			if len(args) > 0 {
				if err := util.SafePathSegment(args[0]); err != nil {
					return fmt.Errorf("invalid cluster ID: %w", err)
				}
				endpoint = "agent-tokens?cluster_id=" + url.QueryEscape(args[0])
			}

			var data struct {
				Tokens []struct {
					ID        uint   `json:"id"`
					Name      string `json:"name"`
					Token     string `json:"token"`
					ClusterID *uint  `json:"cluster_id"`
					ExpiresAt string `json:"expires_at"`
					CreatedAt string `json:"created_at"`
				} `json:"tokens"`
			}
			resp, err := app.API.Do(ctx, "GET", endpoint, nil, &data)
			if err != nil {
				return fmt.Errorf("get tokens: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("get tokens: %s", resp.Status)
			}

			if len(data.Tokens) == 0 {
				fmt.Println("No agent tokens found.")
				return nil
			}

			for _, t := range data.Tokens {
				fmt.Printf("Name:       %s\n", t.Name)
				fmt.Printf("Token:      %s\n", t.Token)
				if t.ClusterID != nil {
					fmt.Printf("Cluster ID: %d\n", *t.ClusterID)
				}
				fmt.Printf("Expires:    %s\n", t.ExpiresAt)
				fmt.Println()
			}
			return nil
		},
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func newClustersExitCommand() *cobra.Command {
	exitCmd := &cobra.Command{
		Use:   "exit",
		Short: "Enable or disable a cluster as an exit router",
	}

	exitCmd.AddCommand(
		newClustersExitEnableCommand(),
		newClustersExitDisableCommand(),
	)

	return exitCmd
}

func newClustersExitEnableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <cluster>",
		Short: "Enable cluster as exit router (route client traffic through it)",
		Long:  "Enable the cluster as an exit node. Clients can then select it to route their traffic through this cluster.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, err := resolveClusterForExit(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if err := MustApp().API.EnableClusterExitRouter(cmd.Context(), cluster.ID); err != nil {
				return fmt.Errorf("enable exit router: %w", err)
			}

			color.New(color.FgGreen).Printf("✓ Exit router enabled for cluster %s (%d)\n", cluster.Name, cluster.ID)
			return nil
		},
	}
}

func newClustersExitDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <cluster>",
		Short: "Disable cluster as exit router",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, err := resolveClusterForExit(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if err := MustApp().API.DisableClusterExitRouter(cmd.Context(), cluster.ID); err != nil {
				return fmt.Errorf("disable exit router: %w", err)
			}

			color.New(color.FgGreen).Printf("✓ Exit router disabled for cluster %s (%d)\n", cluster.Name, cluster.ID)
			return nil
		},
	}
}

func resolveClusterForExit(ctx context.Context, ref string) (*api.Cluster, error) {
	clusters, err := MustApp().API.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	if len(clusters) == 0 {
		return nil, errors.New("no clusters available")
	}

	trimmed := strings.TrimSpace(ref)
	for _, c := range clusters {
		if strings.EqualFold(c.Name, trimmed) {
			return &c, nil
		}
	}
	if id, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		for _, c := range clusters {
			if c.ID == id {
				return &c, nil
			}
		}
	}
	return nil, fmt.Errorf("cluster %q not found", ref)
}
