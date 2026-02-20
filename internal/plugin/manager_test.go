package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// mockHostServices implements HostServices for context tests.
type mockHostServices struct{}

func (m *mockHostServices) GetAuthContext(context.Context) (*AuthContext, error) { return nil, nil }
func (m *mockHostServices) APIRequest(context.Context, string, string, []byte) (int, []byte, error) {
	return 0, nil, nil
}
func (m *mockHostServices) GetConfig(context.Context) (*HostConfig, error)       { return nil, nil }
func (m *mockHostServices) Log(context.Context, LogLevel, string) error         { return nil }
func (m *mockHostServices) PromptInput(context.Context, string, bool) (string, error) { return "", nil }
func (m *mockHostServices) PromptConfirm(context.Context, string) (bool, error)  { return false, nil }

// mockPlugin is a minimal in-process plugin for testing.
type mockPlugin struct {
	manifest    Manifest
	runArgs     []string
	executeResp *ExecuteResponse // if set, returned by Execute
}

func (p *mockPlugin) Manifest() Manifest { return p.manifest }
func (p *mockPlugin) Execute(ctx context.Context, req ExecuteRequest) ExecuteResponse {
	p.runArgs = req.Args
	if p.executeResp != nil {
		return *p.executeResp
	}
	return ExecuteResponse{ExitCode: 0, Stdout: "ok"}
}

func TestNewManager(t *testing.T) {
	m := NewManager(nil, "/home", false)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestManager_RegisterBuiltin_ListPlugins(t *testing.T) {
	m := NewManager(nil, "/tmp", false)
	p := &mockPlugin{
		manifest: Manifest{
			Name:        "test",
			Version:     "1.0",
			Description: "Test plugin",
			Commands:    []CommandSpec{{Name: "run", Short: "Run"}},
		},
	}
	m.RegisterBuiltin("test", p)

	list := m.ListPlugins()
	if len(list) != 1 {
		t.Fatalf("ListPlugins len = %d, want 1", len(list))
	}
	if list[0].Name != "test" {
		t.Errorf("Name = %q", list[0].Name)
	}
	if list[0].Type != "builtin" {
		t.Errorf("Type = %q", list[0].Type)
	}
	if list[0].Version != "1.0" {
		t.Errorf("Version = %q", list[0].Version)
	}
}

func TestManager_GetPlugin_Builtin(t *testing.T) {
	m := NewManager(nil, "/tmp", false)
	p := &mockPlugin{manifest: Manifest{Name: "myplugin", Commands: []CommandSpec{}}}
	m.RegisterBuiltin("myplugin", p)

	got := m.GetPlugin("myplugin")
	if got != p {
		t.Error("GetPlugin myplugin did not return registered builtin")
	}
	if m.GetPlugin("nonexistent") != nil {
		t.Error("GetPlugin nonexistent should return nil")
	}
}

func TestManager_Shutdown(t *testing.T) {
	m := NewManager(nil, "/tmp", false)
	m.Shutdown() // no-op when no externals
}

func TestBuildCobraCommand_Leaf(t *testing.T) {
	p := &mockPlugin{
		manifest: Manifest{
			Commands: []CommandSpec{
				{Name: "leaf", Short: "Leaf command"},
			},
		},
	}
	cmd := BuildCobraCommand(p.Manifest().Commands[0], p)
	if cmd.Use != "leaf" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Short != "Leaf command" {
		t.Errorf("Short = %q", cmd.Short)
	}
	if cmd.RunE == nil {
		t.Fatal("RunE should be set for leaf command")
	}

	// Run the command
	err := cmd.RunE(cmd, []string{})
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(p.runArgs) != 1 || p.runArgs[0] != "leaf" {
		t.Errorf("runArgs = %v", p.runArgs)
	}
}

func TestBuildCobraCommand_ExecuteReturnsError(t *testing.T) {
	p := &mockPlugin{
		manifest: Manifest{
			Commands: []CommandSpec{
				{Name: "fail", Short: "Fails"},
			},
		},
	}
	// Override Execute to return error
	p.executeResp = &ExecuteResponse{ExitCode: 0, Error: "something went wrong"}
	cmd := BuildCobraCommand(p.Manifest().Commands[0], p)

	err := cmd.RunE(cmd, []string{})
	if err == nil {
		t.Fatal("expected error from Execute")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Errorf("error = %v", err)
	}
}

func TestBuildCobraCommand_WithSubcommands(t *testing.T) {
	p := &mockPlugin{
		manifest: Manifest{
			Commands: []CommandSpec{
				{
					Name:  "parent",
					Short: "Parent",
					Subcommands: []CommandSpec{
						{Name: "child", Short: "Child"},
					},
				},
			},
		},
	}
	cmd := BuildCobraCommand(p.Manifest().Commands[0], p)
	if len(cmd.Commands()) != 1 {
		t.Fatalf("subcommands = %d", len(cmd.Commands()))
	}
	child := cmd.Commands()[0]
	if child.Name() != "child" {
		t.Errorf("child name = %q", child.Name())
	}
}

func TestBuildCobraCommand_DisableFlagParsing(t *testing.T) {
	p := &mockPlugin{
		manifest: Manifest{
			Commands: []CommandSpec{
				{Name: "raw", Short: "Raw", DisableFlagParsing: true},
			},
		},
	}
	cmd := BuildCobraCommand(p.Manifest().Commands[0], p)
	if !cmd.DisableFlagParsing {
		t.Error("DisableFlagParsing should be true")
	}
}

func TestHostServicesFromContext(t *testing.T) {
	ctx := context.Background()
	if HostServicesFromContext(ctx) != nil {
		t.Error("HostServicesFromContext(background) should be nil")
	}
}

func TestHostServicesFromContext_WithValue(t *testing.T) {
	host := &mockHostServices{}
	ctx := contextWithHostServices(context.Background(), host)
	got := HostServicesFromContext(ctx)
	if got != host {
		t.Error("HostServicesFromContext should return the set host")
	}
}


func TestManager_DiscoverExternalPlugins(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(pluginsDir, "prysm-plugin-extra")
	if err := os.WriteFile(pluginPath, []byte("#!/bin/sh\nexit 0"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewManager(nil, dir, false)
	m.RegisterBuiltin("builtin1", &mockPlugin{manifest: Manifest{Name: "b1", Commands: []CommandSpec{}}})
	m.DiscoverExternalPlugins()

	list := m.ListPlugins()
	if len(list) < 2 {
		t.Errorf("expected at least 2 plugins (builtin + external), got %d", len(list))
	}
	var foundExternal bool
	for _, p := range list {
		if p.Type == "external" && p.Name == "extra" {
			foundExternal = true
			break
		}
	}
	if !foundExternal {
		t.Error("external plugin extra not found")
	}
}

func TestManager_DiscoverExternalPlugins_SkipsConflictingBuiltin(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(pluginsDir, "prysm-plugin-onboard")
	if err := os.WriteFile(pluginPath, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewManager(nil, dir, true) // debug to hit log path
	m.RegisterBuiltin("onboard", &mockPlugin{manifest: Manifest{Name: "onboard", Commands: []CommandSpec{}}})
	m.DiscoverExternalPlugins()

	list := m.ListPlugins()
	if len(list) != 1 {
		t.Errorf("expected 1 plugin (external skipped), got %d", len(list))
	}
}

func TestManager_RegisterCommands(t *testing.T) {
	rootCmd := cobra.Command{}
	rootCmd.AddCommand(&cobra.Command{Use: "existing"})

	m := NewManager(nil, t.TempDir(), false)
	m.RegisterBuiltin("p", &mockPlugin{
		manifest: Manifest{
			Commands: []CommandSpec{
				{Name: "newcmd", Short: "New"},
				{Name: "existing", Short: "Conflict"},
			},
		},
	})
	m.RegisterCommands(&rootCmd)

	names := make(map[string]bool)
	for _, c := range rootCmd.Commands() {
		names[c.Name()] = true
	}
	if !names["newcmd"] {
		t.Error("newcmd not registered")
	}
	// "existing" was already on root; plugin's "existing" is skipped (conflict), so we have exactly 2 commands
	if len(rootCmd.Commands()) != 2 {
		t.Errorf("expected 2 commands (existing + newcmd), got %d", len(rootCmd.Commands()))
	}
}
