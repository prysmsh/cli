package cmd

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/daemon"
)


func newMeshEnrollCommand() *cobra.Command {
	var (
		deviceID string
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Enroll this device for WireGuard access",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()

			if deviceID == "" {
				if host, err := os.Hostname(); err == nil {
					deviceID = host
				}
			}

			deviceID = strings.TrimSpace(deviceID)
			if deviceID == "" {
				return errors.New("device identifier is required")
			}

			keyDir := filepath.Join(app.Config.HomeDir, "wireguard")
			if err := os.MkdirAll(keyDir, 0o700); err != nil {
				return fmt.Errorf("prepare wireguard directory: %w", err)
			}

			keyPath := filepath.Join(keyDir, fmt.Sprintf("%s.key", sanitizeFileSegment(deviceID)))
			if !force {
				if _, err := os.Stat(keyPath); err == nil {
					return fmt.Errorf("private key already exists at %s (use --force to overwrite)", keyPath)
				}
			}

			privateKey, err := wgtypes.GeneratePrivateKey()
			if err != nil {
				return fmt.Errorf("generate private key: %w", err)
			}

			if err := os.WriteFile(keyPath, []byte(privateKey.String()), 0o600); err != nil {
				return fmt.Errorf("write private key: %w", err)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			req := api.RegisterWireguardDeviceRequest{
				DeviceID:  deviceID,
				PublicKey: privateKey.PublicKey().String(),
				Capabilities: map[string]interface{}{
					"platform": runtime.GOOS,
					"arch":     runtime.GOARCH,
					"version":  version,
				},
				Metadata: map[string]interface{}{
					"hostname": deviceID,
					"created":  time.Now().UTC().Format(time.RFC3339),
				},
			}

			resp, err := app.API.RegisterWireguardDevice(ctx, req)
			if err != nil {
				return err
			}

			renderWireguardEnrollment(resp, keyPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&deviceID, "device-id", "", "custom device identifier (default: system hostname)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing private key material if present")
	return cmd
}

func newMeshConfigCommand() *cobra.Command {
	var (
		deviceID   string
		writePath  string
		outputJSON bool
	)

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Fetch and render WireGuard configuration for an enrolled device",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()

			if deviceID == "" {
				if host, err := os.Hostname(); err == nil {
					deviceID = host
				}
			}

			deviceID = strings.TrimSpace(deviceID)
			if deviceID == "" {
				return errors.New("device identifier is required")
			}

			keyPath := filepath.Join(app.Config.HomeDir, "wireguard", fmt.Sprintf("%s.key", sanitizeFileSegment(deviceID)))
			keyBytes, err := os.ReadFile(keyPath)
			if err != nil {
				return fmt.Errorf("read private key: %w (run `prysm mesh enroll` first)", err)
			}
			privateKey := strings.TrimSpace(string(keyBytes))
			if privateKey == "" {
				return fmt.Errorf("private key file %s is empty", keyPath)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			resp, err := app.API.GetWireguardConfig(ctx, deviceID)
			if err != nil {
				return err
			}

			if outputJSON {
				data, err := json.MarshalIndent(resp, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			configStr := buildWireguardConfig(privateKey, resp)

			if writePath != "" {
				dest := writePath
				if !filepath.IsAbs(dest) {
					dest = filepath.Join(app.Config.HomeDir, dest)
				}
				if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
					return fmt.Errorf("prepare config directory: %w", err)
				}
				if err := os.WriteFile(dest, []byte(configStr), 0o600); err != nil {
					return fmt.Errorf("write config: %w", err)
				}
				color.New(color.FgGreen).Printf("✅ WireGuard config written to %s\n", dest)
			} else {
				fmt.Println(configStr)
			}

			if len(resp.Warnings) > 0 {
				color.New(color.FgYellow).Println("\nWarnings:")
				for _, w := range resp.Warnings {
					color.New(color.FgYellow).Printf("  • %s\n", w)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&deviceID, "device-id", "", "device identifier (default: system hostname)")
	cmd.Flags().StringVar(&writePath, "write-config", "", "write WireGuard config to this path (default: stdout)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "output raw JSON response instead of wg-quick config")
	return cmd
}

func renderWireguardEnrollment(resp *api.WireguardConfigResponse, keyPath string) {
	color.New(color.FgGreen).Println("✅ Device enrolled successfully")
	color.New(color.FgHiBlack).Printf("Private key stored at %s\n", keyPath)

	color.New(color.FgCyan).Printf("\nAssigned address: %s (%s)\n", resp.Config.Address, resp.Config.CIDR)
	if len(resp.Config.DNS) > 0 {
		color.New(color.FgCyan).Printf("DNS servers: %s\n", strings.Join(resp.Config.DNS, ", "))
	}
	color.New(color.FgCyan).Printf("Peers discovered: %d\n", len(resp.Peers))

	if len(resp.Warnings) > 0 {
		color.New(color.FgYellow).Println("\nWarnings:")
		for _, w := range resp.Warnings {
			color.New(color.FgYellow).Printf("  • %s\n", w)
		}
	}

	color.New(color.FgHiBlack).Println("\nNext steps:")
	color.New(color.FgHiBlack).Println("  Automatic (recommended):")
	color.New(color.FgHiBlack).Println("    1. Start the daemon: sudo prysm mesh meshd")
	color.New(color.FgHiBlack).Printf("    2. Start the mesh tunnel: prysm mesh up --device-id \"%s\"\n", resp.Device.DeviceID)
	color.New(color.FgHiBlack).Println("\n  Manual (for custom setups):")
	color.New(color.FgHiBlack).Printf("    1. Generate config: prysm mesh config --device-id \"%s\" --write-config wg0.conf\n", resp.Device.DeviceID)
	color.New(color.FgHiBlack).Println("    2. Import wg0.conf into your WireGuard client")
}

func buildWireguardConfig(privateKey string, resp *api.WireguardConfigResponse) string {
	var b strings.Builder
	fmt.Fprintln(&b, "[Interface]")
	fmt.Fprintf(&b, "PrivateKey = %s\n", privateKey)
	fmt.Fprintf(&b, "Address = %s\n", resp.Config.Address)
	if len(resp.Config.DNS) > 0 {
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(resp.Config.DNS, ", "))
	}
	if resp.Config.MTU > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", resp.Config.MTU)
	}
	if resp.Config.PersistentKeepaliveSec > 0 {
		fmt.Fprintf(&b, "PersistentKeepalive = %d\n", resp.Config.PersistentKeepaliveSec)
	}

	for _, peer := range resp.Peers {
		fmt.Fprintln(&b, "\n[Peer]")
		fmt.Fprintf(&b, "PublicKey = %s\n", peer.PublicKey)
		if peer.Endpoint != "" {
			fmt.Fprintf(&b, "Endpoint = %s\n", peer.Endpoint)
		}
		if len(peer.AllowedIPs) > 0 {
			fmt.Fprintf(&b, "AllowedIPs = %s\n", strings.Join(peer.AllowedIPs, ", "))
		}
		if peer.PersistentKeepaliveSecs > 0 {
			fmt.Fprintf(&b, "PersistentKeepalive = %d\n", peer.PersistentKeepaliveSecs)
		}
		if peer.DERPRegion != "" {
			fmt.Fprintf(&b, "# DERP Region: %s\n", peer.DERPRegion)
		}
	}

	return strings.TrimSpace(b.String())
}

// keyToHexForDaemon converts a WireGuard key to hex for the daemon (wireguard-go UAPI expects hex).
// Keys from enroll (wgtypes) are base64; the daemon passes them to IpcSet which expects hex.
func keyToHexForDaemon(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if idx := strings.IndexAny(raw, "\r\n"); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	if raw == "" {
		return "", errors.New("key is empty")
	}
	if len(raw) == 64 && isHexKey(raw) {
		return strings.ToLower(raw), nil
	}
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding, base64.StdEncoding, base64.URLEncoding} {
		dec, err := enc.DecodeString(raw)
		if err == nil && len(dec) == 32 {
			return hex.EncodeToString(dec), nil
		}
	}
	return "", fmt.Errorf("key must be 32 bytes base64 or 64 hex chars, got %d chars", len(raw))
}

func isHexKey(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// addressToCIDR ensures the daemon receives a valid CIDR (e.g. 100.96.0.1/32).
// The API often returns a bare IP; netlink expects a CIDR.
func addressToCIDR(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return addr
	}
	if strings.Contains(addr, "/") {
		return addr
	}
	return addr + "/32"
}

func buildDaemonApplyConfig(privateKey string, resp *api.WireguardConfigResponse) (daemon.ApplyConfigRequest, error) {
	privateKeyHex, err := keyToHexForDaemon(privateKey)
	if err != nil {
		return daemon.ApplyConfigRequest{}, fmt.Errorf("private key: %w", err)
	}
	cfg := daemon.ApplyConfigRequest{
		Interface: daemon.InterfaceConfig{
			PrivateKey: privateKeyHex,
			Address:    addressToCIDR(resp.Config.Address),
			DNS:        resp.Config.DNS,
			MTU:        resp.Config.MTU,
		},
	}

	for _, peer := range resp.Peers {
		pubHex, err := keyToHexForDaemon(peer.PublicKey)
		if err != nil {
			return daemon.ApplyConfigRequest{}, fmt.Errorf("peer public key: %w", err)
		}
		cfg.Peers = append(cfg.Peers, daemon.PeerConfig{
			PublicKey:  pubHex,
			Endpoint:   peer.Endpoint,
			AllowedIPs: peer.AllowedIPs,
			Keepalive:  peer.PersistentKeepaliveSecs,
		})
	}

	if len(resp.Warnings) > 0 {
		cfg.Warnings = append(cfg.Warnings, resp.Warnings...)
	}

	return cfg, nil
}

func newMeshUpCommand() *cobra.Command {
	var (
		deviceID   string
		socketPath string
		applyOnly  bool
	)

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Apply the latest WireGuard config and start the mesh tunnel via prysm-meshd",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()

			if deviceID == "" {
				if host, err := os.Hostname(); err == nil {
					deviceID = host
				}
			}

			deviceID = strings.TrimSpace(deviceID)
			if deviceID == "" {
				return errors.New("device identifier is required")
			}

			keyPath := filepath.Join(app.Config.HomeDir, "wireguard", fmt.Sprintf("%s.key", sanitizeFileSegment(deviceID)))
			keyBytes, err := os.ReadFile(keyPath)
			if err != nil {
				return fmt.Errorf("read private key: %w (run `prysm mesh enroll` first)", err)
			}

			privateKey := strings.TrimSpace(string(keyBytes))
			if privateKey == "" {
				return fmt.Errorf("private key file %s is empty", keyPath)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			resp, err := app.API.GetWireguardConfig(ctx, deviceID)
			if err != nil {
				return err
			}

			client := daemon.NewClient(socketPath)
			applyReq, err := buildDaemonApplyConfig(privateKey, resp)
			if err != nil {
				return err
			}

			if err := client.Apply(ctx, applyReq); err != nil {
				return fmt.Errorf("apply config: %w", err)
			}

			if len(applyReq.Warnings) > 0 {
				color.New(color.FgYellow).Println("Warnings:")
				for _, w := range applyReq.Warnings {
					color.New(color.FgYellow).Printf("  • %s\n", w)
				}
			}

			color.New(color.FgGreen).Println("✅ Configuration applied to prysm-meshd")

			if applyOnly {
				return nil
			}

			startCtx, startCancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer startCancel()
			if err := client.Start(startCtx); err != nil {
				return fmt.Errorf("start tunnel: %w", err)
			}

			color.New(color.FgGreen).Println("🚀 Mesh tunnel started")
			return nil
		},
	}

	cmd.Flags().StringVar(&deviceID, "device-id", "", "device identifier (default: system hostname)")
	cmd.Flags().StringVar(&socketPath, "socket", daemon.DefaultSocket(), "path to the prysm-meshd Unix domain socket")
	cmd.Flags().BoolVar(&applyOnly, "apply-only", false, "apply configuration but do not start the tunnel")
	return cmd
}

func newMeshDownCommand() *cobra.Command {
	var socketPath string

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the mesh tunnel via prysm-meshd",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := daemon.NewClient(socketPath)
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := client.Stop(ctx); err != nil {
				return fmt.Errorf("stop tunnel: %w", err)
			}

			color.New(color.FgGreen).Println("🛑 Mesh tunnel stopped")
			return nil
		},
	}

	cmd.Flags().StringVar(&socketPath, "socket", daemon.DefaultSocket(), "path to the prysm-meshd Unix domain socket")
	return cmd
}

func newMeshStatusCommand() *cobra.Command {
	var socketPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show runtime status reported by prysm-meshd",
		RunE: func(cmd *cobra.Command, args []string) error {
			derpPid, derpRunning := readDerpPidAndCheckRunning()

			client := daemon.NewClient(socketPath)
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			// Try to fetch mesh nodes and clusters from backend
			var meshNodes []api.MeshNode
			var clusters []api.Cluster
			func() {
				defer func() { _ = recover() }()
				app := MustApp()
				if nodes, err := app.API.ListMeshNodes(ctx); err == nil {
					meshNodes = nodes
				}
				if c, err := app.API.ListClusters(ctx); err == nil {
					clusters = c
				}
			}()

			status, err := client.Status(ctx)
			if err != nil {
				if derpRunning {
					color.New(color.FgGreen).Printf("DERP: connected (PID %d)\n", derpPid)
					color.New(color.FgHiBlack).Println("Mesh interface: not used (DERP-only mode)")
				} else {
					color.New(color.FgHiBlack).Println("For mesh via DERP only (no daemon): prysm mesh connect")
				}
				return fmt.Errorf("query status: %w", err)
			}

			if status.InterfaceUp {
				color.New(color.FgGreen).Println("✅ Mesh interface is up")
			} else {
				color.New(color.FgHiBlack).Println("WireGuard: not used (DERP-only)")
			}

			if derpRunning {
				color.New(color.FgGreen).Printf("DERP: connected (PID %d)\n", derpPid)
			} else {
				color.New(color.FgHiBlack).Println("DERP: not connected")
			}

			if !status.LastApply.IsZero() {
				color.New(color.FgHiBlack).Printf("Last apply: %s\n", status.LastApply.UTC().Format(time.RFC3339))
			}
			color.New(color.FgHiBlack).Printf("WireGuard peers: %d\n", status.PeerCount)
			if status.PeerCount == 0 {
				color.New(color.FgHiBlack).Println("  (tunnel endpoints; mesh nodes → prysm mesh peers)")
			}
			// Show registered clusters
			if len(clusters) > 0 {
				fmt.Println()
				color.New(color.FgCyan).Printf("Registered clusters: %d\n", len(clusters))
				for _, c := range clusters {
					statusColor := color.FgHiBlack
					statusIcon := "○"
					if c.Status == "connected" {
						statusColor = color.FgGreen
						statusIcon = "●"
					} else if c.Status == "disconnected" {
						statusColor = color.FgRed
						statusIcon = "○"
					}
					color.New(statusColor).Printf("  %s %s", statusIcon, c.Name)
					if c.MeshIP != "" {
						color.New(color.FgBlue).Printf(" (%s)", c.MeshIP)
					}
					if c.Region != "" {
						color.New(color.FgHiBlack).Printf(" [%s]", c.Region)
					}
					if c.IsExitRouter {
						color.New(color.FgYellow).Printf(" [exit]")
					}
					color.New(color.FgHiBlack).Printf(" %s\n", c.Status)
				}
			}

			if len(meshNodes) > 0 {
				fmt.Println()
				color.New(color.FgCyan).Printf("Mesh peers: %d\n", len(meshNodes))
				for _, node := range meshNodes {
					nodeType := node.PeerType
					if nodeType == "" {
						nodeType = "unknown"
					}
					statusColor := color.FgHiBlack
					statusIcon := "○"
					if node.Status == "connected" {
						statusColor = color.FgGreen
						statusIcon = "●"
					}
					ip := node.WGAddress
					if ip == "" {
						ip = "(DERP relay)"
					}
					color.New(statusColor).Printf("  %s ", statusIcon)
					color.New(color.FgHiBlack).Printf("%s [%s] - %s\n", node.DeviceID, nodeType, ip)
				}
			}

			if len(status.Peers) > 0 {
				fmt.Println()
				color.New(color.FgCyan).Println("WireGuard peers")
				for _, peer := range status.Peers {
					color.New(color.FgHiBlack).Printf("  %s\n", peer.PublicKey)
					if peer.Endpoint != "" {
						color.New(color.FgHiBlack).Printf("    Endpoint: %s\n", peer.Endpoint)
					}
					if peer.LastHandshake != "" {
						color.New(color.FgHiBlack).Printf("    Last handshake: %s\n", peer.LastHandshake)
					}
					color.New(color.FgHiBlack).Printf("    RX: %d bytes • TX: %d bytes\n", peer.BytesReceived, peer.BytesSent)
				}
			}

			if len(status.Warnings) > 0 {
				fmt.Println()
				color.New(color.FgYellow).Println("Warnings:")
				for _, w := range status.Warnings {
					color.New(color.FgYellow).Printf("  • %s\n", w)
				}
			}

			if !status.InterfaceUp && !derpRunning {
				fmt.Println()
				color.New(color.FgHiBlack).Println("Tip: Run `prysm mesh connect` to join the mesh via DERP (no WireGuard needed).")
			} else if status.InterfaceUp && status.PeerCount == 0 {
				fmt.Println()
				color.New(color.FgHiBlack).Println("Tip: Run `prysm mesh connect` to join the mesh via DERP when no direct relays are configured.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&socketPath, "socket", daemon.DefaultSocket(), "path to the prysm-meshd Unix domain socket")
	return cmd
}

func sanitizeFileSegment(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return "device"
	}
	input = strings.ToLower(input)
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "..", "-", "@", "-", "#", "-", "%", "-")
	return replacer.Replace(input)
}
