package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
	"github.com/prysmsh/cli/internal/util"
)

func newClustersCommand() *cobra.Command {
	clustersCmd := &cobra.Command{
		Use:     "clusters",
		Aliases: []string{"cluster", "cl"},
		Short:   "Manage Kubernetes clusters",
	}

	clustersCmd.AddCommand(
		newClustersListCommand(),
		newClustersStatusCommand(),
		newClustersMeshDebugCommand(),
		newClustersCheckCommand(),
		newClustersCheckLocalCommand(),
		newClustersInspectCommand(),
		newClustersRemoveCommand(),
		newClustersTokenCommand(),
		newClustersExitCommand(),
		newClustersReconcileCommand(),
	)

	return clustersCmd
}

func newClustersListCommand() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			var data struct {
				Clusters []struct {
					ID             uint   `json:"id"`
					PublicID       string `json:"public_id"`
					Name           string `json:"name"`
					Status         string `json:"status"`
					Region         string `json:"region"`
					KubeVersion    string `json:"kube_version"`
					NodeCount      int    `json:"node_count"`
					PodCount       int    `json:"pod_count"`
					ServiceCount   int    `json:"service_count"`
					NamespaceCount int    `json:"namespace_count"`
					LastSeen       string `json:"last_seen"`
				} `json:"clusters"`
			}
			resp, err := app.API.Do(ctx, "GET", "clusters", nil, &data)
			if err != nil {
				return fmt.Errorf("list clusters: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("list clusters: %s", resp.Status)
			}

			if len(data.Clusters) == 0 {
				fmt.Println("No clusters registered. Deploy an agent to register a cluster.")
				return nil
			}

			headers := []string{"PUBLIC ID", "NAME", "STATUS", "REGION", "NODES", "PODS", "SVCS"}
			rows := make([][]string, 0, len(data.Clusters))
			for _, c := range data.Clusters {
				statusStr := style.Error.Render(c.Status)
				if c.Status == "connected" {
					statusStr = style.Success.Render(c.Status)
				}
				pid := c.PublicID
				if pid == "" {
					pid = fmt.Sprintf("(id:%d)", c.ID)
				}
				rows = append(rows, []string{
					pid,
					truncate(c.Name, 30),
					statusStr,
					c.Region,
					fmt.Sprintf("%d", c.NodeCount),
					fmt.Sprintf("%d", c.PodCount),
					fmt.Sprintf("%d", c.ServiceCount),
				})
			}
			ui.PrintTable(headers, rows)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json)")
	return cmd
}

func newClustersStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status [cluster-id]",
		Short: "Show detailed status of a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			clusterID := args[0]
			if err := util.SafePathSegment(clusterID); err != nil {
				return fmt.Errorf("invalid cluster ID: %w", err)
			}

			var data struct {
				Cluster struct {
					ID             uint    `json:"id"`
					Name           string  `json:"name"`
					Status         string  `json:"status"`
					Region         string  `json:"region"`
					KubeVersion    string  `json:"kube_version"`
					NodeCount      int     `json:"node_count"`
					PodCount       int     `json:"pod_count"`
					ServiceCount   int     `json:"service_count"`
					NamespaceCount int     `json:"namespace_count"`
					CPUUsage       float64 `json:"cpu_usage"`
					MemoryUsage    float64 `json:"memory_usage"`
					LastSeen       string  `json:"last_seen"`
					CreatedAt      string  `json:"created_at"`
				} `json:"cluster"`
			}
			resp, err := app.API.Do(ctx, "GET", fmt.Sprintf("clusters/%s", clusterID), nil, &data)
			if err != nil {
				return fmt.Errorf("get cluster: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("get cluster: %s", resp.Status)
			}

			c := data.Cluster

			fmt.Print(style.Bold.Render(fmt.Sprintf("Cluster: %s\n", c.Name)))
			fmt.Println(strings.Repeat("-", 40))
			fmt.Printf("ID:           %d\n", c.ID)
			fmt.Printf("Status:       ")
			if c.Status == "connected" {
				fmt.Println(style.Success.Render(c.Status))
			} else {
				fmt.Println(style.Error.Render(c.Status))
			}
			fmt.Printf("Region:       %s\n", c.Region)
			fmt.Printf("K8s Version:  %s\n", c.KubeVersion)
			fmt.Printf("Last Seen:    %s\n", c.LastSeen)
			fmt.Println()
			fmt.Println(style.Bold.Render("Resources:"))
			fmt.Printf("  Nodes:      %d\n", c.NodeCount)
			fmt.Printf("  Pods:       %d\n", c.PodCount)
			fmt.Printf("  Services:   %d\n", c.ServiceCount)
			fmt.Printf("  Namespaces: %d\n", c.NamespaceCount)
			fmt.Println()
			fmt.Println(style.Bold.Render("Usage:"))
			fmt.Printf("  CPU:        %.1f%%\n", c.CPUUsage)
			fmt.Printf("  Memory:     %.1f%%\n", c.MemoryUsage)

			// Zero Trust / Mesh onboarding status (always show so UI-enabled state is visible)
			clusterIDStr := strconv.FormatUint(uint64(c.ID), 10)
			fmt.Println()
			fmt.Println(style.Bold.Render("Zero Trust / Mesh (onboarding):"))
			ztResp, errZT := app.API.GetZeroTrustConfigs(ctx)
			if errZT != nil {
				fmt.Println(style.MutedStyle.Render("  Could not load — " + errZT.Error()))
			} else {
				var ztCfg *api.ZeroTrustConfig
				var ztStatus *api.ZeroTrustStatus
				for i := range ztResp.Configs {
					sid := ztResp.Configs[i].ClusterID.String()
					if sid == clusterIDStr || sid == c.Name {
						ztCfg = &ztResp.Configs[i]
						break
					}
				}
				for i := range ztResp.Statuses {
					sid := ztResp.Statuses[i].ClusterID.String()
					if sid == clusterIDStr || sid == c.Name {
						ztStatus = &ztResp.Statuses[i]
						break
					}
				}
				if ztCfg != nil {
					enabledStr := "disabled"
					if ztCfg.Enabled {
						enabledStr = style.Success.Render("enabled")
					} else {
						enabledStr = style.MutedStyle.Render("disabled")
					}
					fmt.Printf("  Enabled:    %s\n", enabledStr)
				}
				if ztStatus != nil {
					cniStr := style.Error.Render("not ready")
					if ztStatus.CNIReady {
						cniStr = style.Success.Render("ready")
					}
					fmt.Printf("  CNI:        %s (pods: %d/%d)\n", cniStr, ztStatus.CNIPodsReady, ztStatus.CNIPods)
					fmt.Printf("  Enrolled:   %d pods in %d namespaces\n", ztStatus.EnrolledPods, ztStatus.EnrolledNamespaces)
					if ztStatus.Version != "" {
						fmt.Printf("  Version:    %s\n", ztStatus.Version)
					}
				}
				if ztCfg == nil && ztStatus == nil {
					fmt.Println(style.MutedStyle.Render("  No Zero Trust config for this cluster. Enable in Dashboard → Mesh → Settings."))
				}
			}

			return nil
		},
	}
}

func newClustersMeshDebugCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mesh-debug",
		Short: "Debug why mesh topology shows no data (CNI enabled but no connections)",
		Long:  "Calls the topology API with a 48h window and prints diagnostics: event counts per cluster, registered clusters, and a checklist to fix \"UI shows mesh CNI enabled but no mesh data\".",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			resp, err := app.API.GetMeshTopology(ctx, "48h")
			if err != nil {
				return fmt.Errorf("topology API: %w", err)
			}

			fmt.Println(style.Bold.Render("Mesh topology (last 48h)"))
			fmt.Println(strings.Repeat("-", 50))
			fmt.Printf("Nodes: %d  Edges: %d\n", len(resp.Nodes), len(resp.Edges))

			if resp.Diagnostics != nil {
				d := resp.Diagnostics
				fmt.Println()
				fmt.Println(style.Bold.Render("Diagnostics (why topology can be empty)"))
				fmt.Printf("  Total events in store (in-memory + PG): %d\n", d.TotalEventsInStore)
				fmt.Printf("  Clusters with at least one event:      %v\n", d.ClustersWithData)
				if len(d.PerClusterEventCount) > 0 {
					fmt.Println("  Per-cluster event count (last 48h in PG):")
					for cid, n := range d.PerClusterEventCount {
						fmt.Printf("    %q: %d\n", cid, n)
					}
				}
				if len(d.RegisteredClusters) > 0 {
					fmt.Println("  Registered clusters (id, name, status):")
					for _, c := range d.RegisteredClusters {
						id, _ := c["id"]
						name, _ := c["name"]
						status, _ := c["status"]
						fmt.Printf("    %v  %v  %v\n", id, name, status)
					}
				}
				if d.WindowHint != "" {
					fmt.Println()
					fmt.Println(style.MutedStyle.Render("  " + d.WindowHint))
				}
				fmt.Println()
				fmt.Println(style.Bold.Render("Checklist (CNI enabled but no mesh data)"))
				fmt.Println("  1. cluster_id match: Events use cluster_id from the agent (name or id). If UI uses id 5 but agent sends \"frank\", filter by \"frank\" or ensure agent cluster_id matches.")
				fmt.Println("  2. Agent sends events: Agent must POST to backend /api/v1/agent/ztunnel/events (X-Cluster-ID header). Check agent logs for \"mesh-topology: sent N connection events\".")
				fmt.Println("  3. eBPF collector: If using eBPF, ensure MeshEnabled and mesh endpoint point to agent or backend. Agent proxies to backend.")
				fmt.Println("  4. Backend NATS: If using NATS mesh.connections, backend needs NATS_URL set; otherwise only HTTP ingestion is used.")
				fmt.Println("  5. Generate traffic: Topology is built from connection events. Create pod-to-pod traffic or use Mesh Settings → Inject test data.")
			} else {
				fmt.Println()
				fmt.Println(style.Success.Render("Topology has data; no diagnostics needed."))
			}
			return nil
		},
	}
}

// newClustersCheckCommand returns a command that gets kubeconfig for each cluster and runs kubectl get nodes and agent pod status, using the same Bubble Tea TUI as other commands.
func newClustersCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Ping each cluster and check agent status via kubectl",
		Long:  "For each available cluster, fetches a short-lived kubeconfig and runs `kubectl get nodes` to verify connectivity. Traffic goes: kubectl → backend → DERP → agent in cluster → K8s API. So the agent must be running and connected to DERP, and the backend must have DERP configured (DERP_RELAY_URL + token). If this fails with \"server is currently unable to handle the request\", the agent path is broken: use `prysm clusters check-local` to confirm the cluster is up with your local kubeconfig, then check agent pods and DERP. Requires kubectl in PATH.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			clusters, err := app.API.ListClusters(ctx)
			if err != nil {
				return fmt.Errorf("list clusters: %w", err)
			}
			if len(clusters) == 0 {
				fmt.Println("No clusters available.")
				return nil
			}
			if _, err := exec.LookPath("kubectl"); err != nil {
				return fmt.Errorf("kubectl not found in PATH: %w", err)
			}

			taskNames := make([]string, 0, len(clusters))
			nameToCluster := make(map[string]api.Cluster)
			for _, c := range clusters {
				name := c.Name
				if name == "" {
					name = fmt.Sprintf("id=%d", c.ID)
				}
				taskNames = append(taskNames, name)
				nameToCluster[name] = c
			}

			_, _, err = ui.RunBatchWithDetail("Checking clusters (kubeconfig + kubectl get nodes + agent)", taskNames, func(name string) (detail string, err error) {
				cluster := nameToCluster[name]
				return runOneClusterCheck(ctx, app, cluster)
			})
			return err
		},
	}
}

// fetchProxyError GETs the cluster proxy and returns the backend's Status message (e.g. "cluster proxy requires DERP; ...").
func fetchProxyError(ctx context.Context, app *App, clusterID int64) string {
	code, body, err := app.API.GetProxyResponse(ctx, fmt.Sprintf("%d", clusterID))
	if err != nil || code < 400 || len(body) == 0 {
		return ""
	}
	var st struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &st) != nil {
		return ""
	}
	return strings.TrimSpace(st.Message)
}

// extractKubectlError turns kubectl combined output (often with client-go log noise like memcache.go) into a short user-facing message.
func extractKubectlError(out []byte, fallback error) error {
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return fallback
	}
	// Client-go often logs: E... memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: ..."
	if idx := strings.Index(raw, `err="`); idx >= 0 {
		start := idx + len(`err="`)
		end := start
		for end < len(raw) {
			if raw[end] == '\\' && end+1 < len(raw) {
				end += 2
				continue
			}
			if raw[end] == '"' {
				break
			}
			end++
		}
		if end > start {
			msg := raw[start:end]
			if msg != "" {
				// Hint when backend message couldn't be fetched (cluster proxy uses DERP only)
				if strings.Contains(msg, "unable to handle the request") {
					msg += " — backend reaches the cluster via agent over DERP: check (1) agent is running in the cluster (kubectl get pods -n prysm-system), (2) backend has DERP_RELAY_URL and token, (3) agent is connected to DERP. Use 'prysm clusters check-local' to verify the cluster with your local kubeconfig."
				}
				return fmt.Errorf("%s", msg)
			}
		}
	}
	// Fallback: use first line that looks like an error message (no E0212... prefix)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip client-go log lines (E<digits> or I<digits> at start)
		if len(line) > 1 && (line[0] == 'E' || line[0] == 'I' || line[0] == 'W') && line[1] >= '0' && line[1] <= '9' {
			continue
		}
		return fmt.Errorf("%s", line)
	}
	return fallback
}

// perClusterCheckTimeout limits how long a single cluster check (kubectl + optional backend fetch) can take.
// Prevents hanging when the proxy retries DERP for a long time.
const perClusterCheckTimeout = 20 * time.Second

// runOneClusterCheck fetches kubeconfig for the cluster, runs kubectl get nodes and agent status, returns a one-line detail or error.
func runOneClusterCheck(ctx context.Context, app *App, cluster api.Cluster) (detail string, err error) {
	ctx, cancel := context.WithTimeout(ctx, perClusterCheckTimeout)
	defer cancel()

	tmpPath, err := writeClusterKubeconfigTemp(ctx, app, cluster)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpPath)

	kubectlCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", tmpPath, "get", "nodes", "-o", "name")
	out, runErr := kubectlCmd.CombinedOutput()
	if runErr != nil {
		err = extractKubectlError(out, runErr)
		// When kubectl shows generic 503 message, fetch the backend proxy response so we can show the real reason (e.g. DERP not configured).
		if err != nil && strings.Contains(err.Error(), "unable to handle the request") {
			if backendMsg := fetchProxyError(ctx, app, cluster.ID); backendMsg != "" {
				return "", fmt.Errorf("%s", backendMsg)
			}
		}
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	nodeCount := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nodeCount++
		}
	}
	agentLine := runAgentStatusLine(ctx, tmpPath)
	if agentLine != "" {
		detail = fmt.Sprintf("%d node(s), %s", nodeCount, agentLine)
	} else {
		detail = fmt.Sprintf("%d node(s)", nodeCount)
	}
	return detail, nil
}

// runAgentStatusLine returns a short one-line agent summary (e.g. "agent 2/2") or empty if none. Pass empty kubeconfigPath for default context.
func runAgentStatusLine(ctx context.Context, kubeconfigPath string) string {
	for _, selector := range []string{"app.kubernetes.io/name=agent", "app=prysm-agent"} {
		out, err := runKubectl(ctx, kubeconfigPath, "get", "pods", "-n", "prysm-system", "-l", selector, "-o", `jsonpath={range .items[*]}{.status.phase}{"\n"}{end}`)
		if err != nil {
			continue
		}
		phases := strings.Split(strings.TrimSpace(string(out)), "\n")
		var running int
		for _, p := range phases {
			if p == "Running" {
				running++
			}
		}
		total := len(phases)
		if total == 0 {
			continue
		}
		return fmt.Sprintf("agent %d/%d", running, total)
	}
	return ""
}

// newClustersInspectCommand connects to each cluster using Prysm-generated kubeconfig and runs kubectl to show what's running (nodes, prysm-system, kube-system).
func newClustersInspectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect",
		Short: "Connect to each cluster (Prysm kubeconfig) and show nodes and pods",
		Long:  "For each registered cluster, fetches a short-lived kubeconfig via the Prysm API and runs kubectl to show nodes, prysm-system pods, and kube-system pods. Useful for k3d or any cluster. Requires kubectl in PATH.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 90*time.Second)
			defer cancel()

			clusters, err := app.API.ListClusters(ctx)
			if err != nil {
				return fmt.Errorf("list clusters: %w", err)
			}
			if len(clusters) == 0 {
				fmt.Println("No clusters available.")
				return nil
			}
			if _, err := exec.LookPath("kubectl"); err != nil {
				return fmt.Errorf("kubectl not found in PATH: %w", err)
			}

			for _, cluster := range clusters {
				name := cluster.Name
				if name == "" {
					name = fmt.Sprintf("id=%d", cluster.ID)
				}
				fmt.Println()
				fmt.Println(style.Bold.Render("Cluster: " + name))
				fmt.Println(strings.Repeat("-", 50))

				tmpPath, err := writeClusterKubeconfigTemp(ctx, app, cluster)
				if err != nil {
					fmt.Printf("  kubeconfig: %s %v\n", style.Error.Render("FAIL"), err)
					continue
				}

				// Nodes
				fmt.Println(style.MutedStyle.Render("  Nodes:"))
				out, err := runKubectl(ctx, tmpPath, "get", "nodes", "-o", "wide")
				if err != nil {
					fmt.Printf("    %s\n", strings.TrimSpace(string(out)))
					if out == nil {
						fmt.Printf("    %v\n", err)
					}
				} else {
					for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
						fmt.Printf("    %s\n", line)
					}
				}

				// prysm-system
				fmt.Println(style.MutedStyle.Render("  prysm-system pods:"))
				out, err = runKubectl(ctx, tmpPath, "get", "pods", "-n", "prysm-system", "-o", "wide")
				if err != nil {
					fmt.Printf("    %s\n", strings.TrimSpace(string(out)))
					if len(out) == 0 {
						fmt.Printf("    %v\n", err)
					}
				} else {
					for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
						fmt.Printf("    %s\n", line)
					}
				}

				// kube-system (core)
				fmt.Println(style.MutedStyle.Render("  kube-system pods:"))
				out, err = runKubectl(ctx, tmpPath, "get", "pods", "-n", "kube-system", "-o", "wide")
				if err != nil {
					fmt.Printf("    %s\n", strings.TrimSpace(string(out)))
					if len(out) == 0 {
						fmt.Printf("    %v\n", err)
					}
				} else {
					for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
						fmt.Printf("    %s\n", line)
					}
				}
				_ = os.Remove(tmpPath)
			}
			fmt.Println()
			return nil
		},
	}
}

// writeClusterKubeconfigTemp fetches kubeconfig for the cluster via API, writes to a temp file, returns path. Caller must os.Remove(path).
func writeClusterKubeconfigTemp(ctx context.Context, app *App, cluster api.Cluster) (string, error) {
	resp, err := app.API.ConnectKubernetes(ctx, cluster.ID, "", "")
	if err != nil {
		return "", err
	}
	kubeconfig, err := decodeKubeconfig(resp.Kubeconfig)
	if err != nil {
		return "", err
	}
	if token := app.API.Token(); token != "" && strings.Contains(kubeconfig, "token: PLACEHOLDER") {
		kubeconfig = strings.Replace(kubeconfig, "token: PLACEHOLDER", "token: "+util.QuoteYAMLString(token), 1)
	}
	if strings.Contains(kubeconfig, "token: PLACEHOLDER") {
		return "", fmt.Errorf("no auth token (run prysm login)")
	}
	execPath := resolveExecPath()
	kubeconfig, err = replaceTokenWithExecCredential(kubeconfig, execPath)
	if err != nil {
		return "", err
	}
	tmpFile, err := os.CreateTemp("", "prysm-kube-*")
	if err != nil {
		return "", err
	}
	_, _ = tmpFile.WriteString(kubeconfig)
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	return tmpPath, nil
}

// runKubectl runs kubectl with the given kubeconfig and args; returns combined output and error. Pass empty kubeconfigPath to use default (current context).
func runKubectl(ctx context.Context, kubeconfigPath string, args ...string) ([]byte, error) {
	var all []string
	if kubeconfigPath != "" {
		all = append([]string{"--kubeconfig", kubeconfigPath}, args...)
	} else {
		all = args
	}
	cmd := exec.CommandContext(ctx, "kubectl", all...)
	return cmd.CombinedOutput()
}

// newClustersCheckLocalCommand checks cluster status using the current kubectl context (or all contexts with --all). No Prysm API.
func newClustersCheckLocalCommand() *cobra.Command {
	var allContexts bool
	cmd := &cobra.Command{
		Use:   "check-local",
		Short: "Check cluster status using current kubectl context(s)",
		Long:  "Runs kubectl get nodes and shows prysm-system agent pods using your local kubeconfig (current context). Use --all to check every context in your kubeconfig. No API or login required.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := exec.LookPath("kubectl"); err != nil {
				return fmt.Errorf("kubectl not found in PATH: %w", err)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			var contexts []string
			if allContexts {
				out, err := runKubectl(ctx, "", "config", "get-contexts", "-o", "name")
				if err != nil {
					return fmt.Errorf("list contexts: %w", err)
				}
				for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					name = strings.TrimSpace(name)
					if name != "" {
						contexts = append(contexts, name)
					}
				}
				if len(contexts) == 0 {
					fmt.Println("No contexts in kubeconfig.")
					return nil
				}
			} else {
				out, err := runKubectl(ctx, "", "config", "current-context")
				if err != nil {
					return fmt.Errorf("current context: %w", err)
				}
				cur := strings.TrimSpace(string(out))
				if cur == "" {
					fmt.Println("No current context set.")
					return nil
				}
				contexts = []string{cur}
			}

			for _, ctxName := range contexts {
				detail, err := runOneClusterCheckLocal(ctx, ctxName, allContexts)
				if allContexts {
					if err != nil {
						fmt.Printf("  %s %s %s\n", style.Error.Render("✗"), ctxName, err)
					} else {
						fmt.Printf("  %s %s  %s\n", style.Success.Render("✓"), ctxName, detail)
					}
				} else {
					fmt.Println(style.Bold.Render("Context: " + ctxName))
					if err != nil {
						fmt.Printf("  %s\n", err)
					} else {
						fmt.Printf("  %s\n", detail)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&allContexts, "all", false, "Check all contexts in kubeconfig")
	return cmd
}

// runOneClusterCheckLocal runs kubectl get nodes and agent status for the given context (no API kubeconfig).
func runOneClusterCheckLocal(ctx context.Context, contextName string, switchContext bool) (detail string, err error) {
	if switchContext {
		out, err := exec.CommandContext(ctx, "kubectl", "config", "use-context", contextName).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("switch context: %w", err)
		}
		_ = out
	}

	out, runErr := runKubectl(ctx, "", "get", "nodes", "-o", "name")
	if runErr != nil {
		return "", extractKubectlError(out, runErr)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	nodeCount := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nodeCount++
		}
	}
	agentLine := runAgentStatusLine(ctx, "")
	if agentLine != "" {
		detail = fmt.Sprintf("%d node(s), %s", nodeCount, agentLine)
	} else {
		detail = fmt.Sprintf("%d node(s)", nodeCount)
	}
	return detail, nil
}

func newClustersTokenCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "token [cluster-id]",
		Short: "Get or create agent token for a cluster",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			endpoint := "agent-tokens"
			if len(args) > 0 {
				if err := util.SafePathSegment(args[0]); err != nil {
					return fmt.Errorf("invalid cluster ID: %w", err)
				}
				endpoint = "agent-tokens?cluster_id=" + url.QueryEscape(args[0])
			}

			var data struct {
				Tokens []struct {
					ID        uint   `json:"id"`
					Name      string `json:"name"`
					Token     string `json:"token"`
					ClusterID *uint  `json:"cluster_id"`
					ExpiresAt string `json:"expires_at"`
					CreatedAt string `json:"created_at"`
				} `json:"tokens"`
			}
			resp, err := app.API.Do(ctx, "GET", endpoint, nil, &data)
			if err != nil {
				return fmt.Errorf("get tokens: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("get tokens: %s", resp.Status)
			}

			if len(data.Tokens) == 0 {
				fmt.Println("No agent tokens found.")
				return nil
			}

			for _, t := range data.Tokens {
				fmt.Printf("Name:       %s\n", t.Name)
				fmt.Printf("Token:      %s\n", t.Token)
				if t.ClusterID != nil {
					fmt.Printf("Cluster ID: %d\n", *t.ClusterID)
				}
				fmt.Printf("Expires:    %s\n", t.ExpiresAt)
				fmt.Println()
			}
			return nil
		},
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func newClustersExitCommand() *cobra.Command {
	exitCmd := &cobra.Command{
		Use:   "exit",
		Short: "Enable or disable a cluster as an exit router",
	}

	exitCmd.AddCommand(
		newClustersExitEnableCommand(),
		newClustersExitDisableCommand(),
	)

	return exitCmd
}

func newClustersExitEnableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <cluster>",
		Short: "Enable cluster as exit router (route client traffic through it)",
		Long:  "Enable the cluster as an exit node. Clients can then select it to route their traffic through this cluster.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, err := resolveClusterForExit(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if err := MustApp().API.EnableClusterExitRouter(cmd.Context(), cluster.ID); err != nil {
				return fmt.Errorf("enable exit router: %w", err)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("✓ Exit router enabled for cluster %s (%d)", cluster.Name, cluster.ID)))
			return nil
		},
	}
}

func newClustersExitDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <cluster>",
		Short: "Disable cluster as exit router",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, err := resolveClusterForExit(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if err := MustApp().API.DisableClusterExitRouter(cmd.Context(), cluster.ID); err != nil {
				return fmt.Errorf("disable exit router: %w", err)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("✓ Exit router disabled for cluster %s (%d)", cluster.Name, cluster.ID)))
			return nil
		},
	}
}

// newClustersRemoveCommand removes a cluster from the backend (disconnects first if needed).
func newClustersRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <cluster>",
		Short: "Remove a cluster from Prysm",
		Long:  "Removes a registered cluster by name or ID. If the cluster is connected, it is disconnected first. Use this when a cluster no longer exists (e.g. deleted k3d cluster).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			cluster, err := resolveClusterForExit(ctx, args[0])
			if err != nil {
				return err
			}

			app := MustApp()
			// Disconnect first so backend allows delete (backend requires status != "connected").
			_ = app.API.DisconnectCluster(ctx, cluster.ID)
			if err := app.API.DeleteCluster(ctx, cluster.ID); err != nil {
				return fmt.Errorf("delete cluster: %w", err)
			}
			fmt.Println(style.Success.Render(fmt.Sprintf("Removed cluster %q (id=%d)", cluster.Name, cluster.ID)))
			return nil
		},
	}
}

func resolveClusterForExit(ctx context.Context, ref string) (*api.Cluster, error) {
	clusters, err := MustApp().API.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	if len(clusters) == 0 {
		return nil, errors.New("no clusters available")
	}

	trimmed := strings.TrimSpace(ref)
	for _, c := range clusters {
		if strings.EqualFold(c.Name, trimmed) {
			return &c, nil
		}
	}
	if id, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		for _, c := range clusters {
			if c.ID == id {
				return &c, nil
			}
		}
	}
	return nil, fmt.Errorf("cluster %q not found", ref)
}

func newClustersReconcileCommand() *cobra.Command {
	var ebpfImage, logImage, cniImage, fluentbitImage string

	cmd := &cobra.Command{
		Use:   "reconcile <cluster>",
		Short: "Push component image versions for agent reconciliation",
		Long:  "Set desired component image versions for a cluster. The agent will pick up these overrides on its next reconciliation cycle.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			cluster, err := resolveClusterForExit(ctx, args[0])
			if err != nil {
				return err
			}

			req := api.ComponentConfigUpdate{
				EBPFImage:      ebpfImage,
				LogImage:       logImage,
				CNIImage:       cniImage,
				FluentBitImage: fluentbitImage,
			}

			if err := MustApp().API.UpdateComponentConfig(ctx, cluster.ID, req); err != nil {
				return fmt.Errorf("update component config: %w", err)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("Component config updated for cluster %s (%d)", cluster.Name, cluster.ID)))
			if ebpfImage != "" {
				fmt.Printf("  ebpf-image:      %s\n", ebpfImage)
			}
			if logImage != "" {
				fmt.Printf("  log-image:       %s\n", logImage)
			}
			if cniImage != "" {
				fmt.Printf("  cni-image:       %s\n", cniImage)
			}
			if fluentbitImage != "" {
				fmt.Printf("  fluentbit-image: %s\n", fluentbitImage)
			}
			fmt.Println("The agent will apply these on its next reconciliation cycle.")
			return nil
		},
	}

	cmd.Flags().StringVar(&ebpfImage, "ebpf-image", "", "eBPF collector image (e.g. ghcr.io/prysmsh/ebpf-collector:v1.2)")
	cmd.Flags().StringVar(&logImage, "log-image", "", "Log collector image (e.g. fluent/fluent-bit:4.2)")
	cmd.Flags().StringVar(&cniImage, "cni-image", "", "CNI plugin image (e.g. ghcr.io/prysmsh/cni:latest)")
	cmd.Flags().StringVar(&fluentbitImage, "fluentbit-image", "", "Fluent Bit sidecar image (e.g. fluent/fluent-bit:4.2)")

	return cmd
}
