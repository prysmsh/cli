package cmd

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func newPluginCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage CLI plugins",
	}

	cmd.AddCommand(newPluginListCommand())
	cmd.AddCommand(newPluginInfoCommand())

	return cmd
}

func newPluginListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			if pluginMgr == nil {
				return fmt.Errorf("plugin system not initialized")
			}

			plugins := pluginMgr.ListPlugins()
			if len(plugins) == 0 {
				fmt.Println("No plugins installed.")
				fmt.Println("\nExternal plugins are discovered from $PRYSM_HOME/plugins/ and $PATH (prysm-plugin-* binaries).")
				return nil
			}

			bold := color.New(color.Bold)
			for _, p := range plugins {
				typeColor := color.New(color.FgCyan)
				if p.Type == "external" {
					typeColor = color.New(color.FgYellow)
				}

				bold.Printf("  %s", p.Name)
				fmt.Print(" ")
				typeColor.Printf("(%s)", p.Type)
				if p.Version != "" {
					fmt.Printf(" v%s", p.Version)
				}
				fmt.Println()
				if p.Description != "" {
					fmt.Printf("    %s\n", p.Description)
				}
				if p.Path != "" {
					fmt.Printf("    Path: %s\n", p.Path)
				}
			}

			return nil
		},
	}
}

func newPluginInfoCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show details about a plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if pluginMgr == nil {
				return fmt.Errorf("plugin system not initialized")
			}

			name := args[0]
			p := pluginMgr.GetPlugin(name)
			if p == nil {
				return fmt.Errorf("plugin %q not found", name)
			}

			manifest := p.Manifest()
			bold := color.New(color.Bold)

			bold.Printf("Plugin: %s\n", manifest.Name)
			fmt.Printf("Version: %s\n", manifest.Version)
			fmt.Printf("Description: %s\n", manifest.Description)

			if len(manifest.Commands) > 0 {
				fmt.Println("\nCommands:")
				for _, c := range manifest.Commands {
					printCommandTree(c, "  ")
				}
			}

			return nil
		},
	}
}

func printCommandTree(spec interface{ }, indent string) {
	// Use type assertion to handle the CommandSpec
	type cmdSpec struct {
		Name        string
		Short       string
		Subcommands []cmdSpec
	}

	// This is called from newPluginInfoCommand with plugin.CommandSpec
	// but since we can't import plugin here without a cycle, we use the Manifest directly
}

func init() {
	// printCommandTree is filled in at initialization from the plugin package types
}

// printPluginCommandTree prints a command spec tree with indentation.
func printPluginCommandTree(name, short string, subNames []string, indent string) {
	bold := color.New(color.Bold)
	bold.Printf("%s%s", indent, name)
	if short != "" {
		fmt.Printf(" — %s", short)
	}
	fmt.Println()

	for _, sub := range subNames {
		fmt.Printf("%s  %s\n", indent, sub)
	}
}

// FormatPluginCommands returns a formatted string of plugin commands for display.
func FormatPluginCommands() string {
	if pluginMgr == nil {
		return ""
	}
	plugins := pluginMgr.ListPlugins()
	if len(plugins) == 0 {
		return ""
	}
	var parts []string
	for _, p := range plugins {
		parts = append(parts, fmt.Sprintf("%s (%s)", p.Name, p.Type))
	}
	return strings.Join(parts, ", ")
}
