package cmd

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var sshTargetRE = regexp.MustCompile(`^[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+$`)

func newDockerCommand() *cobra.Command {
	dockerCmd := &cobra.Command{
		Use:   "docker",
		Short: "Docker helper commands",
		// Skip app init — docker commands work without Prysm config/auth.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	}

	dockerCmd.AddCommand(newDockerContextCommand())
	return dockerCmd
}

func newDockerContextCommand() *cobra.Command {
	var (
		contextName  string
		useDefault   bool
		listContexts bool
	)

	cmd := &cobra.Command{
		Use:   "context [user@host]",
		Short: "Create and switch Docker context to a remote SSH machine",
		Long: `Create a Docker context pointing at a remote host via SSH and switch to it.

Examples:
  prysm docker context root@10.0.0.5
  prysm docker context --name staging deploy@staging.example.com
  prysm docker context --default
  prysm docker context --list`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := exec.LookPath("docker"); err != nil {
				return fmt.Errorf("docker CLI not found in PATH; please install Docker first")
			}

			if listContexts {
				return dockerContextList()
			}

			if useDefault {
				return dockerContextUse("default")
			}

			if len(args) == 0 {
				return cmd.Help()
			}

			target := args[0]
			if !sshTargetRE.MatchString(target) {
				return fmt.Errorf("invalid SSH target %q; expected format user@host", target)
			}

			name := contextName
			if name == "" {
				// Derive name from the host portion: user@my-server → prysm-my-server
				parts := strings.SplitN(target, "@", 2)
				name = "prysm-" + parts[1]
			}

			return dockerContextCreate(name, target)
		},
	}

	cmd.Flags().StringVar(&contextName, "name", "", "context name (default: prysm-<host>)")
	cmd.Flags().BoolVar(&useDefault, "default", false, "switch back to the default Docker context")
	cmd.Flags().BoolVar(&listContexts, "list", false, "list all Docker contexts")

	return cmd
}

// dockerContextCreate creates (or recreates) a Docker context and switches to it.
func dockerContextCreate(name, target string) error {
	// Remove existing context with the same name so create is idempotent.
	// Ignore errors — the context may not exist yet.
	_ = execDocker("context", "rm", "-f", name)

	if err := execDocker("context", "create", name, "--docker", "host=ssh://"+target); err != nil {
		return fmt.Errorf("create docker context: %w", err)
	}

	if err := dockerContextUse(name); err != nil {
		return err
	}

	color.New(color.FgGreen).Printf("Docker context %q created and activated (ssh://%s)\n", name, target)
	return nil
}

// dockerContextUse switches to the named Docker context.
func dockerContextUse(name string) error {
	if err := execDocker("context", "use", name); err != nil {
		return fmt.Errorf("switch docker context: %w", err)
	}
	if name == "default" {
		color.New(color.FgGreen).Println("Switched back to default Docker context")
	}
	return nil
}

// dockerContextList prints all Docker contexts.
func dockerContextList() error {
	out, err := exec.Command("docker", "context", "ls").CombinedOutput()
	if err != nil {
		return fmt.Errorf("list docker contexts: %w", err)
	}
	fmt.Print(string(out))
	return nil
}

// execDocker runs a docker command, forwarding combined output on failure.
func execDocker(args ...string) error {
	cmd := exec.Command("docker", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
