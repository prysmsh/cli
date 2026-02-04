package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/config"
	"github.com/warp-run/prysm-cli/internal/session"
)

// execCredentialResponse mirrors the Kubernetes ExecCredential type
// (client.authentication.k8s.io/v1) so we can emit it without importing
// k8s.io/client-go.
type execCredentialResponse struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Status     execCredentialStatusResp `json:"status"`
}

type execCredentialStatusResp struct {
	Token               string `json:"token"`
	ExpirationTimestamp string `json:"expirationTimestamp,omitempty"`
}

func newCredentialCommand() *cobra.Command {
	credCmd := &cobra.Command{
		Use:   "credential",
		Short: "Emit credentials for external tools (kubectl, etc.)",
	}

	credCmd.AddCommand(newCredentialK8sCommand())
	return credCmd
}

func newCredentialK8sCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "k8s",
		Short: "Print a Kubernetes ExecCredential for kubectl authentication",
		// Bypass the normal PersistentPreRunE — this command must be fast
		// and only reads the local session file (no API client needed).
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil },
		RunE: func(_ *cobra.Command, _ []string) error {
			return runCredentialK8s()
		},
	}
}

func runCredentialK8s() error {
	homeDir, err := config.DefaultHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "prysm credential k8s: cannot determine config directory: %v\n", err)
		os.Exit(1)
	}

	store := session.NewStore(filepath.Join(homeDir, "session.json"))
	sess, err := store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "prysm credential k8s: failed to load session: %v\n", err)
		os.Exit(1)
	}
	if sess == nil {
		fmt.Fprintln(os.Stderr, "prysm credential k8s: no session found — run `prysm login` first")
		os.Exit(1)
	}

	if sess.IsExpired(0) {
		fmt.Fprintln(os.Stderr, "prysm credential k8s: session expired — run `prysm login` to refresh")
		os.Exit(1)
	}

	resp := execCredentialResponse{
		APIVersion: "client.authentication.k8s.io/v1",
		Kind:       "ExecCredential",
		Status: execCredentialStatusResp{
			Token: sess.Token,
		},
	}

	if exp := sess.ExpiresAt(); !exp.IsZero() {
		resp.Status.ExpirationTimestamp = exp.UTC().Format(time.RFC3339)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(resp); err != nil {
		fmt.Fprintf(os.Stderr, "prysm credential k8s: failed to encode credential: %v\n", err)
		os.Exit(1)
	}

	return nil
}
