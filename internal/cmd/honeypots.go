package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func newHoneypotsCommand() *cobra.Command {
	honeypotsCmd := &cobra.Command{
		Use:     "honeypots",
		Aliases: []string{"honeypot", "hp"},
		Short:   "Manage honeypot deployments and view intrusion events",
	}

	honeypotsCmd.AddCommand(
		newHoneypotsStatusCommand(),
		newHoneypotsEventsCommand(),
		newHoneypotsDeployCommand(),
		newHoneypotsDisableCommand(),
		newHoneypotsTypesCommand(),
	)

	return honeypotsCmd
}

func newHoneypotsStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show honeypot deployment status across clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			var data struct {
				Configs []struct {
					ID                uint     `json:"id"`
					ClusterID         string   `json:"cluster_id"`
					ClusterName       string   `json:"cluster_name"`
					ClusterStatus     string   `json:"cluster_status"`
					Enabled           bool     `json:"enabled"`
					Profile           string   `json:"profile"`
					Status            string   `json:"status"`
					DeployedHoneypots []string `json:"deployed_honeypots"`
				} `json:"configs"`
			}
			resp, err := app.API.Do(ctx, "GET", "honeypots/configs", nil, &data)
			if err != nil {
				return fmt.Errorf("get honeypot configs: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("get honeypot configs: %s", resp.Status)
			}

			if len(data.Configs) == 0 {
				fmt.Println("No clusters found. Register a cluster first.")
				return nil
			}

			bold := color.New(color.Bold)
			bold.Println("Honeypot Deployments")
			fmt.Println(strings.Repeat("-", 90))
			bold.Printf("%-4s %-25s %-10s %-10s %-12s %-25s\n", "ID", "CLUSTER", "ENABLED", "STATUS", "PROFILE", "HONEYPOTS")
			fmt.Println(strings.Repeat("-", 90))

			for _, c := range data.Configs {
				enabledStr := "No"
				enabledColor := color.FgRed
				if c.Enabled {
					enabledStr = "Yes"
					enabledColor = color.FgGreen
				}

				statusColor := color.FgYellow
				if c.Status == "active" {
					statusColor = color.FgGreen
				} else if c.Status == "error" {
					statusColor = color.FgRed
				}

				honeypots := "-"
				if len(c.DeployedHoneypots) > 0 {
					honeypots = strings.Join(c.DeployedHoneypots, ", ")
					if len(honeypots) > 25 {
						honeypots = fmt.Sprintf("%d deployed", len(c.DeployedHoneypots))
					}
				}

				fmt.Printf("%-4s %-25s ", c.ClusterID, truncate(c.ClusterName, 25))
				color.New(enabledColor).Printf("%-10s ", enabledStr)
				color.New(statusColor).Printf("%-10s ", c.Status)
				fmt.Printf("%-12s %-25s\n", c.Profile, honeypots)
			}

			return nil
		},
	}
}

func newHoneypotsEventsCommand() *cobra.Command {
	var (
		clusterID string
		severity  string
		hpType    string
		limit     int
	)

	cmd := &cobra.Command{
		Use:   "events",
		Short: "List honeypot intrusion events",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			params := []string{}
			if clusterID != "" {
				params = append(params, "cluster_id="+clusterID)
			}
			if severity != "" {
				params = append(params, "severity="+severity)
			}
			if hpType != "" {
				params = append(params, "honeypot_type="+hpType)
			}
			params = append(params, fmt.Sprintf("page_size=%d", limit))

			endpoint := "honeypots/events"
			if len(params) > 0 {
				endpoint += "?" + strings.Join(params, "&")
			}

			var data struct {
				Events []struct {
					ID           uint   `json:"id"`
					Timestamp    string `json:"timestamp"`
					SourceIP     string `json:"source_ip"`
					SourcePort   int    `json:"source_port"`
					HoneypotType string `json:"honeypot_type"`
					DestPort     int    `json:"dest_port"`
					EventType    string `json:"event_type"`
					Username     string `json:"username"`
					Password     string `json:"password"`
					Command      string `json:"command"`
					Severity     string `json:"severity"`
				} `json:"events"`
				Total int64 `json:"total"`
			}
			resp, err := app.API.Do(ctx, "GET", endpoint, nil, &data)
			if err != nil {
				return fmt.Errorf("get honeypot events: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("get honeypot events: %s", resp.Status)
			}

			if len(data.Events) == 0 {
				fmt.Println("No honeypot events found.")
				return nil
			}

			bold := color.New(color.Bold)
			bold.Printf("Honeypot Events (%d total, showing %d)\n", data.Total, len(data.Events))
			fmt.Println(strings.Repeat("-", 100))
			bold.Printf("%-20s %-8s %-16s %-15s %-12s %-20s\n", "TIMESTAMP", "SEVERITY", "SOURCE IP", "HONEYPOT", "EVENT", "DETAILS")
			fmt.Println(strings.Repeat("-", 100))

			for _, e := range data.Events {
				sevColor := color.FgWhite
				switch strings.ToLower(e.Severity) {
				case "critical":
					sevColor = color.FgRed
				case "high":
					sevColor = color.FgYellow
				case "medium":
					sevColor = color.FgCyan
				}

				details := ""
				if e.Username != "" {
					details = fmt.Sprintf("user:%s", e.Username)
				} else if e.Command != "" {
					details = truncate(e.Command, 20)
				}

				ts := e.Timestamp
				if len(ts) > 19 {
					ts = ts[:19]
				}

				fmt.Printf("%-20s ", ts)
				color.New(sevColor).Printf("%-8s ", e.Severity)
				fmt.Printf("%-16s %-15s %-12s %-20s\n",
					e.SourceIP,
					truncate(e.HoneypotType, 15),
					truncate(e.EventType, 12),
					details)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&clusterID, "cluster", "c", "", "Filter by cluster ID")
	cmd.Flags().StringVarP(&severity, "severity", "s", "", "Filter by severity")
	cmd.Flags().StringVarP(&hpType, "type", "t", "", "Filter by honeypot type")
	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Number of events to show")
	return cmd
}

func newHoneypotsDeployCommand() *cobra.Command {
	var profile string

	cmd := &cobra.Command{
		Use:   "deploy [cluster-id]",
		Short: "Deploy honeypots to a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			clusterID := args[0]

			body := map[string]interface{}{
				"enabled": true,
				"profile": profile,
			}

			var result struct {
				Message string `json:"message"`
				Config  struct {
					ID      uint   `json:"id"`
					Status  string `json:"status"`
					Profile string `json:"profile"`
				} `json:"config"`
			}

			resp, err := app.API.Do(ctx, "PUT", fmt.Sprintf("honeypots/configs/%s", clusterID), body, &result)
			if err != nil {
				return fmt.Errorf("deploy honeypots: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("deploy honeypots: %s", resp.Status)
			}

			color.Green("✓ Honeypots deployment initiated for cluster %s\n", clusterID)
			fmt.Printf("  Profile: %s\n", result.Config.Profile)
			fmt.Printf("  Status:  %s\n", result.Config.Status)
			fmt.Println()
			fmt.Println("The agent will deploy honeypots on the next reconciliation cycle (usually within 2 minutes).")
			fmt.Println("Use 'prysm honeypots status' to check deployment progress.")

			return nil
		},
	}

	cmd.Flags().StringVarP(&profile, "profile", "p", "standard", "Honeypot profile (minimal, standard, full)")
	return cmd
}

func newHoneypotsDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable [cluster-id]",
		Short: "Disable honeypots on a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			clusterID := args[0]

			body := map[string]interface{}{
				"enabled": false,
			}

			resp, err := app.API.Do(ctx, "PUT", fmt.Sprintf("honeypots/configs/%s", clusterID), body, nil)
			if err != nil {
				return fmt.Errorf("disable honeypots: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("disable honeypots: %s", resp.Status)
			}

			color.Yellow("Honeypots disabled for cluster %s\n", clusterID)
			fmt.Println("The agent will remove honeypot deployments on the next reconciliation cycle.")

			return nil
		},
	}
}

func newHoneypotsTypesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "types",
		Short: "List available honeypot types",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			var data struct {
				Types []struct {
					Name        string   `json:"name"`
					Description string   `json:"description"`
					Ports       []int    `json:"ports"`
					Protocols   []string `json:"protocols"`
				} `json:"types"`
				Profiles map[string][]string `json:"profiles"`
			}
			resp, err := app.API.Do(ctx, "GET", "honeypots/types", nil, &data)
			if err != nil {
				return fmt.Errorf("get honeypot types: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("get honeypot types: %s", resp.Status)
			}

			bold := color.New(color.Bold)
			bold.Println("Available Honeypot Types")
			fmt.Println(strings.Repeat("-", 70))

			for _, t := range data.Types {
				bold.Printf("%s\n", t.Name)
				fmt.Printf("  %s\n", t.Description)
				if len(t.Ports) > 0 {
					ports := make([]string, len(t.Ports))
					for i, p := range t.Ports {
						ports[i] = fmt.Sprintf("%d", p)
					}
					fmt.Printf("  Ports: %s\n", strings.Join(ports, ", "))
				}
				fmt.Println()
			}

			if len(data.Profiles) > 0 {
				bold.Println("Deployment Profiles")
				fmt.Println(strings.Repeat("-", 70))
				for name, types := range data.Profiles {
					fmt.Printf("%-12s %s\n", name+":", strings.Join(types, ", "))
				}
			}

			return nil
		},
	}
}
