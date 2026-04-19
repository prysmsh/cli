package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
)

func newInstallCommand() *cobra.Command {
	var withCollector bool
	var hostName string

	cmd := &cobra.Command{
		Use:   "install --ssh <user@host>",
		Short: "Install the Prysm agent on a remote host",
		Long: `Install the Prysm agent on a remote Linux host via SSH.

Creates an agent token, generates a docker-compose stack, copies it to
the remote host, and starts the agent — all over SSH. No need for prysm
to be installed on the remote host.

After installation the host is reachable through the mesh:
  prysm ssh <hostname>
  prysm mesh peers`,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, _ := cmd.Flags().GetString("ssh")
			target = strings.TrimSpace(target)
			if target == "" {
				return fmt.Errorf("--ssh flag is required (e.g. prysm install --ssh root@192.168.1.100)")
			}

			return runInstall(cmd, target, hostName, withCollector)
		},
	}

	cmd.Flags().String("ssh", "", "SSH target (user@host or host)")
	cmd.MarkFlagRequired("ssh")
	cmd.Flags().StringVar(&hostName, "name", "", "host name for the agent (defaults to remote hostname)")
	cmd.Flags().BoolVar(&withCollector, "collector", false, "include eBPF collector on the remote host")
	return cmd
}

func runInstall(cmd *cobra.Command, target, hostName string, withCollector bool) error {
	a := MustApp()

	// 1. Verify SSH connectivity
	fmt.Println(style.Info.Render(fmt.Sprintf("Connecting to %s...", target)))
	if err := sshRun(target, "true"); err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	fmt.Println(style.Success.Render("SSH connection OK"))

	// 2. Get remote hostname if not provided
	if hostName == "" {
		out, err := sshOutput(target, "hostname")
		if err != nil {
			return fmt.Errorf("get remote hostname: %w", err)
		}
		hostName = strings.TrimSpace(out)
	}
	fmt.Println(style.Info.Render(fmt.Sprintf("Host name: %s", hostName)))

	// 3. Check Docker is available on remote
	if err := sshRun(target, "docker info >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("Docker is not running on %s — install Docker first", target)
	}
	fmt.Println(style.Success.Render("Docker available on remote host"))

	// 4. Create agent token via API
	type tokenReq struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	type tokenResp struct {
		Token struct {
			Token string `json:"token"`
		} `json:"token"`
	}
	var tokenResult tokenResp
	_, tokenErr := a.API.Do(cmd.Context(), "POST", "/tokens", tokenReq{
		Name:        fmt.Sprintf("docker-agent-%s", hostName),
		Permissions: []string{"*"},
	}, &tokenResult)
	if tokenErr != nil {
		return fmt.Errorf("create agent token: %w", tokenErr)
	}
	agentToken := tokenResult.Token.Token
	if agentToken == "" {
		return fmt.Errorf("empty token in API response")
	}
	fmt.Println(style.Success.Render("Agent token created"))

	// 5. Get config
	backendURL := strings.TrimSuffix(a.Config.APIBaseURL, "/api/v1")
	derpURL := a.Config.DERPServerURL

	// 6. Get org ID from profile
	profile, err := a.API.GetProfile(cmd.Context())
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}
	var orgID uint64
	if len(profile.Organizations) > 0 {
		orgID = uint64(profile.Organizations[0].ID)
	}

	// 7. Generate docker-compose.yml
	compose := generateInstallCompose(hostName, backendURL, agentToken, orgID, derpURL, withCollector)

	// 8. Write compose to remote host via SSH
	fmt.Println(style.Info.Render("Deploying agent to remote host..."))
	writeCmd := fmt.Sprintf("mkdir -p ~/.prysm && cat > ~/.prysm/docker-compose.yml")
	sshCmd := exec.Command("ssh", target, writeCmd)
	sshCmd.Stdin = strings.NewReader(compose)
	sshCmd.Stderr = os.Stderr
	if err := sshCmd.Run(); err != nil {
		return fmt.Errorf("write compose file to remote: %w", err)
	}

	// 9. Pull images and start the agent
	if err := ui.WithSpinner("Pulling images and starting agent...", func() error {
		return sshRun(target, "cd ~/.prysm && docker compose pull -q && docker compose up -d")
	}); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}
	fmt.Println(style.Success.Render("Agent started on " + hostName))

	// 10. Poll for registration
	registered := false
	_ = ui.WithSpinner("Waiting for agent to register...", func() error {
		type clustersResp struct {
			Clusters []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"clusters"`
		}
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)
			var resp clustersResp
			if _, err := a.API.Do(cmd.Context(), "GET", "/clusters", nil, &resp); err != nil {
				continue
			}
			for _, c := range resp.Clusters {
				if strings.EqualFold(c.Name, hostName) && c.Status == "connected" {
					registered = true
					return nil
				}
			}
		}
		return nil
	})

	if registered {
		fmt.Println(style.Success.Render(fmt.Sprintf("Host %q registered and connected to mesh!", hostName)))
	} else {
		fmt.Println(style.Warning.Render("Agent started but not registered yet — may take a few more minutes."))
	}

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  prysm clusters              — view registered hosts\n")
	fmt.Printf("  prysm ssh %s       — SSH via mesh\n", hostName)
	fmt.Printf("  prysm mesh peers            — view mesh peers\n")

	return nil
}

func sshRun(target, command string) error {
	devnull, _ := os.Open(os.DevNull)
	defer devnull.Close()
	cmd := exec.Command("ssh", "-n", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", target, command)
	cmd.Stdin = devnull
	return cmd.Run()
}

func sshOutput(target, command string) (string, error) {
	devnull, _ := os.Open(os.DevNull)
	defer devnull.Close()
	cmd := exec.Command("ssh", "-n", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", target, command)
	cmd.Stdin = devnull
	out, err := cmd.Output()
	return string(out), err
}

func generateInstallCompose(hostName, backendURL, token string, orgID uint64, derpURL string, withCollector bool) string {
	var sb strings.Builder
	sb.WriteString("# Prysm Agent - installed by `prysm install`\n")
	sb.WriteString(fmt.Sprintf("# Host: %s\n", hostName))
	sb.WriteString(fmt.Sprintf("# Backend: %s\n\n", backendURL))
	sb.WriteString("services:\n")
	sb.WriteString("  prysm-agent:\n")
	sb.WriteString("    image: ghcr.io/prysmsh/agent:latest\n")
	sb.WriteString("    container_name: prysm-agent\n")
	sb.WriteString("    restart: unless-stopped\n")
	sb.WriteString("    network_mode: host\n")
	sb.WriteString("    privileged: true\n")
	sb.WriteString("    environment:\n")
	sb.WriteString(fmt.Sprintf("      BACKEND_URL: %s\n", quote(backendURL)))
	sb.WriteString(fmt.Sprintf("      AGENT_TOKEN: %s\n", quote(token)))
	sb.WriteString(fmt.Sprintf("      CLUSTER_NAME: %s\n", quote(hostName)))
	sb.WriteString(fmt.Sprintf("      ORGANIZATION_ID: %s\n", quote(fmt.Sprintf("%d", orgID))))
	sb.WriteString(fmt.Sprintf("      AGENT_MODE: %s\n", quote("docker")))
	if derpURL != "" {
		sb.WriteString(fmt.Sprintf("      DERP_SERVERS: %s\n", quote(derpURL)))
	}
	sb.WriteString("    volumes:\n")
	sb.WriteString("      - /var/run/docker.sock:/var/run/docker.sock:ro\n")
	sb.WriteString("      - prysm-agent-data:/var/lib/prysm\n")
	sb.WriteString("      - /proc:/host/proc:ro\n")
	sb.WriteString("      - /sys:/host/sys:ro\n")

	if withCollector {
		sb.WriteString("\n  prysm-ebpf-collector:\n")
		sb.WriteString("    image: ghcr.io/prysmsh/prysm/ebpf-collector:latest\n")
		sb.WriteString("    container_name: prysm-ebpf-collector\n")
		sb.WriteString("    restart: unless-stopped\n")
		sb.WriteString("    network_mode: host\n")
		sb.WriteString("    privileged: true\n")
		sb.WriteString("    environment:\n")
		sb.WriteString(fmt.Sprintf("      BACKEND_URL: %s\n", quote(backendURL)))
		sb.WriteString(fmt.Sprintf("      AGENT_TOKEN: %s\n", quote(token)))
		sb.WriteString(fmt.Sprintf("      CLUSTER_NAME: %s\n", quote(hostName)))
		sb.WriteString(fmt.Sprintf("      ORGANIZATION_ID: %s\n", quote(fmt.Sprintf("%d", orgID))))
		sb.WriteString("      PRYSM_ALERT_STDOUT: \"true\"\n")
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - /proc:/host/proc:ro\n")
		sb.WriteString("      - /sys:/host/sys:ro\n")
		sb.WriteString("      - /sys/kernel/debug:/sys/kernel/debug:ro\n")
		sb.WriteString("      - /sys/fs/bpf:/sys/fs/bpf\n")
	}

	sb.WriteString("\nvolumes:\n")
	sb.WriteString("  prysm-agent-data:\n")
	return sb.String()
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
