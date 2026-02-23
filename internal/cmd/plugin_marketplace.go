package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
)

func newPluginMarketplaceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "marketplace",
		Short: "Discover and install marketplace plugins",
	}

	cmd.AddCommand(
		newPluginMarketplaceSearchCommand(),
		newPluginMarketplaceInfoCommand(),
		newPluginMarketplaceInstallCommand(),
		newPluginMarketplaceInstallsCommand(),
		newPluginMarketplaceRollbackCommand(),
		newPluginMarketplaceUninstallCommand(),
	)
	return cmd
}

func newPluginMarketplaceSearchCommand() *cobra.Command {
	var (
		query    string
		category string
		verified bool
	)
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search marketplace plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			params := []string{}
			if strings.TrimSpace(query) != "" {
				params = append(params, "search="+query)
			}
			if strings.TrimSpace(category) != "" {
				params = append(params, "category="+category)
			}
			if verified {
				params = append(params, "verified=true")
			}
			endpoint := "marketplace/plugins"
			if len(params) > 0 {
				endpoint += "?" + strings.Join(params, "&")
			}

			var resp struct {
				Plugins []struct {
					ID          uint   `json:"id"`
					Slug        string `json:"slug"`
					DisplayName string `json:"display_name"`
					Summary     string `json:"summary"`
					Category    string `json:"category"`
					Verified    bool   `json:"verified"`
				} `json:"plugins"`
				Total int64 `json:"total"`
			}
			if _, err := app.API.Do(ctx, "GET", endpoint, nil, &resp); err != nil {
				return fmt.Errorf("search marketplace plugins: %w", err)
			}
			if len(resp.Plugins) == 0 {
				fmt.Println("No marketplace plugins found.")
				return nil
			}

			fmt.Println(style.Bold.Render(fmt.Sprintf("Marketplace Plugins (%d)", resp.Total)))
			fmt.Println(strings.Repeat("-", 100))
			fmt.Printf("%-30s %-10s %-10s %s\n", "SLUG", "CATEGORY", "VERIFIED", "SUMMARY")
			fmt.Println(strings.Repeat("-", 100))
			for _, p := range resp.Plugins {
				v := "no"
				if p.Verified {
					v = "yes"
				}
				fmt.Printf("%-30s %-10s %-10s %s\n", p.Slug, p.Category, v, truncateText(p.Summary, 45))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "Search query")
	cmd.Flags().StringVar(&category, "category", "", "Category filter")
	cmd.Flags().BoolVar(&verified, "verified", false, "Only show verified plugins")
	return cmd
}

func newPluginMarketplaceInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <slug>",
		Short: "Show marketplace plugin details and releases",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			slug := strings.TrimSpace(args[0])
			var data struct {
				Plugin struct {
					ID          uint   `json:"id"`
					Slug        string `json:"slug"`
					DisplayName string `json:"display_name"`
					Summary     string `json:"summary"`
					Category    string `json:"category"`
					Verified    bool   `json:"verified"`
				} `json:"plugin"`
				Releases []struct {
					ID          uint   `json:"id"`
					Version     string `json:"version"`
					PluginType  string `json:"plugin_type"`
					ScanStatus  string `json:"scan_status"`
					PublishedAt string `json:"published_at"`
				} `json:"releases"`
			}
			if _, err := app.API.Do(ctx, "GET", "marketplace/plugins/"+slug+"/releases", nil, &data); err != nil {
				return fmt.Errorf("get marketplace plugin: %w", err)
			}

			fmt.Println(style.Bold.Render(data.Plugin.DisplayName + " (" + data.Plugin.Slug + ")"))
			fmt.Printf("Category: %s\n", data.Plugin.Category)
			fmt.Printf("Verified: %v\n", data.Plugin.Verified)
			if data.Plugin.Summary != "" {
				fmt.Printf("Summary:  %s\n", data.Plugin.Summary)
			}
			fmt.Println()
			if len(data.Releases) == 0 {
				fmt.Println("No releases published yet.")
				return nil
			}
			fmt.Println(style.Bold.Render("Releases"))
			for _, r := range data.Releases {
				fmt.Printf("  - id=%d version=%s type=%s scan=%s\n", r.ID, r.Version, r.PluginType, r.ScanStatus)
			}
			return nil
		},
	}
	return cmd
}

func newPluginMarketplaceInstallCommand() *cobra.Command {
	var (
		clustersCSV string
		pinned      bool
	)
	cmd := &cobra.Command{
		Use:   "install <slug>@<version>",
		Short: "Install a marketplace plugin release to target clusters",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 25*time.Second)
			defer cancel()

			slug, version, err := parseSlugVersion(args[0])
			if err != nil {
				return err
			}

			var rel struct {
				Releases []struct {
					ID      uint   `json:"id"`
					Version string `json:"version"`
				} `json:"releases"`
			}
			if _, err := app.API.Do(ctx, "GET", "marketplace/plugins/"+slug+"/releases", nil, &rel); err != nil {
				return fmt.Errorf("get releases: %w", err)
			}
			var releaseID uint
			for _, r := range rel.Releases {
				if r.Version == version {
					releaseID = r.ID
					break
				}
			}
			if releaseID == 0 {
				return fmt.Errorf("release %s not found for plugin %s", version, slug)
			}

			targets := []string{"*"}
			if strings.TrimSpace(clustersCSV) != "" {
				targets = splitCSV(clustersCSV)
				if len(targets) == 0 {
					targets = []string{"*"}
				}
			}

			body := map[string]interface{}{
				"marketplace_release_id": releaseID,
				"target_clusters":        targets,
				"pinned":                 pinned,
			}
			var created struct {
				Install struct {
					ID     uint   `json:"id"`
					Status string `json:"status"`
				} `json:"install"`
			}
			if _, err := app.API.Do(ctx, "POST", "marketplace/installs", body, &created); err != nil {
				return fmt.Errorf("create install: %w", err)
			}
			fmt.Println(style.Success.Render(fmt.Sprintf("Installed %s@%s (install id=%d, status=%s)", slug, version, created.Install.ID, created.Install.Status)))
			return nil
		},
	}
	cmd.Flags().StringVar(&clustersCSV, "clusters", "*", `Target clusters CSV (e.g. "frank,hp" or "*")`)
	cmd.Flags().BoolVar(&pinned, "pinned", true, "Pin install to this exact release")
	return cmd
}

func newPluginMarketplaceInstallsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "installs",
		Short: "List marketplace installs for your org",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			var data struct {
				Installs []struct {
					ID                   uint     `json:"id"`
					MarketplaceReleaseID uint     `json:"marketplace_release_id"`
					TargetClusters       []string `json:"target_clusters"`
					Status               string   `json:"status"`
					Pinned               bool     `json:"pinned"`
				} `json:"installs"`
			}
			if _, err := app.API.Do(ctx, "GET", "marketplace/installs", nil, &data); err != nil {
				return fmt.Errorf("list installs: %w", err)
			}
			if len(data.Installs) == 0 {
				fmt.Println("No marketplace installs found.")
				return nil
			}

			fmt.Println(style.Bold.Render("Marketplace Installs"))
			for _, i := range data.Installs {
				fmt.Printf("- id=%d release=%d status=%s pinned=%v targets=%s\n",
					i.ID, i.MarketplaceReleaseID, i.Status, i.Pinned, strings.Join(i.TargetClusters, ","))
			}
			return nil
		},
	}
	return cmd
}

func newPluginMarketplaceRollbackCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback <install-id>",
		Short: "Rollback (disable) a marketplace install",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid install id: %w", err)
			}
			if _, err := app.API.Do(ctx, "POST", fmt.Sprintf("marketplace/installs/%d/rollback", id), nil, nil); err != nil {
				return fmt.Errorf("rollback install: %w", err)
			}
			fmt.Println(style.Success.Render(fmt.Sprintf("Rolled back install %d", id)))
			return nil
		},
	}
	return cmd
}

func newPluginMarketplaceUninstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall <install-id>",
		Short: "Uninstall a marketplace install",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid install id: %w", err)
			}
			if _, err := app.API.Do(ctx, "DELETE", fmt.Sprintf("marketplace/installs/%d", id), nil, nil); err != nil {
				return fmt.Errorf("delete install: %w", err)
			}
			fmt.Println(style.Success.Render(fmt.Sprintf("Uninstalled install %d", id)))
			return nil
		},
	}
	return cmd
}

func parseSlugVersion(input string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(input), "@")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("expected <slug>@<version>, got %q", input)
	}
	return parts[0], parts[1], nil
}

func splitCSV(v string) []string {
	raw := strings.Split(v, ",")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
