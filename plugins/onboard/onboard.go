// Package onboard implements the builtin "onboard" plugin for setting up
// Prysm agents on Kubernetes clusters and Docker hosts.
package onboard

import (
	"context"
	"fmt"
	"strings"

	"github.com/warp-run/prysm-cli/internal/plugin"
)

// OnboardPlugin is a builtin plugin that provides agent onboarding commands.
type OnboardPlugin struct {
	host plugin.HostServices
}

// New creates a new onboard plugin with the given host services.
// Pass nil for host if registering commands eagerly; call SetHost before Execute.
func New(host plugin.HostServices) *OnboardPlugin {
	return &OnboardPlugin{host: host}
}

// SetHost sets (or replaces) the host services used by this plugin.
func (p *OnboardPlugin) SetHost(host plugin.HostServices) {
	p.host = host
}

// Manifest returns the plugin's metadata and command tree.
func (p *OnboardPlugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:        "onboard",
		Version:     "0.1.0",
		Description: "Agent onboarding for Kubernetes and Docker hosts",
		Commands: []plugin.CommandSpec{
			{
				Name:  "onboard",
				Short: "Onboard a new agent (Kubernetes or Docker)",
				Subcommands: []plugin.CommandSpec{
					{
						Name:               "k8s",
						Short:              "Onboard a Kubernetes cluster using Helm",
						DisableFlagParsing: true,
					},
					{
						Name:  "docker",
						Short: "Onboard a Docker host (generates docker-compose.yml)",
					},
					{
						Name:  "docker-compose",
						Short: "Onboard a Docker host with full stack (agent + eBPF collector)",
					},
				},
			},
		},
	}
}

// Execute dispatches the command to the appropriate skill.
func (p *OnboardPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	if len(req.Args) == 0 {
		return p.showMenu(ctx)
	}

	switch req.Args[0] {
	case "k8s":
		return p.onboardK8s(ctx, req)
	case "docker":
		return p.onboardDocker(ctx, req)
	case "docker-compose":
		return p.onboardDockerCompose(ctx, req)
	default:
		return p.showMenu(ctx)
	}
}

// showMenu presents an interactive picker when no subcommand is given.
func (p *OnboardPlugin) showMenu(ctx context.Context) plugin.ExecuteResponse {
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelInfo, "Prysm Agent Onboarding")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "Choose an onboarding method:")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  1. Kubernetes cluster (Helm chart)")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  2. Docker host (single container)")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  3. Docker Compose (full stack)")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")

	choice, err := p.host.PromptInput(ctx, "Enter choice (1-3)", false)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("input error: %v", err)}
	}

	choice = strings.TrimSpace(choice)
	switch choice {
	case "1":
		return p.onboardK8s(ctx, plugin.ExecuteRequest{})
	case "2":
		return p.onboardDocker(ctx, plugin.ExecuteRequest{})
	case "3":
		return p.onboardDockerCompose(ctx, plugin.ExecuteRequest{})
	default:
		return plugin.ExecuteResponse{ExitCode: 1, Error: "invalid choice"}
	}
}
