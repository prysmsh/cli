package plugin

import (
	"context"
	"testing"
)

// mockPlugin is a minimal in-process plugin for testing.
type mockPlugin struct {
	manifest Manifest
	runArgs  []string
}

func (p *mockPlugin) Manifest() Manifest { return p.manifest }
func (p *mockPlugin) Execute(ctx context.Context, req ExecuteRequest) ExecuteResponse {
	p.runArgs = req.Args
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
