package plugin

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	goplugin "github.com/hashicorp/go-plugin"
	"github.com/spf13/cobra"
)

// Manager discovers, loads, and manages plugin lifecycle.
type Manager struct {
	builtins  map[string]Plugin
	externals map[string]*externalEntry
	hostSvc   HostServices
	homeDir   string
	debug     bool
	clients   []*goplugin.Client // for cleanup
}

type externalEntry struct {
	disc   DiscoveredPlugin
	plugin Plugin // lazy-loaded
}

// NewManager creates a new plugin manager.
func NewManager(hostSvc HostServices, homeDir string, debug bool) *Manager {
	return &Manager{
		builtins:  make(map[string]Plugin),
		externals: make(map[string]*externalEntry),
		hostSvc:   hostSvc,
		homeDir:   homeDir,
		debug:     debug,
	}
}

// RegisterBuiltin registers an in-process plugin.
func (m *Manager) RegisterBuiltin(name string, p Plugin) {
	m.builtins[name] = p
}

// DiscoverExternal scans for external plugin binaries.
func (m *Manager) DiscoverExternalPlugins() {
	discovered := DiscoverExternal(m.homeDir)
	for _, d := range discovered {
		if _, exists := m.builtins[d.Name]; exists {
			if m.debug {
				log.Printf("[plugin] skipping external %q (conflicts with builtin)", d.Name)
			}
			continue
		}
		m.externals[d.Name] = &externalEntry{disc: d}
	}
}

// RegisterCommands adds plugin commands to the root Cobra command.
// Builtins get full command trees; externals get placeholder commands.
func (m *Manager) RegisterCommands(rootCmd *cobra.Command) {
	existing := make(map[string]bool)
	for _, cmd := range rootCmd.Commands() {
		existing[cmd.Name()] = true
	}

	// Register builtin plugin commands
	for name, p := range m.builtins {
		manifest := p.Manifest()
		for _, spec := range manifest.Commands {
			if existing[spec.Name] {
				if m.debug {
					log.Printf("[plugin] skipping command %q from builtin %q (conflicts with existing)", spec.Name, name)
				}
				continue
			}
			cmd := BuildCobraCommand(spec, p)
			rootCmd.AddCommand(cmd)
			existing[spec.Name] = true
		}
	}

	// Register external plugin placeholder commands
	for name, entry := range m.externals {
		if existing[name] {
			if m.debug {
				log.Printf("[plugin] skipping external %q (command name conflicts)", name)
			}
			continue
		}
		cmd := m.buildExternalCommand(name, entry)
		rootCmd.AddCommand(cmd)
		existing[name] = true
	}
}

// ListPlugins returns info about all registered plugins.
func (m *Manager) ListPlugins() []PluginInfo {
	var list []PluginInfo
	for name, p := range m.builtins {
		manifest := p.Manifest()
		list = append(list, PluginInfo{
			Name:        name,
			Version:     manifest.Version,
			Description: manifest.Description,
			Type:        "builtin",
		})
	}
	for name, entry := range m.externals {
		list = append(list, PluginInfo{
			Name:        name,
			Description: "external plugin at " + entry.disc.Path,
			Type:        "external",
			Path:        entry.disc.Path,
		})
	}
	return list
}

// GetPlugin returns a named plugin, or nil if not found.
func (m *Manager) GetPlugin(name string) Plugin {
	if p, ok := m.builtins[name]; ok {
		return p
	}
	if entry, ok := m.externals[name]; ok {
		if entry.plugin == nil {
			if err := m.loadExternal(entry); err != nil {
				log.Printf("[plugin] failed to load %q: %v", name, err)
				return nil
			}
		}
		return entry.plugin
	}
	return nil
}

// Shutdown kills all external plugin subprocesses.
func (m *Manager) Shutdown() {
	for _, c := range m.clients {
		c.Kill()
	}
}

// PluginInfo describes a registered plugin.
type PluginInfo struct {
	Name        string
	Version     string
	Description string
	Type        string // "builtin" or "external"
	Path        string // only for external
}

// BuildCobraCommand creates a full Cobra command tree from a builtin plugin's CommandSpec.
func BuildCobraCommand(spec CommandSpec, p Plugin) *cobra.Command {
	cmd := &cobra.Command{
		Use:   spec.Name,
		Short: spec.Short,
		Long:  spec.Long,
	}

	if spec.DisableFlagParsing {
		cmd.DisableFlagParsing = true
	}

	if len(spec.Subcommands) > 0 {
		for _, sub := range spec.Subcommands {
			cmd.AddCommand(BuildCobraCommand(sub, p))
		}
	} else {
		// Leaf command — execute the plugin
		pluginRef := p
		cmdName := spec.Name
		cmd.RunE = func(c *cobra.Command, args []string) error {
			// Reconstruct the full argument list including the subcommand path
			fullArgs := []string{cmdName}
			fullArgs = append(fullArgs, args...)

			wd, _ := os.Getwd()
			resp := pluginRef.Execute(c.Context(), ExecuteRequest{
				Args:       fullArgs,
				WorkingDir: wd,
			})

			if resp.Stdout != "" {
				fmt.Print(resp.Stdout)
			}
			if resp.Error != "" {
				return fmt.Errorf("%s", resp.Error)
			}
			if resp.ExitCode != 0 {
				os.Exit(resp.ExitCode)
			}
			return nil
		}
	}

	return cmd
}

// buildExternalCommand creates a placeholder Cobra command for an external plugin.
// Flag parsing is disabled; all args are passed through to the plugin's Execute.
func (m *Manager) buildExternalCommand(name string, entry *externalEntry) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              fmt.Sprintf("External plugin: %s", name),
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if entry.plugin == nil {
				if err := m.loadExternal(entry); err != nil {
					return fmt.Errorf("failed to load plugin %q: %w", name, err)
				}
			}

			wd, _ := os.Getwd()
			resp := entry.plugin.Execute(cmd.Context(), ExecuteRequest{
				Args:       args,
				WorkingDir: wd,
			})

			if resp.Stdout != "" {
				fmt.Print(resp.Stdout)
			}
			if resp.Error != "" {
				return fmt.Errorf("%s", resp.Error)
			}
			if resp.ExitCode != 0 {
				os.Exit(resp.ExitCode)
			}
			return nil
		},
	}
}

// loadExternal starts an external plugin subprocess and connects via gRPC.
func (m *Manager) loadExternal(entry *externalEntry) error {
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins: map[string]goplugin.Plugin{
			PluginKey: &GRPCPluginImpl{},
		},
		Cmd:              exec.Command(entry.disc.Path),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Stderr:           os.Stderr,
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return fmt.Errorf("connect to plugin %q: %w", entry.disc.Name, err)
	}

	raw, err := rpcClient.Dispense(PluginKey)
	if err != nil {
		client.Kill()
		return fmt.Errorf("dispense plugin %q: %w", entry.disc.Name, err)
	}

	p, ok := raw.(*GRPCPluginClient)
	if !ok {
		client.Kill()
		return fmt.Errorf("plugin %q returned unexpected type", entry.disc.Name)
	}

	entry.plugin = p
	m.clients = append(m.clients, client)
	return nil
}

// buildCommandPath reconstructs the command path from cobra for plugin routing.
func buildCommandPath(cmd *cobra.Command) string {
	var parts []string
	for c := cmd; c != nil && c.Parent() != nil; c = c.Parent() {
		parts = append([]string{c.Name()}, parts...)
	}
	return strings.Join(parts, " ")
}

// contextWithHostServices wraps a context with host services for plugin access.
func contextWithHostServices(ctx context.Context, host HostServices) context.Context {
	return context.WithValue(ctx, hostServicesKey{}, host)
}

// HostServicesFromContext retrieves HostServices from context.
func HostServicesFromContext(ctx context.Context) HostServices {
	if h, ok := ctx.Value(hostServicesKey{}).(HostServices); ok {
		return h
	}
	return nil
}

type hostServicesKey struct{}
