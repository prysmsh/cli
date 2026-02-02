package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/util"
)

func newConnectCommand() *cobra.Command {
	connectCmd := &cobra.Command{
		Use:   "connect",
		Short: "Establish access to managed infrastructure resources",
	}

	connectCmd.AddCommand(
		newConnectKubernetesCommand(),
		newConnectDevicesCommand(),
	)

	return connectCmd
}

func newConnectKubernetesCommand() *cobra.Command {
	var (
		clusterRef string
		namespace  string
		reason     string
		outputPath string
	)

	cmd := &cobra.Command{
		Use:   "k8s",
		Short: "Issue a temporary kubeconfig for a managed Kubernetes cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if clusterRef == "" {
				return errors.New("cluster reference is required (--cluster)")
			}

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

			cluster, err := findCluster(clusters, clusterRef)
			if err != nil {
				var b strings.Builder
				fmt.Fprintf(&b, "%v\nAvailable clusters:\n", err)
				for _, c := range clusters {
					status := color.HiGreenString(c.Status)
					if strings.ToLower(c.Status) != "connected" {
						status = color.HiRedString(c.Status)
					}
					fmt.Fprintf(&b, "  - %d\t%s\t%s\n", c.ID, c.Name, status)
				}
				return errors.New(b.String())
			}

			resp, err := app.API.ConnectKubernetes(ctx, cluster.ID, namespace, reason)
			if err != nil {
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
				return fmt.Errorf("kubeconfig has no auth token; run `prysm login` then retry `connect k8s`")
			}

			if outputPath != "" {
				dest := outputPath
				if !filepath.IsAbs(dest) {
					dest, _ = filepath.Abs(dest)
				}
				if err := os.WriteFile(dest, []byte(kubeconfig), 0o600); err != nil {
					return fmt.Errorf("write kubeconfig: %w", err)
				}
				color.New(color.FgGreen).Printf("📁 Kubeconfig written to %s\n", dest)
			} else {
				fmt.Println("----- kubeconfig (apply with kubectl) -----")
				fmt.Print(kubeconfig)
				if !strings.HasSuffix(kubeconfig, "\n") {
					fmt.Println()
				}
				fmt.Println("----- end kubeconfig -----")
				color.New(color.FgHiBlack).Println("Tip: rerun with --output <path> to save this configuration.")
			}

			color.New(color.FgGreen).Printf("✅ Kubernetes session established for %s (session: %s)\n", resp.Cluster.Name, resp.Session.SessionID)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterRef, "cluster", "", "cluster name or ID")
	cmd.Flags().StringVar(&namespace, "namespace", "", "override namespace policy")
	cmd.Flags().StringVar(&reason, "reason", "", "access justification for audit logs")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "write kubeconfig to file")

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
				color.New(color.FgYellow).Println("No mesh peers registered for your organization.")
				return nil
			}

			renderMeshNodes(nodes)
			return nil
		},
	}
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
