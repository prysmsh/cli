package cmdutil

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestNewCommand(t *testing.T) {
	b := NewCommand("test", "A test command")
	cmd := b.Build()
	if cmd.Use != "test" {
		t.Errorf("Use = %q, want %q", cmd.Use, "test")
	}
	if cmd.Short != "A test command" {
		t.Errorf("Short = %q, want %q", cmd.Short, "A test command")
	}
}

func TestCommandBuilder_Long(t *testing.T) {
	cmd := NewCommand("test", "short").Long("A longer description").Build()
	if cmd.Long != "A longer description" {
		t.Errorf("Long = %q", cmd.Long)
	}
}

func TestCommandBuilder_Example(t *testing.T) {
	cmd := NewCommand("test", "short").Example("  test --flag").Build()
	if cmd.Example != "  test --flag" {
		t.Errorf("Example = %q", cmd.Example)
	}
}

func TestCommandBuilder_Hidden(t *testing.T) {
	cmd := NewCommand("test", "short").Hidden().Build()
	if !cmd.Hidden {
		t.Error("expected Hidden = true")
	}
}

func TestCommandBuilder_Aliases(t *testing.T) {
	cmd := NewCommand("test", "short").Aliases("t", "tst").Build()
	if len(cmd.Aliases) != 2 || cmd.Aliases[0] != "t" || cmd.Aliases[1] != "tst" {
		t.Errorf("Aliases = %v", cmd.Aliases)
	}
}

func TestCommandBuilder_Args(t *testing.T) {
	cmd := NewCommand("test", "short").Args(cobra.ExactArgs(1)).Build()
	if cmd.Args == nil {
		t.Error("expected Args to be set")
	}
}

func TestCommandBuilder_RunE(t *testing.T) {
	called := false
	cmd := NewCommand("test", "short").RunE(func(cmd *cobra.Command, args []string) error {
		called = true
		return nil
	}).Build()
	if cmd.RunE == nil {
		t.Fatal("expected RunE to be set")
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if !called {
		t.Error("RunE was not called")
	}
}

func TestCommandBuilder_AddSubcommand(t *testing.T) {
	sub := &cobra.Command{Use: "sub", Short: "a subcommand"}
	cmd := NewCommand("parent", "short").AddSubcommand(sub).Build()
	if !cmd.HasSubCommands() {
		t.Error("expected parent to have subcommands")
	}
	found := false
	for _, c := range cmd.Commands() {
		if c.Use == "sub" {
			found = true
		}
	}
	if !found {
		t.Error("subcommand 'sub' not found")
	}
}

func TestCommandBuilder_Flags(t *testing.T) {
	b := NewCommand("test", "short")
	flagsCmd := b.Flags()
	if flagsCmd == nil {
		t.Fatal("Flags() returned nil")
	}
	// Flags() returns the underlying command for adding flags
	if flagsCmd.Use != "test" {
		t.Errorf("Flags().Use = %q, want %q", flagsCmd.Use, "test")
	}
}

func TestCommandBuilder_Chaining(t *testing.T) {
	cmd := NewCommand("deploy", "Deploy resources").
		Long("Deploy resources to a cluster").
		Example("  deploy --namespace default").
		Aliases("d").
		Hidden().
		Build()

	if cmd.Use != "deploy" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Long != "Deploy resources to a cluster" {
		t.Errorf("Long = %q", cmd.Long)
	}
	if !cmd.Hidden {
		t.Error("expected hidden")
	}
	if len(cmd.Aliases) != 1 || cmd.Aliases[0] != "d" {
		t.Errorf("Aliases = %v", cmd.Aliases)
	}
}

func TestContextWithTimeout(t *testing.T) {
	parent := context.Background()
	ctx, cancel := ContextWithTimeout(parent, 5*time.Second)
	defer cancel()

	if ctx == nil {
		t.Fatal("ctx is nil")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline")
	}
	if time.Until(deadline) > 6*time.Second {
		t.Error("deadline is too far in the future")
	}
	if time.Until(deadline) < 4*time.Second {
		t.Error("deadline is too soon")
	}
}

func TestContextWithTimeout_Cancel(t *testing.T) {
	ctx, cancel := ContextWithTimeout(context.Background(), 30*time.Second)
	cancel()

	select {
	case <-ctx.Done():
		// expected
	case <-time.After(time.Second):
		t.Error("context was not cancelled")
	}
}

func TestTimeoutConstants(t *testing.T) {
	if DefaultTimeout != 30*time.Second {
		t.Errorf("DefaultTimeout = %v", DefaultTimeout)
	}
	if LongTimeout != 60*time.Second {
		t.Errorf("LongTimeout = %v", LongTimeout)
	}
	if ShortTimeout != 10*time.Second {
		t.Errorf("ShortTimeout = %v", ShortTimeout)
	}
	if ShortTimeout >= DefaultTimeout {
		t.Error("ShortTimeout should be less than DefaultTimeout")
	}
	if DefaultTimeout >= LongTimeout {
		t.Error("DefaultTimeout should be less than LongTimeout")
	}
}
