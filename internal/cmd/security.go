package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/util"
)

func newSecurityCommand() *cobra.Command {
	securityCmd := &cobra.Command{
		Use:     "security",
		Aliases: []string{"sec"},
		Short:   "Security scanning, vulnerabilities, and compliance",
	}

	securityCmd.AddCommand(
		newSecuritySummaryCommand(),
		newSecurityVulnsCommand(),
		newSecurityComplianceCommand(),
	)

	return securityCmd
}

func newSecuritySummaryCommand() *cobra.Command {
	var clusterID string

	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Show security summary (unique CVEs by severity)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			endpoint := "security/compliance/summary"
			if clusterID != "" {
				if err := util.SafePathSegment(clusterID); err != nil {
					return fmt.Errorf("invalid cluster ID: %w", err)
				}
				endpoint += "?cluster_id=" + url.QueryEscape(clusterID)
			}

			var data struct {
				Total             int64 `json:"total"`
				Critical          int64 `json:"critical"`
				High              int64 `json:"high"`
				Medium            int64 `json:"medium"`
				Low               int64 `json:"low"`
				Compliant         int64 `json:"compliant"`
				NonCompliant      int64 `json:"non_compliant"`
				AffectedInstances int64 `json:"affected_instances"`
			}
			resp, err := app.API.Do(ctx, "GET", endpoint, nil, &data)
			if err != nil {
				return fmt.Errorf("get security summary: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("get security summary: %s", resp.Status)
			}

			bold := color.New(color.Bold)
			bold.Println("Security Summary (Unique CVEs)")
			fmt.Println(strings.Repeat("-", 40))

			// Critical
			fmt.Printf("Critical:  ")
			if data.Critical > 0 {
				color.New(color.FgRed, color.Bold).Printf("%d\n", data.Critical)
			} else {
				color.Green("0\n")
			}

			// High
			fmt.Printf("High:      ")
			if data.High > 0 {
				color.New(color.FgYellow).Printf("%d\n", data.High)
			} else {
				color.Green("0\n")
			}

			// Medium
			fmt.Printf("Medium:    ")
			if data.Medium > 0 {
				color.New(color.FgCyan).Printf("%d\n", data.Medium)
			} else {
				color.Green("0\n")
			}

			// Low
			fmt.Printf("Low:       %d\n", data.Low)

			fmt.Println(strings.Repeat("-", 40))
			bold.Printf("Total:     %d unique CVEs\n", data.Total)
			fmt.Printf("Instances: %d affected containers\n", data.AffectedInstances)

			return nil
		},
	}

	cmd.Flags().StringVarP(&clusterID, "cluster", "c", "", "Filter by cluster ID")
	return cmd
}

func newSecurityVulnsCommand() *cobra.Command {
	vulnsCmd := &cobra.Command{
		Use:     "vulns",
		Aliases: []string{"vulnerabilities", "cves"},
		Short:   "Manage vulnerabilities",
	}

	vulnsCmd.AddCommand(
		newVulnsListCommand(),
		newVulnsGetCommand(),
	)

	return vulnsCmd
}

func newVulnsListCommand() *cobra.Command {
	var (
		clusterID string
		severity  string
		namespace string
		limit     int
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List vulnerabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			v := url.Values{}
			if clusterID != "" {
				if err := util.SafePathSegment(clusterID); err != nil {
					return fmt.Errorf("invalid cluster ID: %w", err)
				}
				v.Set("cluster_id", clusterID)
			}
			if severity != "" {
				v.Set("severity", strings.ToUpper(severity))
			}
			if namespace != "" {
				v.Set("namespace", namespace)
			}
			v.Set("page_size", fmt.Sprintf("%d", limit))

			endpoint := "security/vulnerabilities"
			if q := v.Encode(); q != "" {
				endpoint += "?" + q
			}

			var data struct {
				Vulnerabilities []struct {
					ID               uint    `json:"id"`
					VulnerabilityID  string  `json:"vulnerability_id"`
					Severity         string  `json:"severity"`
					PackageName      string  `json:"package_name"`
					InstalledVersion string  `json:"installed_version"`
					FixedVersion     string  `json:"fixed_version"`
					Title            string  `json:"title"`
					Namespace        string  `json:"namespace"`
					PodName          string  `json:"pod_name"`
					ImageName        string  `json:"image_name"`
					CVSSv3Score      float64 `json:"cvss_v3_score"`
					LastSeen         string  `json:"last_seen"`
				} `json:"vulnerabilities"`
				Total    int64 `json:"total"`
				Page     int   `json:"page"`
				PageSize int   `json:"page_size"`
			}
			resp, err := app.API.Do(ctx, "GET", endpoint, nil, &data)
			if err != nil {
				return fmt.Errorf("list vulnerabilities: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("list vulnerabilities: %s", resp.Status)
			}

			if len(data.Vulnerabilities) == 0 {
				color.Green("No vulnerabilities found.\n")
				return nil
			}

			bold := color.New(color.Bold)
			bold.Printf("Vulnerabilities (%d total, showing %d)\n", data.Total, len(data.Vulnerabilities))
			fmt.Println(strings.Repeat("-", 100))
			bold.Printf("%-18s %-10s %-5s %-25s %-20s %-15s\n", "CVE", "SEVERITY", "CVSS", "PACKAGE", "NAMESPACE", "FIXED")
			fmt.Println(strings.Repeat("-", 100))

			for _, v := range data.Vulnerabilities {
				sevColor := color.FgWhite
				switch strings.ToUpper(v.Severity) {
				case "CRITICAL":
					sevColor = color.FgRed
				case "HIGH":
					sevColor = color.FgYellow
				case "MEDIUM":
					sevColor = color.FgCyan
				}

				fmt.Printf("%-18s ", truncate(v.VulnerabilityID, 18))
				color.New(sevColor).Printf("%-10s ", v.Severity)
				fmt.Printf("%-5.1f %-25s %-20s %-15s\n",
					v.CVSSv3Score,
					truncate(v.PackageName, 25),
					truncate(v.Namespace, 20),
					truncate(v.FixedVersion, 15))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&clusterID, "cluster", "c", "", "Filter by cluster ID")
	cmd.Flags().StringVarP(&severity, "severity", "s", "", "Filter by severity (CRITICAL, HIGH, MEDIUM, LOW)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Filter by namespace")
	cmd.Flags().IntVarP(&limit, "limit", "l", 50, "Number of results to show")
	return cmd
}

func newVulnsGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get [cve-id]",
		Short: "Get details of a specific vulnerability",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			cveID := args[0]
			if err := util.SafePathSegment(cveID); err != nil {
				return fmt.Errorf("invalid CVE ID: %w", err)
			}

			var data struct {
				ID               uint    `json:"id"`
				VulnerabilityID  string  `json:"vulnerability_id"`
				Severity         string  `json:"severity"`
				PackageName      string  `json:"package_name"`
				InstalledVersion string  `json:"installed_version"`
				FixedVersion     string  `json:"fixed_version"`
				Title            string  `json:"title"`
				Description      string  `json:"description"`
				Namespace        string  `json:"namespace"`
				PodName          string  `json:"pod_name"`
				ContainerName    string  `json:"container_name"`
				ImageName        string  `json:"image_name"`
				CVSSv3Score      float64 `json:"cvss_v3_score"`
				CVSSv3Vector     string  `json:"cvss_v3_vector"`
				PrimaryURL       string  `json:"primary_url"`
				FirstDetected    string  `json:"first_detected"`
				LastSeen         string  `json:"last_seen"`
			}
			resp, err := app.API.Do(ctx, "GET", fmt.Sprintf("security/vulnerabilities/%s", cveID), nil, &data)
			if err != nil {
				return fmt.Errorf("get vulnerability: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("get vulnerability: %s", resp.Status)
			}

			bold := color.New(color.Bold)
			bold.Printf("%s\n", data.VulnerabilityID)
			fmt.Println(strings.Repeat("-", 60))

			fmt.Printf("Severity:    ")
			switch strings.ToUpper(data.Severity) {
			case "CRITICAL":
				color.Red("%s\n", data.Severity)
			case "HIGH":
				color.Yellow("%s\n", data.Severity)
			case "MEDIUM":
				color.Cyan("%s\n", data.Severity)
			default:
				fmt.Printf("%s\n", data.Severity)
			}

			fmt.Printf("CVSS Score:  %.1f\n", data.CVSSv3Score)
			if data.CVSSv3Vector != "" {
				fmt.Printf("CVSS Vector: %s\n", data.CVSSv3Vector)
			}
			fmt.Println()

			bold.Println("Affected Package:")
			fmt.Printf("  Name:      %s\n", data.PackageName)
			fmt.Printf("  Installed: %s\n", data.InstalledVersion)
			fmt.Printf("  Fixed:     %s\n", data.FixedVersion)
			fmt.Println()

			bold.Println("Location:")
			fmt.Printf("  Namespace: %s\n", data.Namespace)
			fmt.Printf("  Pod:       %s\n", data.PodName)
			fmt.Printf("  Container: %s\n", data.ContainerName)
			fmt.Printf("  Image:     %s\n", data.ImageName)
			fmt.Println()

			if data.Title != "" {
				bold.Println("Title:")
				fmt.Printf("  %s\n", data.Title)
				fmt.Println()
			}

			if data.Description != "" {
				bold.Println("Description:")
				fmt.Printf("  %s\n", truncate(data.Description, 500))
				fmt.Println()
			}

			if data.PrimaryURL != "" {
				fmt.Printf("Reference:   %s\n", data.PrimaryURL)
			}
			fmt.Printf("First Seen:  %s\n", data.FirstDetected)
			fmt.Printf("Last Seen:   %s\n", data.LastSeen)

			return nil
		},
	}
}

func newSecurityComplianceCommand() *cobra.Command {
	var clusterID string

	cmd := &cobra.Command{
		Use:   "compliance",
		Short: "Show compliance status and trends",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			endpoint := "security/compliance/trends"
			if clusterID != "" {
				if err := util.SafePathSegment(clusterID); err != nil {
					return fmt.Errorf("invalid cluster ID: %w", err)
				}
				endpoint += "?cluster_id=" + url.QueryEscape(clusterID)
			}

			var data struct {
				Trends []struct {
					Date     string `json:"date"`
					Total    int64  `json:"total"`
					Critical int64  `json:"critical"`
					High     int64  `json:"high"`
				} `json:"trends"`
			}
			resp, err := app.API.Do(ctx, "GET", endpoint, nil, &data)
			if err != nil {
				return fmt.Errorf("get compliance trends: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("get compliance trends: %s", resp.Status)
			}

			if len(data.Trends) == 0 {
				fmt.Println("No compliance data available.")
				return nil
			}

			bold := color.New(color.Bold)
			bold.Println("Compliance Trends (Last 30 Days)")
			fmt.Println(strings.Repeat("-", 50))
			bold.Printf("%-12s %-10s %-10s %-10s\n", "DATE", "TOTAL", "CRITICAL", "HIGH")
			fmt.Println(strings.Repeat("-", 50))

			// Show last 10 days
			start := 0
			if len(data.Trends) > 10 {
				start = len(data.Trends) - 10
			}
			for _, t := range data.Trends[start:] {
				fmt.Printf("%-12s %-10d ", t.Date, t.Total)
				if t.Critical > 0 {
					color.New(color.FgRed).Printf("%-10d ", t.Critical)
				} else {
					fmt.Printf("%-10d ", t.Critical)
				}
				if t.High > 0 {
					color.New(color.FgYellow).Printf("%-10d\n", t.High)
				} else {
					fmt.Printf("%-10d\n", t.High)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&clusterID, "cluster", "c", "", "Filter by cluster ID")
	return cmd
}
