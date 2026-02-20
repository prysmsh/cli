package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/config"
	"github.com/warp-run/prysm-cli/internal/derp"
	"github.com/warp-run/prysm-cli/internal/util"
)

func newTunnelCommand() *cobra.Command {
	tunnelCmd := &cobra.Command{
		Use:   "tunnel",
		Short: "Expose secure tunnels via authenticated mesh peers",
	}

	tunnelCmd.AddCommand(
		newTunnelExposeCommand(),
		newTunnelConnectCommand(),
		newTunnelListCommand(),
		newTunnelDeleteCommand(),
		newTunnelDiagnoseCommand(),
	)

	return tunnelCmd
}

func newTunnelExposeCommand() *cobra.Command {
	var (
		port         int
		name         string
		toPeer       string
		externalPort int
		public       bool
		background   bool
		verbose      bool
	)

	cmd := &cobra.Command{
		Use:   "expose [port]",
		Short: "Expose a local port via mesh and optionally as a public URL",
		Long: `Expose a local port so other authenticated peers can connect via the mesh.
With --public, also generates a public URL (https://<id>.tunnel.prysm.sh).

This is a long-lived command (like ngrok). Use --background to run detached.
Press Ctrl+C to stop when running in foreground.`,
		Example: `  # Expose port 8080 with public URL
  prysm tunnel expose 8080 --public

  # Run in background
  prysm tunnel expose 3000 --public --background`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Port: positional arg takes precedence over -p flag
			if len(args) > 0 {
				if _, err := fmt.Sscanf(args[0], "%d", &port); err != nil || port <= 0 || port > 65535 {
					return errors.New("port must be between 1-65535")
				}
			}
			if port <= 0 || port > 65535 {
				return errors.New("port is required (e.g. prysm tunnel expose 8080 or -p 8080)")
			}

			// When --background, spawn a detached child and exit
			if background && os.Getenv("PRYSM_TUNNEL_DAEMON") == "" {
				return runTunnelExposeBackground(port, name, toPeer, externalPort, public, verbose)
			}

			app := MustApp()

			deviceID, err := derp.EnsureDeviceID(app.Config.HomeDir)
			if err != nil {
				return fmt.Errorf("ensure device id: %w", err)
			}

			sess, err := app.Sessions.Load()
			if err != nil {
				return err
			}
			if sess == nil {
				return fmt.Errorf("no active session; run `prysm login`")
			}

			// 1. Create tunnel via API
			createCtx, createCancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			tunnel, err := app.API.CreateTunnel(createCtx, api.TunnelCreateRequest{
				Port:           port,
				Name:           strings.TrimSpace(name),
				TargetDeviceID: deviceID,
				ToPeerDeviceID: strings.TrimSpace(toPeer),
				ExternalPort:   externalPort,
				Protocol:       "tcp",
				IsPublic:       public,
			})
			createCancel()
			if err != nil {
				return err
			}

			// 2. Print tunnel info
			fmt.Println()
			color.New(color.FgGreen, color.Bold).Printf("Tunnel active: localhost:%d\n", port)
			if tunnel.IsPublic && tunnel.ExternalURL != "" {
				color.New(color.FgCyan).Printf("  Public URL:  %s\n", tunnel.ExternalURL)
			}
			color.New(color.FgHiBlack).Printf("  Mesh:        prysm tunnel connect --peer %s --port %d\n", deviceID, port)
			fmt.Printf("  Tunnel ID:   %d\n", tunnel.ID)
			fmt.Printf("  Status:      %s\n", tunnel.Status)
			if tunnel.ToPeerDeviceID != "" {
				fmt.Printf("  Restricted:  %s\n", tunnel.ToPeerDeviceID)
			}
			fmt.Println()
			if os.Getenv("PRYSM_TUNNEL_DAEMON") != "" {
				color.New(color.FgHiBlack).Println("Running in background. Use `prysm tunnel delete <id>` to stop.")
			} else {
				color.New(color.FgHiBlack).Println("Press Ctrl+C to stop")
			}
			fmt.Println()

			// 3. Connect to DERP relay and handle tunnel traffic
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			relay := app.Config.DERPServerURL
			if relay == "" {
				relay = sess.DERPServerURL
			}
			if relay == "" {
				return fmt.Errorf("DERP relay URL not configured")
			}

			var derpToken string
			if tokResp, tokErr := app.API.GetDERPTunnelToken(ctx, deviceID); tokErr == nil && tokResp != nil && tokResp.Token != "" {
				derpToken = tokResp.Token
			}

			// Route tracking for bidirectional forwarding
			routeConns := make(map[string]net.Conn)
			routeConnsMu := sync.RWMutex{}
			var derpClient *derp.Client

			headers := make(http.Header)
			headers.Set("Authorization", "Bearer "+sess.Token)
			headers.Set("X-Session-ID", sess.SessionID)
			headers.Set("X-Org-ID", fmt.Sprintf("%d", sess.Organization.ID))

			logTunnel := func(format string, args ...interface{}) {
				if verbose || app.Debug {
					fmt.Fprintf(os.Stderr, format, args...)
				}
			}
			derpOpts := []derp.Option{
				derp.WithHeaders(headers),
				derp.WithInsecure(app.InsecureTLS),
				derp.WithLogLevel(derp.LogInfo),
			}
			if verbose || app.Debug {
				derpOpts = append(derpOpts, derp.WithLogLevel(derp.LogDebug))
			}
			derpOpts = append(derpOpts, derp.WithTunnelTrafficHandler(func(routeID string, targetPort, _ int, data []byte) {
				if data != nil {
					// traffic_data: forward to existing local connection
					logTunnel("[tunnel] traffic_data route=%s len=%d\n", routeID, len(data))
					routeConnsMu.RLock()
					conn := routeConns[routeID]
					routeConnsMu.RUnlock()
					if conn != nil {
						n, wErr := conn.Write(data)
						logTunnel("[tunnel] wrote %d bytes to local conn (err=%v)\n", n, wErr)
					} else {
						logTunnel("[tunnel] no local conn for route %s\n", routeID)
					}
					return
				}
				// route_setup: dial localhost:<targetPort> and start forwarding
				addr := fmt.Sprintf("127.0.0.1:%d", targetPort)
				logTunnel("[tunnel] route_setup route=%s dialing %s\n", routeID, addr)
				conn, dialErr := net.Dial("tcp", addr)
				if dialErr != nil {
					color.New(color.FgRed).Fprintf(os.Stderr, "tunnel dial %s: %v\n", addr, dialErr)
					return
				}
				logTunnel("[tunnel] connected to %s\n", addr)
				routeConnsMu.Lock()
				routeConns[routeID] = conn
				routeConnsMu.Unlock()

				go func() {
					defer func() {
						routeConnsMu.Lock()
						delete(routeConns, routeID)
						routeConnsMu.Unlock()
						conn.Close()
					}()
					buf := make([]byte, 32*1024)
					for {
						n, readErr := conn.Read(buf)
						if n > 0 {
							logTunnel("[tunnel] read %d bytes from local, sending traffic_data\n", n)
							if sendErr := derpClient.SendTrafficData(routeID, buf[:n]); sendErr != nil {
								logTunnel("[tunnel] SendTrafficData error: %v\n", sendErr)
								return
							}
						}
						if readErr != nil {
							if readErr != io.EOF {
								logTunnel("tunnel read: %v\n", readErr)
							}
							// Send empty traffic_data to signal end-of-stream
							_ = derpClient.SendTrafficData(routeID, nil)
							return
						}
					}
				}()
			}))
			if derpToken != "" {
				derpOpts = append(derpOpts, derp.WithDERPTunnelToken(derpToken))
			} else {
				derpOpts = append(derpOpts, derp.WithSessionToken(sess.Token))
			}
			derpClient = derp.NewClient(relay, deviceID, derpOpts...)

			errCh := make(chan error, 1)
			go func() {
				errCh <- derpClient.Run(ctx)
			}()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)

			// 4. Wait for signal or error, then clean up
			select {
			case <-ctx.Done():
				cleanupTunnel(app, tunnel.ID)
				return ctx.Err()
			case sig := <-sigCh:
				color.New(color.FgYellow).Printf("\nReceived %s, cleaning up tunnel...\n", sig)
				derpClient.Close()
				cleanupTunnel(app, tunnel.ID)
				return nil
			case runErr := <-errCh:
				derpClient.Close()
				cleanupTunnel(app, tunnel.ID)
				return runErr
			}
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "local port to expose (alternative to positional arg)")
	cmd.Flags().StringVar(&name, "name", "", "optional tunnel name")
	cmd.Flags().StringVar(&toPeer, "to-peer", "", "restrict access to specific peer device ID")
	cmd.Flags().IntVar(&externalPort, "external-port", 0, "external port (auto-allocated if omitted)")
	cmd.Flags().BoolVar(&public, "public", false, "generate a public URL (https://<id>.tunnel.prysm.sh)")
	cmd.Flags().BoolVarP(&background, "background", "b", false, "run in background (detached)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose tunnel traffic logging")

	return cmd
}

// runTunnelExposeBackground spawns a detached child process running tunnel expose.
func runTunnelExposeBackground(port int, name, toPeer string, externalPort int, public, verbose bool) error {
	homeDir, err := config.DefaultHomeDir()
	if err != nil {
		return fmt.Errorf("config dir: %w", err)
	}
	logDir := filepath.Join(homeDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, "tunnel.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	args := []string{"tunnel", "expose", fmt.Sprintf("%d", port)}
	if name != "" {
		args = append(args, "--name", name)
	}
	if toPeer != "" {
		args = append(args, "--to-peer", toPeer)
	}
	if externalPort > 0 {
		args = append(args, "--external-port", fmt.Sprintf("%d", externalPort))
	}
	if public {
		args = append(args, "--public")
	}
	if verbose {
		args = append(args, "--verbose")
	}

	child := exec.Command(os.Args[0], args...)
	child.Env = append(os.Environ(), "PRYSM_TUNNEL_DAEMON=1")
	child.Stdin = nil
	child.Stdout = logFile
	child.Stderr = logFile
	if child.SysProcAttr == nil {
		child.SysProcAttr = &syscall.SysProcAttr{}
	}
	setSysProcAttrSetsid(child.SysProcAttr)

	if err := child.Start(); err != nil {
		return fmt.Errorf("start tunnel: %w", err)
	}

	pidPath := filepath.Join(homeDir, fmt.Sprintf("tunnel-%d.pid", port))
	_ = os.MkdirAll(filepath.Dir(pidPath), 0o700)
	_ = os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", child.Process.Pid)), 0o600)

	fmt.Println()
	color.New(color.FgGreen, color.Bold).Printf("Tunnel running in background (PID: %d)\n", child.Process.Pid)
	color.New(color.FgHiBlack).Printf("  Log: %s\n", logPath)
	color.New(color.FgHiBlack).Printf("  Stop: kill %d  or  prysm tunnel delete <id>\n", child.Process.Pid)
	fmt.Println()

	return nil
}

// cleanupTunnel deletes the tunnel record on graceful shutdown.
func cleanupTunnel(app *App, tunnelID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.API.DeleteTunnel(ctx, tunnelID); err != nil {
		color.New(color.FgHiBlack).Fprintf(os.Stderr, "cleanup tunnel %d: %v\n", tunnelID, err)
	} else {
		color.New(color.FgGreen).Println("Tunnel deleted.")
	}
}

func newTunnelConnectCommand() *cobra.Command {
	var (
		peerRef   string
		port      int
		localPort int
	)

	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to a peer's exposed port",
		Long:  "Connect to a peer's exposed port and forward traffic to a local port. Establishes a DERP connection and TCP proxy.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(peerRef) == "" {
				return errors.New("--peer is required")
			}
			if port <= 0 || port > 65535 {
				return errors.New("--port must be between 1-65535")
			}

			app := MustApp()
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			// Look up tunnel from API
			listCtx, listCancel := context.WithTimeout(ctx, 20*time.Second)
			tunnels, err := app.API.ListTunnels(listCtx, peerRef)
			listCancel()
			if err != nil {
				return err
			}

			var match *api.Tunnel
			for i := range tunnels {
				t := &tunnels[i]
				if t.TargetDeviceID == peerRef && t.Port == port {
					match = t
					break
				}
			}
			if match == nil {
				return fmt.Errorf("no tunnel found for peer %s port %d", peerRef, port)
			}

			lp := localPort
			if lp <= 0 {
				lp = port
			}

			sess, err := app.Sessions.Load()
			if err != nil {
				return err
			}
			if sess == nil {
				return fmt.Errorf("no active session; run `prysm login`")
			}

			relay := app.Config.DERPServerURL
			if relay == "" {
				relay = sess.DERPServerURL
			}
			if relay == "" {
				return fmt.Errorf("DERP relay URL not configured")
			}

			deviceID, err := derp.EnsureDeviceID(app.Config.HomeDir)
			if err != nil {
				return fmt.Errorf("ensure device id: %w", err)
			}

			// Prefer signed DERP tunnel token (org binding cryptographically enforced)
			var derpToken string
			if tokResp, err := app.API.GetDERPTunnelToken(ctx, deviceID); err == nil && tokResp != nil && tokResp.Token != "" {
				derpToken = tokResp.Token
			}

			// Map routeID -> net.Conn for traffic_data forwarding
			routeConns := make(map[string]net.Conn)
			routeConnsMu := sync.RWMutex{}

			headers := make(http.Header)
			headers.Set("Authorization", "Bearer "+sess.Token)
			headers.Set("X-Session-ID", sess.SessionID)
			headers.Set("X-Org-ID", fmt.Sprintf("%d", sess.Organization.ID))

			derpOpts := []derp.Option{
				derp.WithHeaders(headers),
				derp.WithInsecure(app.InsecureTLS),
				derp.WithTunnelTrafficHandler(func(routeID string, _, _ int, data []byte) {
					if data == nil {
						return
					}
					routeConnsMu.RLock()
					conn := routeConns[routeID]
					routeConnsMu.RUnlock()
					if conn != nil {
						conn.Write(data)
					}
				}),
			}
			if derpToken != "" {
				derpOpts = append(derpOpts, derp.WithDERPTunnelToken(derpToken))
			} else {
				derpOpts = append(derpOpts, derp.WithSessionToken(sess.Token))
			}
			client := derp.NewClient(relay, deviceID, derpOpts...)

			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", lp))
			if err != nil {
				return fmt.Errorf("listen on localhost:%d: %w", lp, err)
			}
			defer listener.Close()

			color.New(color.FgGreen).Printf("Tunnel: %s:%d -> localhost:%d\n", peerRef, port, lp)
			fmt.Printf("  Tunnel ID: %d\n", match.ID)
			fmt.Printf("  Connect to localhost:%d to reach %s:%d\n", lp, peerRef, port)

			targetClient := "device_" + peerRef
			orgID := fmt.Sprintf("%d", match.OrganizationID)

			go func() {
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					routeID, err := client.SendRouteRequest(orgID, targetClient, match.ExternalPort, match.Port, "TCP")
					if err != nil {
						color.New(color.FgRed).Fprintf(os.Stderr, "route request failed: %v\n", err)
						conn.Close()
						continue
					}
					routeConnsMu.Lock()
					routeConns[routeID] = conn
					routeConnsMu.Unlock()

					go func() {
						defer func() {
							routeConnsMu.Lock()
							delete(routeConns, routeID)
							routeConnsMu.Unlock()
							conn.Close()
						}()
						buf := make([]byte, 32*1024)
						for {
							n, err := conn.Read(buf)
							if n > 0 {
								if sendErr := client.SendTrafficData(routeID, buf[:n]); sendErr != nil {
									return
								}
							}
							if err != nil {
								if err != io.EOF {
									color.New(color.FgHiBlack).Fprintf(os.Stderr, "tunnel read: %v\n", err)
								}
								return
							}
						}
					}()
				}
			}()

			errCh := make(chan error, 1)
			go func() {
				errCh <- client.Run(ctx)
			}()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case sig := <-sigCh:
				color.New(color.FgYellow).Printf("Received %s, closing tunnel...\n", sig)
				client.Close()
				return nil
			case err := <-errCh:
				client.Close()
				return err
			}
		},
	}

	cmd.Flags().StringVar(&peerRef, "peer", "", "peer device ID (from `prysm mesh peers`)")
	cmd.Flags().IntVarP(&port, "port", "p", 0, "peer's exposed port")
	cmd.Flags().IntVarP(&localPort, "local-port", "l", 0, "local port to bind (default: same as port)")
	_ = cmd.MarkFlagRequired("peer")
	_ = cmd.MarkFlagRequired("port")

	return cmd
}

func newTunnelListCommand() *cobra.Command {
	var deviceFilter string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active tunnels",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			tunnels, err := app.API.ListTunnels(ctx, strings.TrimSpace(deviceFilter))
			if err != nil {
				return err
			}

			if len(tunnels) == 0 {
				color.New(color.FgYellow).Println("No tunnels defined.")
				return nil
			}

			fmt.Printf("%-6s %-12s %-8s %-10s %-10s %-8s %s\n", "ID", "DEVICE", "PORT", "EXT.PORT", "TO_PEER", "STATUS", "PUBLIC URL")
			for _, t := range tunnels {
				toPeer := "-"
				if t.ToPeerDeviceID != "" {
					toPeer = t.ToPeerDeviceID
				}
				publicURL := "-"
				if t.IsPublic && t.ExternalURL != "" {
					publicURL = t.ExternalURL
				}
				fmt.Printf("%-6d %-12s %-8d %-10d %-10s %-8s %s\n",
					t.ID, truncate(t.TargetDeviceID, 12), t.Port, t.ExternalPort, truncate(toPeer, 10), t.Status, publicURL)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&deviceFilter, "device", "", "filter by target device ID")
	return cmd
}

func newTunnelDiagnoseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "diagnose",
		Short: "Diagnose tunnel connectivity (session, API, DERP)",
		Long:  "Run tests to diagnose issues establishing tunnel connectivity. Exits 0 if OK, 1 with error details.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			var failed bool

			// 1. Session check
			sess, err := app.Sessions.Load()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Session: FAIL — %v\n", err)
				return err
			}
			if sess == nil {
				fmt.Fprintf(os.Stderr, "Session: FAIL — no active session; run `prysm login`\n")
				return errors.New("no session")
			}
			fmt.Fprintf(os.Stdout, "Session: OK\n")

			// 2. API / profile check
			if _, err := app.API.GetProfile(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "API: FAIL — %v\n", err)
				failed = true
			} else {
				fmt.Fprintf(os.Stdout, "API: OK\n")
			}

			// 3. DERP URL
			relay := app.Config.DERPServerURL
			if relay == "" {
				relay = sess.DERPServerURL
			}
			if relay == "" {
				fmt.Fprintf(os.Stderr, "DERP: FAIL — DERP relay URL not configured\n")
				failed = true
			} else {
				// 4. DERP connectivity (quick dial + register)
				deviceID, _ := derp.EnsureDeviceID(app.Config.HomeDir)
				headers := make(http.Header)
				headers.Set("Authorization", "Bearer "+sess.Token)
				headers.Set("X-Org-ID", fmt.Sprintf("%d", sess.Organization.ID))
				derpOpts := []derp.Option{derp.WithHeaders(headers), derp.WithInsecure(app.InsecureTLS)}
				if tokResp, tokErr := app.API.GetDERPTunnelToken(ctx, deviceID); tokErr == nil && tokResp != nil && tokResp.Token != "" {
					derpOpts = append(derpOpts, derp.WithDERPTunnelToken(tokResp.Token))
				} else {
					derpOpts = append(derpOpts, derp.WithSessionToken(sess.Token))
				}
				derpClient := derp.NewClient(relay, deviceID, derpOpts...)
				runCtx, runCancel := context.WithTimeout(ctx, 15*time.Second)
				err = derpClient.Run(runCtx)
				runCancel()
				derpClient.Close()
				if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					fmt.Fprintf(os.Stderr, "DERP: FAIL — %v\n", err)
					failed = true
				} else {
					fmt.Fprintf(os.Stdout, "DERP: OK (device %s)\n", truncate(deviceID, 16))
				}
			}

			if failed {
				return errors.New("diagnose failed")
			}
			return nil
		},
	}
}

func newTunnelDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete [tunnel-id]",
		Aliases: []string{"rm"},
		Short:   "Delete a tunnel",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			tunnelID := args[0]
			if err := util.SafePathSegment(tunnelID); err != nil {
				return fmt.Errorf("invalid tunnel ID: %w", err)
			}
			if err := app.API.DeleteTunnelByID(ctx, tunnelID); err != nil {
				return err
			}

			color.New(color.FgGreen).Printf("Tunnel %s deleted\n", tunnelID)
			return nil
		},
	}
	return cmd
}
