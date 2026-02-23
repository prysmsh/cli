package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/util"
)

func newPluginWasmCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wasm",
		Short: "Manage WASM plugins deployed to your nodes",
	}

	cmd.AddCommand(
		newWasmPluginsListCommand(),
		newWasmPluginsCreateCommand(),
		newWasmPluginsWizardCommand(),
		newWasmPluginsUpdateCommand(),
		newWasmPluginsDeleteCommand(),
		newWasmPluginsEnableCommand(),
		newWasmPluginsDisableCommand(),
		newWasmPluginsUploadCommand(),
		newWasmPluginsEventsCommand(),
	)

	return cmd
}

func newWasmPluginsListCommand() *cobra.Command {
	var (
		statusFilter string
		pluginType   string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List WASM plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			endpoint := "plugins"
			params := []string{}
			if statusFilter != "" {
				params = append(params, "status="+statusFilter)
			}
			if pluginType != "" {
				params = append(params, "type="+pluginType)
			}
			if len(params) > 0 {
				endpoint += "?" + strings.Join(params, "&")
			}

			var data struct {
				Plugins []struct {
					ID             uint     `json:"id"`
					Name           string   `json:"name"`
					Type           string   `json:"type"`
					Status         string   `json:"status"`
					Enabled        bool     `json:"enabled"`
					StatusMessage  string   `json:"status_message"`
					TargetClusters []string `json:"target_clusters"`
					WasmURL        string   `json:"wasm_url"`
					CreatedAt      string   `json:"created_at"`
				} `json:"plugins"`
				Total int64 `json:"total"`
			}

			resp, err := app.API.Do(ctx, "GET", endpoint, nil, &data)
			if err != nil {
				return fmt.Errorf("list plugins: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("list plugins: %s", resp.Status)
			}

			if len(data.Plugins) == 0 {
				fmt.Println("No WASM plugins found.")
				fmt.Println("\nCreate one with: prysm plugin wasm wizard")
				return nil
			}

			fmt.Println(style.Bold.Render(fmt.Sprintf("WASM Plugins (%d total)", data.Total)))
			fmt.Println(strings.Repeat("-", 100))
			fmt.Print(style.Bold.Render(fmt.Sprintf("%-6s %-28s %-16s %-10s %-8s %-28s\n",
				"ID", "NAME", "TYPE", "STATUS", "ENABLED", "TARGETS")))
			fmt.Println(strings.Repeat("-", 100))

			for _, p := range data.Plugins {
				statusSty := style.Warning
				switch p.Status {
				case "active":
					statusSty = style.Success
				case "error":
					statusSty = style.Error
				case "disabled":
					statusSty = style.Info
				}

				enabledStr := "No"
				enabledSty := style.Error
				if p.Enabled {
					enabledStr = "Yes"
					enabledSty = style.Success
				}

				targets := strings.Join(p.TargetClusters, ", ")
				if len(targets) > 28 {
					targets = fmt.Sprintf("%d clusters", len(p.TargetClusters))
				}
				if targets == "" {
					targets = "-"
				}

				fmt.Printf("%-6d %-28s ", p.ID, truncate(p.Name, 28))
				fmt.Printf("%-16s ", truncate(p.Type, 16))
				fmt.Print(statusSty.Render(fmt.Sprintf("%-10s ", p.Status)))
				fmt.Print(enabledSty.Render(fmt.Sprintf("%-8s ", enabledStr)))
				fmt.Printf("%s\n", targets)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&statusFilter, "status", "s", "", "Filter by status (pending, active, error, disabled)")
	cmd.Flags().StringVarP(&pluginType, "type", "t", "", "Filter by type (network-filter, log-filter, honeypot, custom)")
	return cmd
}

func newWasmPluginsCreateCommand() *cobra.Command {
	var (
		name        string
		description string
		pluginType  string
		clusters    []string
		tags        []string
		wasmURL     string
		wasmSHA256  string
		config      string
		disabled    bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new WASM plugin",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			body := map[string]interface{}{
				"name":            name,
				"description":     description,
				"type":            pluginType,
				"target_clusters": clusters,
				"target_tags":     tags,
				"wasm_url":        wasmURL,
				"wasm_sha256":     wasmSHA256,
				"enabled":         !disabled,
			}

			if config != "" {
				var raw json.RawMessage
				if err := json.Unmarshal([]byte(config), &raw); err != nil {
					return fmt.Errorf("--config must be valid JSON: %w", err)
				}
				body["config"] = raw
			}

			var result struct {
				ID     uint   `json:"id"`
				Name   string `json:"name"`
				Status string `json:"status"`
			}

			resp, err := app.API.Do(ctx, "POST", "plugins", body, &result)
			if err != nil {
				return fmt.Errorf("create plugin: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("create plugin: %s", resp.Status)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("✓ Plugin %q created (id=%d, status=%s)", result.Name, result.ID, result.Status)))
			fmt.Println()
			fmt.Println("Agents will pick up the plugin on their next reconciliation cycle (every 30s).")
			fmt.Printf("Upload a binary with: prysm plugin wasm upload %d ./plugin.wasm\n", result.ID)
			fmt.Printf("Check status with: prysm plugin wasm list\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Plugin name (required)")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Plugin description")
	cmd.Flags().StringVarP(&pluginType, "type", "t", "network-filter", "Plugin type (network-filter, log-filter, honeypot, custom)")
	cmd.Flags().StringArrayVarP(&clusters, "clusters", "c", []string{"*"}, `Target cluster IDs (use "*" for all clusters)`)
	cmd.Flags().StringArrayVar(&tags, "tags", nil, "Target node tags")
	cmd.Flags().StringVar(&wasmURL, "wasm-url", "", "URL to the .wasm binary (Phase 2)")
	cmd.Flags().StringVar(&wasmSHA256, "wasm-sha256", "", "SHA-256 hash of the .wasm binary (Phase 2)")
	cmd.Flags().StringVar(&config, "config", "", "Plugin-specific config as JSON")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "Create plugin in disabled state")
	return cmd
}

func newWasmPluginsWizardCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "wizard",
		Short: "Create a WASM plugin interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(style.Bold.Render("WASM Plugin Wizard"))
			fmt.Println("Press Enter to accept defaults shown in [brackets].")
			fmt.Println()

			name, err := promptRequired("Plugin name")
			if err != nil {
				return fmt.Errorf("plugin name: %w", err)
			}

			description, err := promptWithDefault("Description", "")
			if err != nil {
				return fmt.Errorf("description: %w", err)
			}

			pluginType, err := promptPluginType()
			if err != nil {
				return fmt.Errorf("plugin type: %w", err)
			}

			clustersRaw, err := promptWithDefault(`Target clusters (comma-separated, "*" for all)`, "*")
			if err != nil {
				return fmt.Errorf("target clusters: %w", err)
			}
			clusters := parseCSV(clustersRaw)
			if len(clusters) == 0 {
				clusters = []string{"*"}
			}

			tagsRaw, err := promptWithDefault("Target node tags (comma-separated, optional)", "")
			if err != nil {
				return fmt.Errorf("target tags: %w", err)
			}
			tags := parseCSV(tagsRaw)

			config, err := promptWithDefault(`Plugin config JSON (optional, example: {"level":"warn"})`, "")
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			config = strings.TrimSpace(config)
			if config != "" {
				var raw json.RawMessage
				if err := json.Unmarshal([]byte(config), &raw); err != nil {
					return fmt.Errorf("config must be valid JSON: %w", err)
				}
			}

			disabled, err := util.PromptConfirm("Create plugin disabled?", false)
			if err != nil {
				return fmt.Errorf("disabled prompt: %w", err)
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			body := map[string]interface{}{
				"name":            name,
				"description":     description,
				"type":            pluginType,
				"target_clusters": clusters,
				"target_tags":     tags,
				"enabled":         !disabled,
			}
			if config != "" {
				var raw json.RawMessage
				_ = json.Unmarshal([]byte(config), &raw)
				body["config"] = raw
			}

			var result struct {
				ID     uint   `json:"id"`
				Name   string `json:"name"`
				Status string `json:"status"`
			}

			resp, err := app.API.Do(ctx, "POST", "plugins", body, &result)
			if err != nil {
				return fmt.Errorf("create plugin: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("create plugin: %s", resp.Status)
			}

			fmt.Println()
			fmt.Println(style.Success.Render(fmt.Sprintf("✓ Plugin %q created (id=%d, status=%s)", result.Name, result.ID, result.Status)))
			fmt.Println("Agents reconcile every 30s.")

			uploadNow, err := util.PromptConfirm("Upload a .wasm file now?", true)
			if err != nil {
				return fmt.Errorf("upload prompt: %w", err)
			}
			if uploadNow {
				wasmPath, err := promptRequired("Path to .wasm file")
				if err != nil {
					return fmt.Errorf("wasm path: %w", err)
				}
				uploadCtx, uploadCancel := context.WithTimeout(cmd.Context(), 60*time.Second)
				defer uploadCancel()
				if err := uploadWasmBinary(uploadCtx, fmt.Sprintf("%d", result.ID), wasmPath); err != nil {
					return err
				}
			} else {
				fmt.Printf("Upload later with: prysm plugin wasm upload %d ./plugin.wasm\n", result.ID)
			}

			fmt.Printf("Check status with: prysm plugin wasm list\n")
			return nil
		},
	}
}

func newWasmPluginsUpdateCommand() *cobra.Command {
	var (
		name        string
		description string
		clusters    []string
		tags        []string
		wasmURL     string
		wasmSHA256  string
		config      string
	)

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an existing WASM plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			id := args[0]
			body := map[string]interface{}{}

			if cmd.Flags().Changed("name") {
				body["name"] = name
			}
			if cmd.Flags().Changed("description") {
				body["description"] = description
			}
			if cmd.Flags().Changed("clusters") {
				body["target_clusters"] = clusters
			}
			if cmd.Flags().Changed("tags") {
				body["target_tags"] = tags
			}
			if cmd.Flags().Changed("wasm-url") {
				body["wasm_url"] = wasmURL
			}
			if cmd.Flags().Changed("wasm-sha256") {
				body["wasm_sha256"] = wasmSHA256
			}
			if cmd.Flags().Changed("config") {
				var raw json.RawMessage
				if err := json.Unmarshal([]byte(config), &raw); err != nil {
					return fmt.Errorf("--config must be valid JSON: %w", err)
				}
				body["config"] = raw
			}

			if len(body) == 0 {
				return fmt.Errorf("no fields to update; specify at least one flag")
			}

			var result struct {
				ID     uint   `json:"id"`
				Name   string `json:"name"`
				Status string `json:"status"`
			}

			resp, err := app.API.Do(ctx, "PUT", "plugins/"+id, body, &result)
			if err != nil {
				return fmt.Errorf("update plugin: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("update plugin: %s", resp.Status)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("✓ Plugin %q updated (status reset to: %s)", result.Name, result.Status)))
			fmt.Println("Agents will reconcile on the next cycle (every 30s).")

			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "New plugin name")
	cmd.Flags().StringVarP(&description, "description", "d", "", "New description")
	cmd.Flags().StringArrayVarP(&clusters, "clusters", "c", nil, "New target cluster IDs")
	cmd.Flags().StringArrayVar(&tags, "tags", nil, "New target node tags")
	cmd.Flags().StringVar(&wasmURL, "wasm-url", "", "New .wasm URL")
	cmd.Flags().StringVar(&wasmSHA256, "wasm-sha256", "", "New .wasm SHA-256")
	cmd.Flags().StringVar(&config, "config", "", "New plugin config as JSON")
	return cmd
}

func newWasmPluginsDeleteCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a WASM plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			if !force {
				fmt.Printf("Delete plugin %s? This will stop it on all nodes. [y/N] ", id)
				var confirm string
				if _, err := fmt.Scan(&confirm); err != nil || strings.ToLower(confirm) != "y" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			resp, err := app.API.Do(ctx, "DELETE", "plugins/"+id, nil, nil)
			if err != nil {
				return fmt.Errorf("delete plugin: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("delete plugin: %s", resp.Status)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("✓ Plugin %s deleted", id)))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}

func newWasmPluginsEnableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <id>",
		Short: "Enable a WASM plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			resp, err := app.API.Do(ctx, "PUT", "plugins/"+args[0], map[string]interface{}{"enabled": true}, nil)
			if err != nil {
				return fmt.Errorf("enable plugin: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("enable plugin: %s", resp.Status)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("✓ Plugin %s enabled", args[0])))
			return nil
		},
	}
}

func newWasmPluginsDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <id>",
		Short: "Disable a WASM plugin (keeps it registered but agents won't load it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			resp, err := app.API.Do(ctx, "PUT", "plugins/"+args[0], map[string]interface{}{"enabled": false}, nil)
			if err != nil {
				return fmt.Errorf("disable plugin: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("disable plugin: %s", resp.Status)
			}

			fmt.Println(style.Warning.Render(fmt.Sprintf("Plugin %s disabled. Agents will deactivate it on next reconcile.", args[0])))
			return nil
		},
	}
}

func newWasmPluginsUploadCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <id> <file.wasm>",
		Short: "Upload a .wasm binary for a plugin",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()
			return uploadWasmBinary(ctx, args[0], args[1])
		},
	}
}

func newWasmPluginsEventsCommand() *cobra.Command {
	var pluginID string
	var clusterID string

	cmd := &cobra.Command{
		Use:   "events",
		Short: "List events emitted by WASM plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			endpoint := "plugins/events"
			params := []string{}
			if pluginID != "" {
				params = append(params, "plugin_id="+pluginID)
			}
			if clusterID != "" {
				params = append(params, "cluster_id="+clusterID)
			}
			if len(params) > 0 {
				endpoint += "?" + strings.Join(params, "&")
			}

			var data struct {
				Events []struct {
					ID        uint            `json:"id"`
					PluginID  uint            `json:"plugin_id"`
					ClusterID string          `json:"cluster_id"`
					NodeName  string          `json:"node_name"`
					Payload   json.RawMessage `json:"payload"`
					CreatedAt string          `json:"created_at"`
				} `json:"events"`
				Total int64 `json:"total"`
			}

			resp, err := app.API.Do(ctx, "GET", endpoint, nil, &data)
			if err != nil {
				return fmt.Errorf("list events: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("list events: %s", resp.Status)
			}

			if len(data.Events) == 0 {
				fmt.Println("No plugin events found.")
				return nil
			}

			fmt.Println(style.Bold.Render(fmt.Sprintf("Plugin Events (%d total)", data.Total)))
			fmt.Println(strings.Repeat("-", 90))
			for _, e := range data.Events {
				fmt.Printf("[%s] plugin=%d cluster=%s node=%s\n",
					e.CreatedAt, e.PluginID, e.ClusterID, e.NodeName)
				fmt.Printf("  %s\n", string(e.Payload))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&pluginID, "plugin", "", "Filter by plugin ID")
	cmd.Flags().StringVar(&clusterID, "cluster", "", "Filter by cluster ID")
	return cmd
}

func uploadWasmBinary(ctx context.Context, id, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	if len(data) < 4 || string(data[:4]) != "\x00asm" {
		return fmt.Errorf("%s does not appear to be a WASM binary (missing magic)", path)
	}

	app := MustApp()

	var result struct {
		ID            uint   `json:"id"`
		WasmSHA256    string `json:"wasm_sha256"`
		WasmURL       string `json:"wasm_url"`
		WasmSignature string `json:"wasm_signature"`
		SizeBytes     int    `json:"size_bytes"`
	}

	resp, err := app.API.DoRaw(ctx, "POST", "plugins/"+id+"/upload",
		"application/wasm", bytes.NewReader(data), &result)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	if resp != nil && resp.StatusCode >= 400 {
		return fmt.Errorf("upload: %s", resp.Status)
	}

	sigPreview := result.WasmSignature
	if len(sigPreview) > 32 {
		sigPreview = sigPreview[:32] + "..."
	}

	fmt.Println(style.Success.Render(fmt.Sprintf("✓ Uploaded %d bytes to plugin %s", result.SizeBytes, id)))
	fmt.Printf("  SHA-256:   %s\n", result.WasmSHA256)
	fmt.Printf("  URL:       %s\n", result.WasmURL)
	fmt.Printf("  Signature: %s\n", sigPreview)
	fmt.Println("\nAgents will pick up the new binary on their next reconcile (every 30s).")
	return nil
}

func promptWithDefault(label, def string) (string, error) {
	promptLabel := label
	if def != "" {
		promptLabel = fmt.Sprintf("%s [%s]", label, def)
	}
	value, err := util.PromptInput(promptLabel)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return def, nil
	}
	return value, nil
}

func promptRequired(label string) (string, error) {
	for {
		v, err := util.PromptInput(label)
		if err != nil {
			return "", err
		}
		v = strings.TrimSpace(v)
		if v != "" {
			return v, nil
		}
		fmt.Fprintln(os.Stderr, "Value is required.")
	}
}

func promptPluginType() (string, error) {
	types := []string{"network-filter", "log-filter", "honeypot", "custom"}
	fmt.Fprintln(os.Stderr, "Plugin type:")
	for i, t := range types {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, t)
	}
	for {
		in, err := promptWithDefault("Select type number or name", "1")
		if err != nil {
			return "", err
		}
		in = strings.TrimSpace(strings.ToLower(in))
		if idx, err := strconv.Atoi(in); err == nil && idx >= 1 && idx <= len(types) {
			return types[idx-1], nil
		}
		for _, t := range types {
			if in == t {
				return t, nil
			}
		}
		fmt.Fprintln(os.Stderr, "Invalid type. Enter 1-4 or a valid type name.")
	}
}

func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
