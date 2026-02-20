// Package cmdutil provides utilities for building CLI commands.
package cmdutil

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// CommandBuilder helps construct cobra commands with consistent patterns.
type CommandBuilder struct {
	cmd *cobra.Command
}

// NewCommand creates a new command builder.
func NewCommand(use, short string) *CommandBuilder {
	return &CommandBuilder{
		cmd: &cobra.Command{
			Use:   use,
			Short: short,
		},
	}
}

// Long sets the long description.
func (b *CommandBuilder) Long(long string) *CommandBuilder {
	b.cmd.Long = long
	return b
}

// Example sets example usage.
func (b *CommandBuilder) Example(example string) *CommandBuilder {
	b.cmd.Example = example
	return b
}

// Args sets argument validation.
func (b *CommandBuilder) Args(args cobra.PositionalArgs) *CommandBuilder {
	b.cmd.Args = args
	return b
}

// Hidden marks the command as hidden.
func (b *CommandBuilder) Hidden() *CommandBuilder {
	b.cmd.Hidden = true
	return b
}

// Aliases sets command aliases.
func (b *CommandBuilder) Aliases(aliases ...string) *CommandBuilder {
	b.cmd.Aliases = aliases
	return b
}

// RunE sets the run function.
func (b *CommandBuilder) RunE(fn func(cmd *cobra.Command, args []string) error) *CommandBuilder {
	b.cmd.RunE = fn
	return b
}

// AddSubcommand adds a subcommand.
func (b *CommandBuilder) AddSubcommand(sub *cobra.Command) *CommandBuilder {
	b.cmd.AddCommand(sub)
	return b
}

// Build returns the constructed command.
func (b *CommandBuilder) Build() *cobra.Command {
	return b.cmd
}

// Flags returns the command's flag set for adding flags.
func (b *CommandBuilder) Flags() *cobra.Command {
	return b.cmd
}

// ContextWithTimeout creates a context with timeout and signal handling.
// Returns the context, cancel function, and a cleanup function to call via defer.
func ContextWithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-signalChan:
			fmt.Fprintf(os.Stderr, "\nInterrupted, cancelling...\n")
			cancel()
		case <-ctx.Done():
		}
	}()

	return ctx, func() {
		signal.Stop(signalChan)
		cancel()
	}
}

// DefaultTimeout is the standard timeout for API operations.
const DefaultTimeout = 30 * time.Second

// LongTimeout is used for operations that may take longer (e.g., login, connect).
const LongTimeout = 60 * time.Second

// ShortTimeout is used for quick operations (e.g., status checks).
const ShortTimeout = 10 * time.Second
