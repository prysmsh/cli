package onboard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/warp-run/prysm-cli/internal/plugin"
)

// onboardDocker implements the Docker host onboarding skill.
// It verifies auth, prompts for host name, creates a token,
// generates a docker-compose.yml, and optionally runs docker compose up.
func (p *OnboardPlugin) onboardDocker(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	return p.doDockerOnboard(ctx, req, false)
}

// onboardDockerCompose implements the full-stack Docker onboarding skill.
// Same as docker but includes an eBPF collector sidecar.
func (p *OnboardPlugin) onboardDockerCompose(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	return p.doDockerOnboard(ctx, req, true)
}

func (p *OnboardPlugin) doDockerOnboard(ctx context.Context, req plugin.ExecuteRequest, fullStack bool) plugin.ExecuteResponse {
	// 1. Verify auth
	auth, err := p.host.GetAuthContext(ctx)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: err.Error()}
	}
	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Authenticated as %s (org: %s)", auth.UserEmail, auth.OrgName))

	// 2. Prompt for host name
	hostname, _ := os.Hostname()
	hostName, err := p.host.PromptInput(ctx, fmt.Sprintf("Host name (default: %s)", hostname), false)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: err.Error()}
	}
	if hostName == "" {
		hostName = hostname
	}

	// 3. Create agent token via API
	_ = p.host.Log(ctx, plugin.LogLevelInfo, "Creating agent token...")
	tokenBody, _ := json.Marshal(map[string]interface{}{
		"name":        fmt.Sprintf("docker-agent-%s", hostName),
		"permissions": []string{"*"},
	})
	status, respBody, err := p.host.APIRequest(ctx, "POST", "/tokens", tokenBody)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("failed to create token: %v", err)}
	}
	if status < 200 || status >= 300 {
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("failed to create token (HTTP %d): %s", status, string(respBody))}
	}

	var tokenResp struct {
		Token string `json:"token"`
		ID    uint   `json:"id"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: "failed to parse token response"}
	}
	_ = p.host.Log(ctx, plugin.LogLevelSuccess, "Agent token created")

	// 4. Get config for backend URL and DERP servers
	cfg, err := p.host.GetConfig(ctx)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: err.Error()}
	}
	backendURL := strings.TrimSuffix(cfg.APIBaseURL, "/api/v1")

	// 5. Generate docker-compose.yml
	compose := generateDockerCompose(hostName, backendURL, tokenResp.Token, auth.OrgID, cfg.DERPURL, fullStack)
	outputFile := "prysm-agent-compose.yml"

	if err := os.WriteFile(outputFile, []byte(compose), 0o644); err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("failed to write %s: %v", outputFile, err)}
	}
	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Generated %s", outputFile))

	// 6. Prompt to run docker compose
	run, err := p.host.PromptConfirm(ctx, "Run `docker compose up -d` now?")
	if err != nil {
		_ = p.host.Log(ctx, plugin.LogLevelWarning, "Could not read confirmation, skipping auto-start")
	}

	if run {
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "Starting agent...")
		cmd := exec.CommandContext(ctx, "docker", "compose", "-f", outputFile, "up", "-d")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("docker compose up failed: %v", err)}
		}
		_ = p.host.Log(ctx, plugin.LogLevelSuccess, "Agent started")

		// 7. Poll for agent registration
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "Waiting for agent to register...")
		registered := false
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)
			st, body, err := p.host.APIRequest(ctx, "GET", "/clusters", nil)
			if err != nil || st != 200 {
				continue
			}
			var resp struct {
				Clusters []struct {
					Name   string `json:"name"`
					Status string `json:"status"`
				} `json:"clusters"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				continue
			}
			for _, c := range resp.Clusters {
				if strings.EqualFold(c.Name, hostName) && c.Status == "connected" {
					registered = true
					break
				}
			}
			if registered {
				break
			}
		}

		if registered {
			_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Host %q registered and connected!", hostName))
		} else {
			_ = p.host.Log(ctx, plugin.LogLevelWarning, "Agent has not registered yet. It may take a few more minutes.")
		}
	} else {
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "To start the agent manually:")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  docker compose -f %s up -d", outputFile))
	}

	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "Next steps:")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  prysm clusters              — view registered hosts")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  prysm security events       — view security events")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  docker compose -f %s logs -f  — view agent logs", outputFile))

	return plugin.ExecuteResponse{ExitCode: 0}
}

func generateDockerCompose(hostName, backendURL, token string, orgID uint64, derpURL string, fullStack bool) string {
	var sb strings.Builder
	sb.WriteString("# Prysm Agent - Docker Host\n")
	sb.WriteString("# Generated by `prysm onboard docker`\n")
	sb.WriteString("#\n")
	sb.WriteString(fmt.Sprintf("# Host: %s\n", hostName))
	sb.WriteString(fmt.Sprintf("# Backend: %s\n", backendURL))
	sb.WriteString("\n")
	sb.WriteString("services:\n")
	sb.WriteString("  prysm-agent:\n")
	sb.WriteString("    image: ghcr.io/prysmsh/prysm-agent:latest-docker\n")
	sb.WriteString("    container_name: prysm-agent\n")
	sb.WriteString("    restart: unless-stopped\n")
	sb.WriteString("    network_mode: host\n")
	sb.WriteString("    privileged: true\n")
	sb.WriteString("    environment:\n")
	sb.WriteString(fmt.Sprintf("      BACKEND_URL: %q\n", backendURL))
	sb.WriteString(fmt.Sprintf("      AGENT_TOKEN: %q\n", token))
	sb.WriteString(fmt.Sprintf("      CLUSTER_NAME: %q\n", hostName))
	sb.WriteString(fmt.Sprintf("      ORGANIZATION_ID: %q\n", fmt.Sprintf("%d", orgID)))
	sb.WriteString(fmt.Sprintf("      AGENT_MODE: %q\n", "docker"))
	if derpURL != "" {
		sb.WriteString(fmt.Sprintf("      DERP_SERVERS: %q\n", derpURL))
	}
	sb.WriteString("    volumes:\n")
	sb.WriteString("      - /var/run/docker.sock:/var/run/docker.sock:ro\n")
	sb.WriteString("      - prysm-agent-data:/var/lib/prysm\n")
	sb.WriteString("      - /proc:/host/proc:ro\n")
	sb.WriteString("      - /sys:/host/sys:ro\n")

	if fullStack {
		sb.WriteString("\n")
		sb.WriteString("  prysm-ebpf-collector:\n")
		sb.WriteString("    image: ghcr.io/prysmsh/ebpf-collector:latest\n")
		sb.WriteString("    container_name: prysm-ebpf-collector\n")
		sb.WriteString("    restart: unless-stopped\n")
		sb.WriteString("    network_mode: host\n")
		sb.WriteString("    privileged: true\n")
		sb.WriteString("    environment:\n")
		sb.WriteString(fmt.Sprintf("      BACKEND_URL: %q\n", backendURL))
		sb.WriteString(fmt.Sprintf("      AGENT_TOKEN: %q\n", token))
		sb.WriteString(fmt.Sprintf("      CLUSTER_NAME: %q\n", hostName))
		sb.WriteString(fmt.Sprintf("      ORGANIZATION_ID: %q\n", fmt.Sprintf("%d", orgID)))
		sb.WriteString("      PRYSM_ALERT_STDOUT: \"true\"\n")
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - /proc:/host/proc:ro\n")
		sb.WriteString("      - /sys:/host/sys:ro\n")
		sb.WriteString("      - /sys/kernel/debug:/sys/kernel/debug:ro\n")
		sb.WriteString("      - /sys/fs/bpf:/sys/fs/bpf\n")
	}

	sb.WriteString("\n")
	sb.WriteString("volumes:\n")
	sb.WriteString("  prysm-agent-data:\n")

	return sb.String()
}
