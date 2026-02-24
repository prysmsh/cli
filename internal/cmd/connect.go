package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/output"
	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
	"github.com/prysmsh/cli/internal/util"
)

func newConnectCommand() *cobra.Command {
	connectCmd := &cobra.Command{
		Use:   "connect",
		Short: "Establish access to managed infrastructure resources",
	}

	meshAlias := newMeshConnectCommand()
	meshAlias.Use = "mesh"
	meshAlias.Short = "Join the DERP mesh network"

	connectCmd.AddCommand(
		newConnectKubernetesCommand(),
		newConnectSSHCommand(),
		newConnectDevicesCommand(),
		meshAlias,
	)

	return connectCmd
}

func newSSHCommand() *cobra.Command {
	cmd := newConnectSSHCommand()
	cmd.Use = "ssh <target> [-- <remote command>]"
	cmd.Short = "Open an SSH session with policy checks and audit reason"
	cmd.Long = "Open an SSH session via Prysm policy evaluation. The command records reason metadata and enforces access checks before launching ssh."
	cmd.AddCommand(newSSHOnboardCommand())
	return cmd
}

func newSSHOnboardCommand() *cobra.Command {
	var withCollector bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "onboard <target>",
		Short: "Onboard an SSH-accessible host",
		Long:  "SSH to the target host and run Docker-host onboarding remotely (runs `prysm onboard docker` on the target).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := strings.TrimSpace(args[0])
			if target == "" {
				return fmt.Errorf("target host is required")
			}

			localToken := strings.TrimSpace(currentSessionToken())
			remoteArgs := sshOnboardRemoteArgs("prysm", withCollector, localToken)

			sshArgs := []string{"-t", target}
			sshArgs = append(sshArgs, remoteArgs...)

			if dryRun {
				// Keep dry-run output secret-free even when token forwarding is enabled.
				displayArgs := []string{"-t", target}
				displayArgs = append(displayArgs, sshOnboardRemoteArgs("prysm", withCollector, "redacted")...)
				displayArgs = redactSSHOnboardToken(displayArgs)
				fmt.Printf("ssh %s\n", strings.Join(displayArgs, " "))
				return nil
			}

			stderrOutput, err := runSSHCommand(cmd.Context(), sshArgs)
			if err != nil {
				if isHostKeyVerificationFailure(stderrOutput) {
					host := sshTargetHost(target)
					confirm, promptErr := util.PromptConfirm(
						fmt.Sprintf("Host key for %s is unknown. Add it to ~/.ssh/known_hosts and continue?", host),
						true,
					)
					if promptErr != nil {
						return fmt.Errorf("confirm host key trust: %w", promptErr)
					}
					if !confirm {
						return fmt.Errorf("remote onboarding aborted: host key for %s not trusted", host)
					}
					if addErr := addHostKeyToKnownHosts(cmd.Context(), target); addErr != nil {
						return fmt.Errorf("add host key for %s: %w", host, addErr)
					}

					stderrOutput, err = runSSHCommand(cmd.Context(), sshArgs)
					if err == nil {
						return nil
					}
				}
				if isRemotePrysmNotFound(stderrOutput) {
					confirm, promptErr := util.PromptConfirm(
						"Remote `prysm` CLI not found. Install it to ~/.local/bin/prysm and continue?",
						true,
					)
					if promptErr != nil {
						return fmt.Errorf("confirm remote CLI install: %w", promptErr)
					}
					if !confirm {
						return fmt.Errorf("remote onboarding aborted: remote `prysm` CLI is missing")
					}
					if installErr := installRemotePrysmBinary(cmd.Context(), target); installErr != nil {
						return fmt.Errorf("install remote `prysm` CLI: %w", installErr)
					}

					retryRemoteArgs := sshOnboardRemoteArgs("~/.local/bin/prysm", withCollector, localToken)
					retrySSHArgs := []string{"-t", target}
					retrySSHArgs = append(retrySSHArgs, retryRemoteArgs...)
					_, err = runSSHCommand(cmd.Context(), retrySSHArgs)
					if err == nil {
						return nil
					}
				}
				return fmt.Errorf("remote onboarding failed: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&withCollector, "collector", false, "include eBPF collector in the generated compose stack")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the remote ssh onboarding command without executing it")
	return cmd
}

func sshOnboardRemoteArgs(remoteBinary string, withCollector bool, token string) []string {
	remoteArgs := []string{remoteBinary}
	if strings.TrimSpace(token) != "" {
		remoteArgs = append(remoteArgs, "--token", token)
	}
	remoteArgs = append(remoteArgs, "onboard", "docker")
	if withCollector {
		remoteArgs = append(remoteArgs, "--collector")
	}
	return remoteArgs
}

func redactSSHOnboardToken(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i < len(out)-1; i++ {
		if out[i] == "--token" {
			out[i+1] = "<redacted>"
			i++
		}
	}
	return out
}

func currentSessionToken() string {
	if app == nil || app.API == nil {
		return ""
	}
	return app.API.Token()
}

func runSSHCommand(ctx context.Context, sshArgs []string) (string, error) {
	var combined bytes.Buffer
	sshCmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = io.MultiWriter(os.Stdout, &combined)
	sshCmd.Stderr = io.MultiWriter(os.Stderr, &combined)
	err := sshCmd.Run()
	return combined.String(), err
}

func isHostKeyVerificationFailure(stderrOutput string) bool {
	return strings.Contains(stderrOutput, "Host key verification failed.")
}

func isRemotePrysmNotFound(stderrOutput string) bool {
	return strings.Contains(stderrOutput, "command not found: prysm") ||
		strings.Contains(stderrOutput, "prysm: command not found")
}

func sshTargetHost(target string) string {
	hostPart := strings.TrimSpace(target)
	if idx := strings.LastIndex(hostPart, "@"); idx >= 0 {
		hostPart = hostPart[idx+1:]
	}
	if hostPart == "" {
		return target
	}

	if strings.HasPrefix(hostPart, "[") {
		end := strings.Index(hostPart, "]")
		if end > 1 {
			return hostPart[1:end]
		}
	}

	if host, _, err := net.SplitHostPort(hostPart); err == nil && host != "" {
		return host
	}
	return hostPart
}

func installRemotePrysmBinary(ctx context.Context, target string) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve local executable path: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(execPath)
	if err == nil && strings.TrimSpace(resolvedPath) != "" {
		execPath = resolvedPath
	}

	bin, err := os.Open(execPath)
	if err != nil {
		return fmt.Errorf("open local executable %q: %w", execPath, err)
	}
	defer bin.Close()

	remoteCmd := exec.CommandContext(
		ctx,
		"ssh",
		target,
		"mkdir -p ~/.local/bin && cat > ~/.local/bin/prysm && chmod 755 ~/.local/bin/prysm",
	)
	remoteCmd.Stdin = bin
	remoteCmd.Stdout = os.Stdout
	remoteCmd.Stderr = os.Stderr
	if err := remoteCmd.Run(); err != nil {
		return err
	}
	return nil
}

func addHostKeyToKnownHosts(ctx context.Context, target string) error {
	host := sshTargetHost(target)
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("unable to determine SSH host from target %q", target)
	}

	scanCmd := exec.CommandContext(ctx, "ssh-keyscan", "-H", host)
	keyOutput, err := scanCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh-keyscan: %w: %s", err, strings.TrimSpace(string(keyOutput)))
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", sshDir, err)
	}

	knownHostsPath := filepath.Join(sshDir, "known_hosts")
	f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", knownHostsPath, err)
	}
	defer f.Close()

	if _, err := f.Write(keyOutput); err != nil {
		return fmt.Errorf("write %s: %w", knownHostsPath, err)
	}
	return nil
}

func newConnectKubernetesCommand() *cobra.Command {
	var (
		clusterRef     string
		namespace      string
		reason         string
		outputPath     string
		execCredential bool
	)

	cmd := &cobra.Command{
		Use:   "kube [--cluster name-or-id]",
		Short: "Issue a temporary kubeconfig for a managed Kubernetes cluster",
		Long:  "Get a short-lived kubeconfig to access a cluster via kubectl. Use --cluster to pick by name or ID, or omit it for an interactive list. Example: prysm connect kube --cluster frank -o kubeconfig.yaml && kubectl --kubeconfig=kubeconfig.yaml get nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Second)
			defer cancel()

			clusters, err := app.API.ListClusters(ctx)
			if err != nil {
				return err
			}
			if len(clusters) == 0 {
				return errors.New("no Kubernetes clusters available for your organization")
			}

			ref := clusterRef
			if ref == "" {
				fmt.Fprintln(os.Stderr, style.Bold.Render("Clusters (use name or ID with --cluster next time):"))
				for _, c := range clusters {
					status := output.StatusColor(c.Status)
					fmt.Fprintf(os.Stderr, "  %d\t%s\t%s\n", c.ID, c.Name, status)
				}
				fmt.Fprintln(os.Stderr)
				var promptErr error
				ref, promptErr = util.PromptInput("Cluster (name or ID)")
				if promptErr != nil {
					return fmt.Errorf("cluster selection: %w", promptErr)
				}
				ref = strings.TrimSpace(ref)
				if ref == "" {
					return errors.New("cluster reference is required")
				}
			}

			cluster, err := findCluster(clusters, ref)
			if err != nil {
				var b strings.Builder
				fmt.Fprintf(&b, "%v\nAvailable clusters:\n", err)
				for _, c := range clusters {
					status := output.StatusColor(c.Status)
					fmt.Fprintf(&b, "  %d  %s  %s\n", c.ID, c.Name, status)
				}
				return errors.New(b.String())
			}

			var resp *api.ClusterConnectResponse
			if err := ui.WithSpinner("Connecting to cluster...", func() error {
				var connErr error
				resp, connErr = app.API.ConnectKubernetes(ctx, cluster.ID, namespace, reason)
				return connErr
			}); err != nil {
				return err
			}

			kubeconfig, err := decodeKubeconfig(resp.Kubeconfig)
			if err != nil {
				return err
			}

			// If backend left PLACEHOLDER (e.g. no session token in context), inject current token so kubectl can auth to proxy
			if token := app.API.Token(); token != "" && strings.Contains(kubeconfig, "token: PLACEHOLDER") {
				kubeconfig = strings.Replace(kubeconfig, "token: PLACEHOLDER", "token: "+util.QuoteYAMLString(token), 1)
			}
			if strings.Contains(kubeconfig, "token: PLACEHOLDER") {
				return fmt.Errorf("kubeconfig has no auth token; run `prysm login` then retry `connect kube`")
			}

			if execCredential {
				execPath := resolveExecPath()
				transformed, err := replaceTokenWithExecCredential(kubeconfig, execPath)
				if err != nil {
					return fmt.Errorf("apply --exec-credential transform: %w", err)
				}
				kubeconfig = transformed
			}

			if outputPath != "" {
				dest := outputPath
				if !filepath.IsAbs(dest) {
					dest, _ = filepath.Abs(dest)
				}
				if err := os.WriteFile(dest, []byte(kubeconfig), 0o600); err != nil {
					return fmt.Errorf("write kubeconfig: %w", err)
				}
				fmt.Println(style.Success.Render(fmt.Sprintf("📁 Kubeconfig written to %s", dest)))
			} else {
				fmt.Println("----- kubeconfig (apply with kubectl) -----")
				fmt.Print(kubeconfig)
				if !strings.HasSuffix(kubeconfig, "\n") {
					fmt.Println()
				}
				fmt.Println("----- end kubeconfig -----")
				fmt.Println(style.MutedStyle.Render("Tip: rerun with --output <path> to save this configuration."))
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("✅ Kubernetes session established for %s (session: %s)", resp.Cluster.Name, resp.Session.SessionID)))
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterRef, "cluster", "", "cluster name or ID")
	cmd.Flags().StringVar(&namespace, "namespace", "", "override namespace policy")
	cmd.Flags().StringVar(&reason, "reason", "", "access justification for audit logs")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "write kubeconfig to file")
	cmd.Flags().BoolVar(&execCredential, "exec-credential", true, "use kubectl exec credential plugin instead of embedding a token (disable with --exec-credential=false)")

	return cmd
}

func newConnectDevicesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "devices",
		Short: "Show devices currently connected through the Prysm mesh",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			nodes, err := app.API.ListMeshNodes(ctx)
			if err != nil {
				return err
			}
			if len(nodes) == 0 {
				fmt.Println(style.Warning.Render("No mesh peers registered for your organization."))
				return nil
			}

			renderMeshNodes(nodes)
			return nil
		},
	}
}

func newConnectSSHCommand() *cobra.Command {
	var (
		reason    string
		requestID string
		port      int
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:   "ssh <target> [-- <remote command>]",
		Short: "Open policy-checked SSH access to a host or registered target",
		Long:  "Request SSH access through Prysm policy checks and open an interactive SSH session. The --reason value is required for audit trails.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			target := strings.TrimSpace(args[0])
			remoteCommand := []string{}
			if len(args) > 1 {
				remoteCommand = args[1:]
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Second)
			defer cancel()

			resp, err := app.API.ConnectSSH(ctx, api.SSHConnectRequest{
				Target:    target,
				Reason:    strings.TrimSpace(reason),
				RequestID: strings.TrimSpace(requestID),
				Port:      port,
				Command:   remoteCommand,
				DryRun:    dryRun,
			})
			if err != nil {
				return err
			}

			sshArgs, resolvedTarget, err := buildSSHArgs(target, port, resp, remoteCommand)
			if err != nil {
				return err
			}

			if sid := strings.TrimSpace(resp.Session.SessionID); sid != "" {
				fmt.Println(style.MutedStyle.Render(fmt.Sprintf("SSH session: %s", sid)))
			}

			if dryRun {
				fmt.Println(style.Success.Render("Policy checks passed (dry-run)."))
				fmt.Printf("ssh %s\n", shellJoin(sshArgs))
				fmt.Println(style.MutedStyle.Render("Use --dry-run=false (default) to execute the SSH command."))
				return nil
			}

			if _, err := exec.LookPath("ssh"); err != nil {
				return fmt.Errorf("ssh client not found in PATH")
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("Connecting to %s...", resolvedTarget)))
			sshCmd := exec.Command("ssh", sshArgs...)
			sshCmd.Stdin = os.Stdin
			sshCmd.Stdout = os.Stdout
			sshCmd.Stderr = os.Stderr
			return sshCmd.Run()
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "required justification for audit and policy evaluation")
	cmd.Flags().StringVar(&requestID, "request-id", "", "link this SSH session to an approved access request ID")
	cmd.Flags().IntVar(&port, "port", 0, "override SSH port")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "evaluate policy and print the ssh command without executing it")
	_ = cmd.MarkFlagRequired("reason")

	return cmd
}

func buildSSHArgs(target string, requestedPort int, resp *api.SSHConnectResponse, remoteCommand []string) ([]string, string, error) {
	if resp == nil {
		return nil, "", fmt.Errorf("missing ssh response")
	}

	resolvedTarget := strings.TrimSpace(resp.Connection.Target)
	host := strings.TrimSpace(resp.Connection.Host)
	user := strings.TrimSpace(resp.Connection.User)

	if resolvedTarget == "" && host != "" {
		if user != "" {
			resolvedTarget = user + "@" + host
		} else {
			resolvedTarget = host
		}
	}
	if resolvedTarget == "" {
		resolvedTarget = strings.TrimSpace(target)
	}
	if resolvedTarget == "" {
		return nil, "", fmt.Errorf("unable to resolve SSH target")
	}

	finalPort := resp.Connection.Port
	if finalPort == 0 {
		finalPort = requestedPort
	}

	args := make([]string, 0, 16+len(remoteCommand))
	if finalPort > 0 {
		args = append(args, "-p", strconv.Itoa(finalPort))
	}
	if idFile := strings.TrimSpace(resp.Connection.IdentityFile); idFile != "" {
		args = append(args, "-i", idFile)
	}
	if pc := strings.TrimSpace(resp.Connection.ProxyCommand); pc != "" {
		args = append(args, "-o", "ProxyCommand="+pc)
	}
	for _, opt := range resp.Connection.Options {
		opt = strings.TrimSpace(opt)
		if opt == "" {
			continue
		}
		args = append(args, "-o", opt)
	}
	if len(resp.Connection.SSHArgs) > 0 {
		args = append(args, resp.Connection.SSHArgs...)
	}

	args = append(args, resolvedTarget)
	args = append(args, remoteCommand...)
	return args, resolvedTarget, nil
}

func shellJoin(args []string) string {
	if len(args) == 0 {
		return ""
	}

	quoted := make([]string, 0, len(args))
	for _, a := range args {
		if a == "" {
			quoted = append(quoted, "''")
			continue
		}
		if strings.ContainsAny(a, " \t\n'\"\\") {
			quoted = append(quoted, strconv.Quote(a))
			continue
		}
		quoted = append(quoted, a)
	}
	return strings.Join(quoted, " ")
}

func findCluster(clusters []api.Cluster, ref string) (*api.Cluster, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return nil, errors.New("cluster reference is empty")
	}

	for _, cluster := range clusters {
		if strings.EqualFold(cluster.Name, trimmed) {
			return &cluster, nil
		}
	}

	if id, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		for _, cluster := range clusters {
			if cluster.ID == id {
				return &cluster, nil
			}
		}
	}

	return nil, fmt.Errorf("cluster %q not found", ref)
}

func decodeKubeconfig(material api.KubeconfigMaterial) (string, error) {
	value := material.Value
	switch strings.ToLower(material.Encoding) {
	case "base64", "b64":
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return "", fmt.Errorf("decode kubeconfig: %w", err)
		}
		return string(decoded), nil
	default:
		return value, nil
	}
}

// quoteYAMLString escapes s for use as a double-quoted YAML string value.
// Deprecated: use util.QuoteYAMLString instead.
func quoteYAMLString(s string) string {
	return util.QuoteYAMLString(s)
}

// resolveExecPath returns the absolute path to the current prysm binary.
// Falls back to "prysm" (PATH lookup) if resolution fails.
func resolveExecPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "prysm"
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "prysm"
	}
	return exe
}

// replaceTokenWithExecCredential parses a kubeconfig YAML string and replaces
// every user[].user.token field with a user[].user.exec block that invokes
// `prysm credential k8s`.
func replaceTokenWithExecCredential(raw string, execPath string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return "", fmt.Errorf("parse kubeconfig YAML: %w", err)
	}

	// The document node wraps the root mapping.
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return "", fmt.Errorf("unexpected kubeconfig YAML structure")
	}
	root := doc.Content[0]

	usersNode := yamlMappingValue(root, "users")
	if usersNode == nil || usersNode.Kind != yaml.SequenceNode {
		return "", fmt.Errorf("kubeconfig has no users sequence")
	}

	for _, entry := range usersNode.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		userNode := yamlMappingValue(entry, "user")
		if userNode == nil || userNode.Kind != yaml.MappingNode {
			continue
		}

		// Remove "token" key/value pair from the user mapping.
		yamlMappingDelete(userNode, "token")

		// Build the exec block.
		execNode := &yaml.Node{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "apiVersion"},
				{Kind: yaml.ScalarNode, Value: "client.authentication.k8s.io/v1"},
				{Kind: yaml.ScalarNode, Value: "command"},
				{Kind: yaml.ScalarNode, Value: execPath},
				{Kind: yaml.ScalarNode, Value: "args"},
				{
					Kind: yaml.SequenceNode,
					Tag:  "!!seq",
					Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Value: "credential"},
						{Kind: yaml.ScalarNode, Value: "k8s"},
					},
				},
				{Kind: yaml.ScalarNode, Value: "interactiveMode"},
				{Kind: yaml.ScalarNode, Value: "Never"},
			},
		}

		// Remove any existing exec block before adding the new one.
		yamlMappingDelete(userNode, "exec")

		userNode.Content = append(userNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "exec"},
			execNode,
		)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("serialize kubeconfig YAML: %w", err)
	}
	return string(out), nil
}

// yamlMappingValue returns the value node for the given key in a mapping node.
func yamlMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// yamlMappingDelete removes a key/value pair from a mapping node.
func yamlMappingDelete(mapping *yaml.Node, key string) {
	if mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}
