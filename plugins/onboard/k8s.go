package onboard

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/warp-run/prysm-cli/internal/plugin"
)

const defaultChartRef = "oci://ghcr.io/prysmsh/charts/prysm-agent"

// k8sFlags holds the parsed CLI flags for non-interactive mode.
type k8sFlags struct {
	name         string
	namespace    string
	kubeCtx      string
	chart        string
	backendURL   string      // agent-facing backend URL override
	agentDERPURL string      // agent-facing DERP URL override
	setValues    stringSlice // repeatable --set
	setJSONs     stringSlice // repeatable --set-json
	wait         bool
	timeout      string
	skipPoll     bool
}

// stringSlice implements flag.Value for repeatable --set / --set-json flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// parseK8sFlags parses non-interactive flags from raw args.
// Returns nil when no flags are present (interactive mode).
func parseK8sFlags(args []string) (*k8sFlags, error) {
	f := &k8sFlags{}
	fs := flag.NewFlagSet("onboard-k8s", flag.ContinueOnError)
	fs.StringVar(&f.name, "name", "", "cluster name (required for non-interactive)")
	fs.StringVar(&f.namespace, "namespace", "prysm-system", "Kubernetes namespace")
	fs.StringVar(&f.kubeCtx, "kube-context", "", "kubectl/helm context")
	fs.StringVar(&f.chart, "chart", defaultChartRef, "Helm chart reference")
	fs.StringVar(&f.backendURL, "backend-url", "", "agent-facing backend URL (default: derived from --api-url)")
	fs.StringVar(&f.agentDERPURL, "agent-derp-url", "", "agent-facing DERP URL (default: from --derp-url)")
	fs.Var(&f.setValues, "set", "extra helm --set value (repeatable)")
	fs.Var(&f.setJSONs, "set-json", "extra helm --set-json value (repeatable)")
	fs.BoolVar(&f.wait, "wait", false, "pass --wait to helm")
	fs.StringVar(&f.timeout, "timeout", "", "pass --timeout to helm (e.g. 120s)")
	fs.BoolVar(&f.skipPoll, "skip-poll", false, "skip post-install registration polling")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if f.name == "" {
		return nil, nil // no --name → interactive mode
	}
	return f, nil
}

// onboardK8s implements the Kubernetes onboarding skill.
// It verifies auth, checks for helm, prompts for cluster details,
// creates an agent token, installs the Helm chart, and polls for registration.
func (p *OnboardPlugin) onboardK8s(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	// Parse flags from raw args (everything after "k8s" subcommand name).
	var extraArgs []string
	if len(req.Args) > 1 {
		extraArgs = req.Args[1:]
	}
	flags, err := parseK8sFlags(extraArgs)
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

	// 2. Check helm is installed
	if _, err := exec.LookPath("helm"); err != nil {
		_ = p.host.Log(ctx, plugin.LogLevelError, "helm is not installed or not in PATH")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "Install it from: https://helm.sh/docs/intro/install/")
		return plugin.ExecuteResponse{ExitCode: 1, Error: "helm not found"}
	}
	_ = p.host.Log(ctx, plugin.LogLevelSuccess, "helm found")

	// 3. Resolve cluster name and namespace
	var clusterName, namespace, kubeCtx, chartRef string
	var extraSets, extraSetJSONs []string
	var backendURLOverride, derpURLOverride string
	var helmWait bool
	var helmTimeout string
	var skipPoll bool

	if interactive {
		// Interactive prompts
		clusterName, err = p.host.PromptInput(ctx, "Cluster name", false)
		if err != nil || clusterName == "" {
			return plugin.ExecuteResponse{ExitCode: 1, Error: "cluster name is required"}
		}

		namespace, err = p.host.PromptInput(ctx, "Namespace (default: prysm-system)", false)
		if err != nil {
			return plugin.ExecuteResponse{ExitCode: 1, Error: err.Error()}
		}
		if namespace == "" {
			namespace = "prysm-system"
		}
		chartRef = defaultChartRef
	} else {
		clusterName = flags.name
		namespace = flags.namespace
		kubeCtx = flags.kubeCtx
		chartRef = flags.chart
		backendURLOverride = flags.backendURL
		derpURLOverride = flags.agentDERPURL
		extraSets = flags.setValues
		extraSetJSONs = flags.setJSONs
		helmWait = flags.wait
		helmTimeout = flags.timeout
		skipPoll = flags.skipPoll
	}

	// 4. Create agent token via API
	_ = p.host.Log(ctx, plugin.LogLevelInfo, "Creating agent token...")
	tokenBody, _ := json.Marshal(map[string]interface{}{
		"name":        fmt.Sprintf("agent-%s", clusterName),
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

	// 5. Get config for backend URL and DERP URL
	cfg, err := p.host.GetConfig(ctx)
	if err != nil {
		return plugin.ExecuteResponse{ExitCode: 1, Error: err.Error()}
	}

	// Use --backend-url override if provided, otherwise derive from CLI config
	backendURL := backendURLOverride
	if backendURL == "" {
		backendURL = strings.TrimSuffix(cfg.APIBaseURL, "/api/v1")
	}

	// Use --agent-derp-url override if provided, otherwise use CLI's DERP URL
	derpURL := derpURLOverride
	if derpURL == "" {
		derpURL = cfg.DERPURL
	}

	// 6. Install helm chart
	_ = p.host.Log(ctx, plugin.LogLevelInfo, "Installing Prysm agent Helm chart...")
	helmArgs := []string{
		"upgrade", "--install", "prysm-agent",
		chartRef,
		"--namespace", namespace,
		"--create-namespace",
		"--set", fmt.Sprintf("configSecret.data.AGENT_TOKEN=%s", tokenResp.Token),
		"--set", fmt.Sprintf("configSecret.data.BACKEND_URL=%s", backendURL),
		"--set", fmt.Sprintf("configSecret.data.CLUSTER_NAME=%s", clusterName),
		"--set", fmt.Sprintf("configSecret.data.ORGANIZATION_ID=%d", auth.OrgID),
	}
	if derpURL != "" {
		helmArgs = append(helmArgs,
			"--set", fmt.Sprintf("configSecret.data.DERP_SERVERS=%s", derpURL),
			"--set", fmt.Sprintf("configSecret.data.DERP_SERVER=%s", derpURL),
		)
	}
	if kubeCtx != "" {
		helmArgs = append(helmArgs, "--kube-context", kubeCtx)
	}
	for _, sv := range extraSets {
		helmArgs = append(helmArgs, "--set", sv)
	}
	for _, sj := range extraSetJSONs {
		helmArgs = append(helmArgs, "--set-json", sj)
	}
	if helmWait {
		helmArgs = append(helmArgs, "--wait")
	}
	if helmTimeout != "" {
		helmArgs = append(helmArgs, "--timeout", helmTimeout)
	}

	cmd := exec.CommandContext(ctx, "helm", helmArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = p.host.Log(ctx, plugin.LogLevelError, string(output))
		return plugin.ExecuteResponse{ExitCode: 1, Error: fmt.Sprintf("helm install failed: %v", err)}
	}
	_ = p.host.Log(ctx, plugin.LogLevelSuccess, "Helm chart installed")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, string(output))

	// 7. Poll for agent registration (unless --skip-poll)
	if skipPoll {
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "Skipping registration poll (--skip-poll)")
	} else {
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "Waiting for agent to register...")
		registered := false
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)
			status, body, err := p.host.APIRequest(ctx, "GET", "/clusters", nil)
			if err != nil || status != 200 {
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
				if strings.EqualFold(c.Name, clusterName) && c.Status == "connected" {
					registered = true
					break
				}
			}
			if registered {
				break
			}
		}

		if registered {
			_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Cluster %q registered and connected!", clusterName))
		} else {
			_ = p.host.Log(ctx, plugin.LogLevelWarning, "Agent has not registered yet. It may take a few more minutes.")
			_ = p.host.Log(ctx, plugin.LogLevelPlain, "Check status with: prysm clusters")
		}
	}

	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "Next steps:")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  prysm clusters              — view registered clusters")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  prysm connect k8s           — get kubeconfig for cluster access")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  prysm security events       — view security events")

	return plugin.ExecuteResponse{ExitCode: 0}
}
