package onboard

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/prysmsh/cli/internal/charts"
	"github.com/prysmsh/cli/internal/plugin"
)

// collectorFlags holds the parsed CLI flags for non-interactive collector mode.
type collectorFlags struct {
	cluster     string
	namespace   string
	kubeCtx     string
	chart       string
	composeFile string
}

// parseCollectorFlags parses non-interactive flags from raw args.
// Returns nil when no flags are present (interactive mode).
func parseCollectorFlags(args []string) (*collectorFlags, error) {
	f := &collectorFlags{}
	fs := flag.NewFlagSet("onboard-collector", flag.ContinueOnError)
	fs.StringVar(&f.cluster, "cluster", "", "cluster name (required for non-interactive)")
	fs.StringVar(&f.namespace, "namespace", "prysm-system", "Kubernetes namespace")
	fs.StringVar(&f.kubeCtx, "kube-context", "", "kubectl/helm context")
	fs.StringVar(&f.chart, "chart", "", "Helm chart override (default: embedded)")
	fs.StringVar(&f.composeFile, "compose-file", "", "docker-compose file path (for docker hosts)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if f.cluster == "" {
		return nil, nil // no --cluster → interactive mode
	}
	return f, nil
}

// onboardCollector adds the eBPF collector to an already-onboarded cluster.
func (p *OnboardPlugin) onboardCollector(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	// Parse flags from raw args (everything after "collector" subcommand name).
	var extraArgs []string
	if len(req.Args) > 1 {
		extraArgs = req.Args[1:]
	}
	flags, err := parseCollectorFlags(extraArgs)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("invalid flags: %v", err)}
	}
	interactive := flags == nil

	// 1. Verify auth
	auth, err := p.host.GetAuthContext(ctx)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: err.Error()}
	}
	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Authenticated as %s (org: %s)", auth.UserEmail, auth.OrgName))

	// 2. Fetch clusters from API
	status, body, err := p.host.APIRequest(ctx, "GET", "/connect/k8s/clusters", nil)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("failed to fetch clusters: %v", err)}
	}
	if status < 200 || status >= 300 {
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("failed to fetch clusters (HTTP %d): %s", status, string(body))}
	}

	var clustersResp struct {
		Clusters []struct {
			Name      string `json:"name"`
			Status    string `json:"status"`
			Namespace string `json:"namespace"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(body, &clustersResp); err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("failed to parse clusters: %v", err)}
	}
	if len(clustersResp.Clusters) == 0 {
		_ = p.host.Log(ctx, plugin.LogLevelWarning, "No clusters found. Onboard a cluster first with: prysm onboard kube")
		return plugin.ExecuteResponse{ExitCode: 1, Error: "no clusters available"}
	}

	// 3. Select cluster
	var clusterName, namespace, kubeCtx, chartRef, composeFile string

	if interactive {
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "Select a cluster:")
		for i, c := range clustersResp.Clusters {
			_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  %d. %s (%s)", i+1, c.Name, c.Status))
		}
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "")

		choice, err := p.host.PromptInput(ctx, "Cluster", false)
		if err != nil {
			return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("input error: %v", err)}
		}
		choice = strings.TrimSpace(choice)

		// Parse as 1-based index or name
		idx := -1
		for i, c := range clustersResp.Clusters {
			if fmt.Sprintf("%d", i+1) == choice || strings.EqualFold(c.Name, choice) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return plugin.ExecuteResponse{ExitCode: 1, Error: "invalid cluster selection"}
		}

		clusterName = clustersResp.Clusters[idx].Name
		namespace = clustersResp.Clusters[idx].Namespace
		if namespace == "" {
			namespace = "prysm-system"
		}
	} else {
		clusterName = flags.cluster
		namespace = flags.namespace
		kubeCtx = flags.kubeCtx
		chartRef = flags.chart
		composeFile = flags.composeFile
	}

	// 4. Get config for backend URL
	cfg, err := p.host.GetConfig(ctx)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: err.Error()}
	}
	backendURL := strings.TrimSuffix(cfg.APIBaseURL, "/api/v1")

	// Resolve chart — use embedded chart unless overridden via --chart
	if chartRef == "" {
		extracted, cleanupDir, extractErr := charts.ExtractAgentChart()
		if extractErr != nil {
			return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("extract embedded chart: %v", extractErr)}
		}
		defer os.RemoveAll(cleanupDir)
		chartRef = extracted
	}

	// 5. Detect mode: try helm status to see if this is a k8s cluster
	helmStatusArgs := []string{"status", "prysm-agent", "-n", namespace}
	if kubeCtx != "" {
		helmStatusArgs = append(helmStatusArgs, "--kube-context", kubeCtx)
	}
	helmStatusCmd := exec.CommandContext(ctx, "helm", helmStatusArgs...)
	if err := helmStatusCmd.Run(); err == nil {
		// K8s path: helm upgrade with --reuse-values
		return p.collectorK8sUpgrade(ctx, clusterName, namespace, kubeCtx, chartRef, backendURL)
	}

	// Docker path: update compose file
	return p.collectorDockerUpgrade(ctx, clusterName, backendURL, composeFile, interactive)
}

// collectorK8sUpgrade runs helm upgrade --reuse-values to enable the eBPF collector.
func (p *OnboardPlugin) collectorK8sUpgrade(ctx context.Context, clusterName, namespace, kubeCtx, chartRef, backendURL string) plugin.ExecuteResponse {
	helmArgs := []string{
		"upgrade", "prysm-agent",
		chartRef,
		"--namespace", namespace,
		"--reuse-values",
		"--set", "configSecret.data.METRICS_ENABLE_EBPF=true",
	}
	if kubeCtx != "" {
		helmArgs = append(helmArgs, "--kube-context", kubeCtx)
	}

	var helmOutput []byte
	helmErr := withSpinner("Upgrading Helm release to enable eBPF collector...", func() error {
		cmd := exec.CommandContext(ctx, "helm", helmArgs...)
		out, err := cmd.CombinedOutput()
		helmOutput = out
		if err != nil {
			return fmt.Errorf("helm upgrade failed: %v\n%s", err, string(out))
		}
		return nil
	})
	if helmErr != nil {
		_ = p.host.Log(ctx, plugin.LogLevelError, helmErr.Error())
		return plugin.ExecuteResponse{ExitCode: 1, Error: helmErr.Error()}
	}

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Done. eBPF collector enabled on %q.", clusterName))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, string(helmOutput))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "Next steps:")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  kubectl -n %s get ds  — check collector pods", namespace))

	return plugin.ExecuteResponse{ExitCode: 0}
}

// collectorDockerUpgrade updates a docker-compose file to add the eBPF collector service.
func (p *OnboardPlugin) collectorDockerUpgrade(ctx context.Context, clusterName, backendURL, composeFile string, interactive bool) plugin.ExecuteResponse {
	if composeFile == "" {
		if interactive {
			var err error
			composeFile, err = p.host.PromptInput(ctx, "Docker compose file path (default: prysm-agent-compose.yml)", false)
			if err != nil {
				return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("input error: %v", err)}
			}
			if composeFile == "" {
				composeFile = "prysm-agent-compose.yml"
			}
		} else {
			composeFile = "prysm-agent-compose.yml"
		}
	}

	// Read existing compose file
	data, err := os.ReadFile(composeFile)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("failed to read %s: %v", composeFile, err)}
	}
	content := string(data)

	// Check if collector is already present
	if strings.Contains(content, "prysm-ebpf-collector") {
		_ = p.host.Log(ctx, plugin.LogLevelWarning, "eBPF collector service already exists in compose file")
		return plugin.ExecuteResponse{ExitCode: 0}
	}

	// Find the volumes section and insert the collector service before it
	collectorService := fmt.Sprintf(`
  prysm-ebpf-collector:
    image: ghcr.io/prysmsh/prysm/ebpf-collector:latest
    container_name: prysm-ebpf-collector
    restart: unless-stopped
    network_mode: host
    privileged: true
    environment:
      BACKEND_URL: %q
      CLUSTER_NAME: %q
      PRYSM_ALERT_STDOUT: "true"
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /sys/kernel/debug:/sys/kernel/debug:ro
      - /sys/fs/bpf:/sys/fs/bpf

`, backendURL, clusterName)

	// Insert before the "volumes:" top-level key
	if idx := strings.LastIndex(content, "\nvolumes:"); idx >= 0 {
		content = content[:idx] + collectorService + content[idx:]
	} else {
		// No volumes section; append to end
		content += collectorService
	}

	if err := os.WriteFile(composeFile, []byte(content), 0o644); err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("failed to write %s: %v", composeFile, err)}
	}

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("eBPF collector added to %s", composeFile))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "Next steps:")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  docker compose -f %s up -d  — restart with collector", composeFile))

	return plugin.ExecuteResponse{ExitCode: 0}
}
