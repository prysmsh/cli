package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
)

func newHostsCommand() *cobra.Command {
	hostsCmd := &cobra.Command{
		Use:     "hosts",
		Aliases: []string{"host"},
		Short:   "Manage standalone hosts",
	}

	hostsCmd.AddCommand(
		newHostsListCommand(),
	)

	return hostsCmd
}

func newHostsListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered hosts",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			data, err := fetchClusterList(ctx, app)
			if err != nil {
				return err
			}

			hosts := filterByType(data, "docker", "host")
			if len(hosts) == 0 {
				fmt.Println("No hosts registered. Use 'prysm install --ssh <user@host>' to add one.")
				return nil
			}

			printHostTable(hosts)
			return nil
		},
	}
}

type clusterEntry struct {
	ID             uint   `json:"id"`
	PublicID       string `json:"public_id"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	AgentType      string `json:"agent_type"`
	Region         string `json:"region"`
	NodeCount      int    `json:"node_count"`
	PodCount       int    `json:"pod_count"`
	ServiceCount   int    `json:"service_count"`
}

func fetchClusterList(ctx context.Context, app *App) ([]clusterEntry, error) {
	var data struct {
		Clusters []clusterEntry `json:"clusters"`
	}
	resp, err := app.API.Do(ctx, "GET", "clusters", nil, &data)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	if resp != nil && resp.StatusCode >= 400 {
		return nil, fmt.Errorf("list clusters: %s", resp.Status)
	}
	return data.Clusters, nil
}

func filterByType(entries []clusterEntry, types ...string) []clusterEntry {
	var result []clusterEntry
	for _, e := range entries {
		t := e.AgentType
		if t == "" {
			t = "kubernetes"
		}
		for _, want := range types {
			if t == want {
				result = append(result, e)
				break
			}
		}
	}
	return result
}

func printClusterTable(clusters []clusterEntry) {
	headers := []string{"PUBLIC ID", "NAME", "STATUS", "REGION", "NODES", "PODS", "SVCS"}
	rows := make([][]string, 0, len(clusters))
	for _, c := range clusters {
		statusStr := style.Error.Render(c.Status)
		if c.Status == "connected" {
			statusStr = style.Success.Render(c.Status)
		}
		pid := c.PublicID
		if pid == "" {
			pid = fmt.Sprintf("(id:%d)", c.ID)
		}
		rows = append(rows, []string{
			pid,
			truncate(c.Name, 30),
			statusStr,
			c.Region,
			fmt.Sprintf("%d", c.NodeCount),
			fmt.Sprintf("%d", c.PodCount),
			fmt.Sprintf("%d", c.ServiceCount),
		})
	}
	ui.PrintTable(headers, rows)
}

func printHostTable(hosts []clusterEntry) {
	headers := []string{"PUBLIC ID", "NAME", "STATUS"}
	rows := make([][]string, 0, len(hosts))
	for _, h := range hosts {
		statusStr := style.Error.Render(h.Status)
		if h.Status == "connected" {
			statusStr = style.Success.Render(h.Status)
		}
		pid := h.PublicID
		if pid == "" {
			pid = fmt.Sprintf("(id:%d)", h.ID)
		}
		rows = append(rows, []string{
			pid,
			truncate(h.Name, 30),
			statusStr,
		})
	}
	ui.PrintTable(headers, rows)
}
