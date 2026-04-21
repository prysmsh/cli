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

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/config"
	"github.com/prysmsh/cli/internal/derp"
	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
	"github.com/prysmsh/cli/internal/util"
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
		port              int
		name              string
		toPeer            string
		externalPort      int
		public            bool
		background        bool
		verbose           bool
		clusterRef        string
		service           string
		namespace         string
		scheme            string
		insecureUpstream  bool
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

			scheme = strings.ToLower(strings.TrimSpace(scheme))
			if scheme != "http" && scheme != "https" {
				return fmt.Errorf("--scheme must be http or https (got %q)", scheme)
			}

			if strings.TrimSpace(clusterRef) != "" {
				if background {
					return errors.New("--background is not supported for cluster tunnels")
				}
				if strings.TrimSpace(service) == "" {
					return errors.New("--service is required for cluster tunnels")
				}
				if strings.TrimSpace(namespace) == "" {
					namespace = "default"
				}

				app := MustApp()
				ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
				defer cancel()

				cluster, err := resolveClusterForTunnel(ctx, app, clusterRef)
				if err != nil {
					return err
				}

				var tunnel *api.Tunnel
				if err := ui.WithSpinner("Creating tunnel...", func() error {
					var createErr error
					tunnel, createErr = app.API.CreateTunnel(ctx, api.TunnelCreateRequest{
						Port:            port,
						Name:            strings.TrimSpace(name),
						TargetDeviceID:  fmt.Sprintf("cluster_%d", cluster.ID),
						ToPeerDeviceID:  strings.TrimSpace(toPeer),
						ExternalPort:    externalPort,
						Protocol:        "tcp",
						IsPublic:        public,
						TargetService:   strings.TrimSpace(service),
						TargetNamespace: strings.TrimSpace(namespace),
					})
					return createErr
				}); err != nil {
					return err
				}

				fmt.Println()
				fmt.Println(style.Success.Copy().Bold(true).Render(fmt.Sprintf("Tunnel active: %s/%s:%d", namespace, service, port)))
				if tunnel.IsPublic && tunnel.ExternalURL != "" {
					fmt.Println(style.Info.Render(fmt.Sprintf("  Public URL:  %s", tunnel.ExternalURL)))
				} else {
					fmt.Println(style.Info.Render(fmt.Sprintf("  Connect:     prysm tunnel connect --cluster %s --service %s --namespace %s --port %d", cluster.Name, service, namespace, port)))
				}
				fmt.Printf("  Cluster:     %s\n", cluster.Name)
				fmt.Printf("  Tunnel ID:   %d\n", tunnel.ID)
				fmt.Printf("  Status:      %s\n", tunnel.Status)
				if tunnel.ToPeerDeviceID != "" {
					fmt.Printf("  Restricted:  %s\n", tunnel.ToPeerDeviceID)
				}
				fmt.Println()
				return nil
			}

			// When --background, spawn a detached child and exit
			if background && os.Getenv("PRYSM_TUNNEL_DAEMON") == "" {
				return runTunnelExposeBackground(port, name, toPeer, externalPort, public, verbose, scheme, insecureUpstream)
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

			// Per-request log state; only populated in foreground (daemon mode is silent).
			type pendingReq struct {
				start  time.Time
				method string
				path   string
			}
			showReqLog := os.Getenv("PRYSM_TUNNEL_DAEMON") == ""
			reqLogs := make(map[string]*pendingReq)
			reqLogsMu := sync.Mutex{}

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
					if showReqLog {
						// First bytes of a request carry the HTTP request line. Only
						// stamp the earliest observation per route — skip subsequent
						// chunks (body, keep-alive continuations) until a response is
						// seen and the entry is cleared.
						reqLogsMu.Lock()
						if _, exists := reqLogs[routeID]; !exists {
							if method, path, ok := parseHTTPRequestLine(data); ok {
								reqLogs[routeID] = &pendingReq{start: time.Now(), method: method, path: path}
							}
						}
						reqLogsMu.Unlock()
					}
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
				logTunnel("[tunnel] route_setup route=%s dialing %s (scheme=%s)\n", routeID, addr, scheme)
				conn, dialErr := dialUpstream(addr, scheme, insecureUpstream)
				if dialErr != nil {
					fmt.Fprintf(os.Stderr, "%s\n", style.Error.Render(fmt.Sprintf("tunnel dial %s: %v", addr, dialErr)))
					return
				}
				logTunnel("[tunnel] connected to %s (scheme=%s)\n", addr, scheme)
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
							if showReqLog {
								// Response status line is in the first chunk from the
								// local server. Pair it with the pending request and
								// print one log line per request/response round-trip.
								if status, ok := parseHTTPStatusLine(buf[:n]); ok {
									reqLogsMu.Lock()
									entry := reqLogs[routeID]
									delete(reqLogs, routeID)
									reqLogsMu.Unlock()
									if entry != nil {
										printTunnelRequest(entry.method, entry.path, status, time.Since(entry.start))
									}
								}
							}
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

			// 1. Wait until the DERP socket is up + registered. Only then is it honest to
			//    advertise a tunnel — incoming requests before this point would get
			//    "device not connected" from the backend proxy.
			select {
			case <-derpClient.Ready():
			case runErr := <-errCh:
				derpClient.Close()
				return fmt.Errorf("connect to DERP relay: %w", runErr)
			case <-time.After(15 * time.Second):
				derpClient.Close()
				return fmt.Errorf("timed out connecting to DERP relay at %s", relay)
			case <-ctx.Done():
				derpClient.Close()
				return ctx.Err()
			}

			// 2. Create tunnel record via API. The relay already knows about this CLI,
			//    so the backend's pre-registration handshake will resolve cleanly.
			var tunnel *api.Tunnel
			if err := ui.WithSpinner("Creating tunnel...", func() error {
				createCtx, createCancel := context.WithTimeout(ctx, 20*time.Second)
				defer createCancel()
				var createErr error
				tunnel, createErr = app.API.CreateTunnel(createCtx, api.TunnelCreateRequest{
					Port:           port,
					Name:           strings.TrimSpace(name),
					TargetDeviceID: deviceID,
					ToPeerDeviceID: strings.TrimSpace(toPeer),
					ExternalPort:   externalPort,
					Protocol:       "tcp",
					IsPublic:       public,
				})
				return createErr
			}); err != nil {
				derpClient.Close()
				return err
			}

			// 3. Print tunnel info
			fmt.Println()
			fmt.Println(style.Success.Copy().Bold(true).Render(fmt.Sprintf("Tunnel active: localhost:%d", port)))
			if tunnel.IsPublic && tunnel.ExternalURL != "" {
				fmt.Println(style.Info.Render(fmt.Sprintf("  Public URL:  %s", tunnel.ExternalURL)))
			}
			fmt.Println(style.MutedStyle.Render(fmt.Sprintf("  Mesh:        prysm tunnel connect --peer %s --port %d", deviceID, port)))
			fmt.Printf("  Tunnel ID:   %d\n", tunnel.ID)
			fmt.Printf("  Status:      %s\n", tunnel.Status)
			if tunnel.ToPeerDeviceID != "" {
				fmt.Printf("  Restricted:  %s\n", tunnel.ToPeerDeviceID)
			}
			fmt.Println()
			if os.Getenv("PRYSM_TUNNEL_DAEMON") != "" {
				fmt.Println(style.MutedStyle.Render("Running in background. Use `prysm tunnel delete <id>` to stop."))
			} else {
				fmt.Println(style.MutedStyle.Render("Press Ctrl+C to stop"))
			}
			fmt.Println()

			// Heartbeat loop: the backend reaper expires tunnels with stale
			// heartbeats so that kill -9 / lost-network cases don't leave zombie
			// rows and dead public URLs behind.
			hbCtx, hbCancel := context.WithCancel(ctx)
			defer hbCancel()
			go func() {
				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-hbCtx.Done():
						return
					case <-ticker.C:
						reqCtx, reqCancel := context.WithTimeout(hbCtx, 10*time.Second)
						if err := app.API.HeartbeatTunnel(reqCtx, tunnel.ID); err != nil {
							logTunnel("[tunnel] heartbeat failed: %v\n", err)
						}
						reqCancel()
					}
				}
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
				fmt.Println(style.Warning.Render(fmt.Sprintf("\nReceived %s, cleaning up tunnel...", sig)))
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
	cmd.Flags().StringVar(&clusterRef, "cluster", "", "target a cluster by name or ID (service proxy via DERP)")
	cmd.Flags().StringVar(&service, "service", "", "Kubernetes service name (required with --cluster)")
	cmd.Flags().StringVar(&namespace, "namespace", "default", "Kubernetes service namespace (default: default)")
	cmd.Flags().BoolVarP(&background, "background", "b", false, "run in background (detached)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose tunnel traffic logging")
	cmd.Flags().StringVar(&scheme, "scheme", "http", "upstream scheme: http or https")
	cmd.Flags().BoolVar(&insecureUpstream, "insecure-upstream", true, "skip TLS verification for https upstream (default true for localhost dev)")

	return cmd
}

// runTunnelExposeBackground spawns a detached child process running tunnel expose.
func runTunnelExposeBackground(port int, name, toPeer string, externalPort int, public, verbose bool, scheme string, insecureUpstream bool) error {
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
	if scheme != "" && scheme != "http" {
		args = append(args, "--scheme", scheme)
	}
	if !insecureUpstream {
		args = append(args, "--insecure-upstream=false")
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
	fmt.Println(style.Success.Copy().Bold(true).Render(fmt.Sprintf("Tunnel running in background (PID: %d)", child.Process.Pid)))
	fmt.Println(style.MutedStyle.Render(fmt.Sprintf("  Log: %s", logPath)))
	fmt.Println(style.MutedStyle.Render(fmt.Sprintf("  Stop: kill %d  or  prysm tunnel delete <id>", child.Process.Pid)))
	fmt.Println()

	return nil
}

func resolveClusterForTunnel(ctx context.Context, app *App, ref string) (*api.Cluster, error) {
	clusters, err := app.API.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	if len(clusters) == 0 {
		return nil, errors.New("no clusters available")
	}

	cluster, err := findCluster(clusters, ref)
	if err == nil {
		return cluster, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%v\nAvailable clusters:\n", err)
	for _, c := range clusters {
		status := c.Status
		if strings.ToLower(status) == "connected" {
			status = style.Success.Render(status)
		} else {
			status = style.Error.Render(status)
		}
		fmt.Fprintf(&b, "  - %d\t%s\t%s\n", c.ID, c.Name, status)
	}
	return nil, errors.New(b.String())
}

// cleanupTunnel deletes the tunnel record on graceful shutdown.
func cleanupTunnel(app *App, tunnelID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.API.DeleteTunnel(ctx, tunnelID); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", style.MutedStyle.Render(fmt.Sprintf("cleanup tunnel %d: %v", tunnelID, err)))
	} else {
		fmt.Println(style.Success.Render("Tunnel deleted."))
	}
}

func runClusterTunnelConnect(ctx context.Context, app *App, match *api.Tunnel, localPort int) error {
	clusterID := strings.TrimPrefix(match.TargetDeviceID, "cluster_")
	if clusterID == "" {
		return fmt.Errorf("invalid cluster tunnel target")
	}
	if match.TargetNamespace == "" || match.TargetService == "" {
		return fmt.Errorf("cluster tunnel missing service or namespace")
	}

	if localPort <= 0 {
		localPort = match.Port
	}

	handler := newClusterTunnelProxyHandler(app, clusterID, match.TargetNamespace, match.TargetService, match.Port)
	addr := fmt.Sprintf("127.0.0.1:%d", localPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	srv := &http.Server{Handler: handler}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()

	fmt.Println(style.Success.Render(fmt.Sprintf("Cluster tunnel ready — http://localhost:%d → %s/%s:%d", localPort, match.TargetNamespace, match.TargetService, match.Port)))
	fmt.Println(style.MutedStyle.Render("Press Ctrl+C to stop"))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	defer cleanupTunnel(app, match.ID)

	select {
	case <-ctx.Done():
	case sig := <-sigCh:
		fmt.Println(style.Warning.Render(fmt.Sprintf("\nReceived %s, stopping cluster tunnel...", sig)))
	case srvErr := <-errCh:
		if srvErr != nil && !errors.Is(srvErr, http.ErrServerClosed) {
			return srvErr
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown proxy: %w", err)
	}
	return nil
}

func newClusterTunnelProxyHandler(app *App, clusterID, namespace, service string, targetPort int) http.Handler {
	endpointBase := fmt.Sprintf("/clusters/%s/proxy/api/v1/namespaces/%s/services/%s:%d/proxy", clusterID, namespace, service, targetPort)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		endpoint := endpointBase
		if r.URL.Path != "" && r.URL.Path != "/" {
			endpoint += r.URL.Path
		} else if !strings.HasSuffix(endpoint, "/") {
			endpoint += "/"
		}
		if rawQuery := r.URL.RawQuery; rawQuery != "" {
			endpoint += "?" + rawQuery
		}

		headers := cloneHeader(r.Header)
		headers.Del("Host")
		headers.Del("Connection")
		resp, err := app.API.DoStream(r.Context(), r.Method, endpoint, headers, r.Body)
		if err != nil {
			status := http.StatusBadGateway
			http.Error(w, fmt.Sprintf("cluster proxy error: %v", err), status)
			return
		}
		defer resp.Body.Close()

		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, resp.Body); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", style.MutedStyle.Render(fmt.Sprintf("proxy copy error: %v", err)))
		}
	})
}

func cloneHeader(src http.Header) http.Header {
	dst := make(http.Header, len(src))
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
	return dst
}

func copyHeaders(dst, src http.Header) {
	for key, vv := range src {
		for _, v := range vv {
			dst.Add(key, v)
		}
	}
}

func newTunnelConnectCommand() *cobra.Command {
	var (
		peerRef    string
		port       int
		localPort  int
		clusterRef string
		tunnelRef  string
		service    string
		namespace  string
	)

	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to a peer's exposed port",
		Long:  "Connect to a peer's exposed port and forward traffic to a local port. Establishes a DERP connection and TCP proxy.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			// Cluster private tunnel mode: connect directly via DERP exit route,
			// no pre-existing tunnel record required.
			if strings.TrimSpace(clusterRef) != "" {
				// --tunnel: resolve named ClusterTunnel record to fill service/namespace/port
				if strings.TrimSpace(tunnelRef) != "" {
					tunnelCtx, tunnelCancel := context.WithTimeout(ctx, 20*time.Second)
					tmpCluster, tmpErr := resolveClusterForTunnel(tunnelCtx, app, clusterRef)
					tunnelCancel()
					if tmpErr != nil {
						return tmpErr
					}
					clusterDeviceID := fmt.Sprintf("cluster_%d", tmpCluster.ID)
					t, tErr := app.API.GetClusterTunnelByName(ctx, clusterDeviceID, tunnelRef)
					if tErr != nil {
						return tErr
					}
					service = t.TargetService
					namespace = t.TargetNamespace
					if namespace == "" {
						namespace = "default"
					}
					port = t.Port
				}

				if strings.TrimSpace(service) == "" {
					return errors.New("--service is required with --cluster (or use --tunnel)")
				}
				if port <= 0 || port > 65535 {
					return errors.New("--port must be between 1-65535")
				}
				if namespace == "" {
					namespace = "default"
				}
				lp := localPort
				if lp <= 0 {
					lp = port
				}

				clusterCtx, clusterCancel := context.WithTimeout(ctx, 20*time.Second)
				cluster, err := resolveClusterForTunnel(clusterCtx, app, clusterRef)
				clusterCancel()
				if err != nil {
					return err
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

				var derpToken string
				if tokResp, tokErr := app.API.GetDERPTunnelToken(ctx, deviceID); tokErr == nil && tokResp != nil && tokResp.Token != "" {
					derpToken = tokResp.Token
				}

				targetDeviceID := fmt.Sprintf("cluster_%d", cluster.ID)
				targetAddress := fmt.Sprintf("%s.%s.svc.cluster.local:%d", service, namespace, port)
				orgID := fmt.Sprintf("%d", sess.Organization.ID)

				routeConns := make(map[string]net.Conn)
				routeConnsMu := sync.RWMutex{}
				pendingRoutes := make(map[string]chan string)
				pendingMu := sync.Mutex{}

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
							conn.Write(data) //nolint:errcheck
						}
					}),
					derp.WithRouteResponseHandler(func(routeID, status string) {
						pendingMu.Lock()
						ch := pendingRoutes[routeID]
						delete(pendingRoutes, routeID)
						pendingMu.Unlock()
						if ch != nil {
							select {
							case ch <- status:
							default:
							}
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

				fmt.Println(style.Success.Render(fmt.Sprintf(
					"Cluster tunnel: %s/%s:%d → localhost:%d", namespace, service, port, lp)))
				fmt.Println(style.MutedStyle.Render(fmt.Sprintf(
					"  Cluster: %s (via DERP exit route)", cluster.Name)))
				fmt.Println(style.MutedStyle.Render("Press Ctrl+C to stop"))
				fmt.Println()

				go func() {
					for {
						conn, acceptErr := listener.Accept()
						if acceptErr != nil {
							return
						}
						go func() {
							routeID, routeErr := client.SendExitRouteRequest(orgID, targetDeviceID, targetAddress)
							if routeErr != nil {
								fmt.Fprintf(os.Stderr, "%s\n", style.Error.Render(fmt.Sprintf("exit route request: %v", routeErr)))
								conn.Close()
								return
							}

							ch := make(chan string, 1)
							pendingMu.Lock()
							pendingRoutes[routeID] = ch
							pendingMu.Unlock()

							select {
							case status := <-ch:
								if status != "ok" {
									fmt.Fprintf(os.Stderr, "%s\n", style.Error.Render(fmt.Sprintf("route rejected: %s", status)))
									conn.Close()
									return
								}
							case <-time.After(15 * time.Second):
								pendingMu.Lock()
								delete(pendingRoutes, routeID)
								pendingMu.Unlock()
								fmt.Fprintf(os.Stderr, "%s\n", style.Error.Render("route request timed out"))
								conn.Close()
								return
							case <-ctx.Done():
								conn.Close()
								return
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
									n, readErr := conn.Read(buf)
									if n > 0 {
										if sendErr := client.SendTrafficData(routeID, buf[:n]); sendErr != nil {
											return
										}
									}
									if readErr != nil {
										if readErr != io.EOF {
											fmt.Fprintf(os.Stderr, "%s\n", style.MutedStyle.Render(fmt.Sprintf("tunnel read: %v", readErr)))
										}
										_ = client.SendTrafficData(routeID, nil)
										return
									}
								}
							}()
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
					fmt.Println(style.Warning.Render(fmt.Sprintf("Received %s, closing tunnel...", sig)))
					client.Close()
					return nil
				case runErr := <-errCh:
					client.Close()
					return runErr
				}
			}

			// Peer tunnel mode (existing)
			if strings.TrimSpace(peerRef) == "" {
				return errors.New("--peer is required (or use --cluster for cluster tunnels)")
			}
			if port <= 0 || port > 65535 {
				return errors.New("--port must be between 1-65535")
			}

			// Look up tunnel from API
			var tunnels []api.Tunnel
			if err := ui.WithSpinner("Connecting to tunnel...", func() error {
				listCtx, listCancel := context.WithTimeout(ctx, 20*time.Second)
				defer listCancel()
				var listErr error
				tunnels, listErr = app.API.ListTunnels(listCtx, peerRef)
				return listErr
			}); err != nil {
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

			if strings.HasPrefix(match.TargetDeviceID, "cluster_") {
				return runClusterTunnelConnect(ctx, app, match, lp)
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
						conn.Write(data) //nolint:errcheck
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

			fmt.Println(style.Success.Render(fmt.Sprintf("Tunnel: %s:%d -> localhost:%d", peerRef, port, lp)))
			fmt.Printf("  Tunnel ID: %d\n", match.ID)
			fmt.Printf("  Connect to localhost:%d to reach %s:%d\n", lp, peerRef, port)

			targetClient := "device_" + peerRef
			if strings.HasPrefix(peerRef, "cluster_") {
				targetClient = peerRef
			}
			orgID := fmt.Sprintf("%d", match.OrganizationID)

			go func() {
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					routeID, err := client.SendRouteRequest(orgID, targetClient, match.ExternalPort, match.Port, "TCP")
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s\n", style.Error.Render(fmt.Sprintf("route request failed: %v", err)))
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
									fmt.Fprintf(os.Stderr, "%s\n", style.MutedStyle.Render(fmt.Sprintf("tunnel read: %v", err)))
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
				fmt.Println(style.Warning.Render(fmt.Sprintf("Received %s, closing tunnel...", sig)))
				client.Close()
				return nil
			case err := <-errCh:
				client.Close()
				return err
			}
		},
	}

	cmd.Flags().StringVar(&peerRef, "peer", "", "peer device ID (from `prysm mesh peers`)")
	cmd.Flags().IntVarP(&port, "port", "p", 0, "port to connect to")
	cmd.Flags().IntVarP(&localPort, "local-port", "l", 0, "local port to bind (default: same as port)")
	cmd.Flags().StringVar(&clusterRef, "cluster", "", "cluster name or ID for private cluster tunnel (via DERP exit route)")
	cmd.Flags().StringVar(&tunnelRef, "tunnel", "", "ClusterTunnel name (resolves service/namespace/port from backend)")
	cmd.Flags().StringVar(&service, "service", "", "Kubernetes service name (required with --cluster)")
	cmd.Flags().StringVar(&namespace, "namespace", "default", "Kubernetes namespace (default: default)")

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
				fmt.Println(style.Warning.Render("No tunnels defined."))
				return nil
			}

			fmt.Printf("%-6s %-12s %-8s %-10s %-10s %-8s %-10s %s\n", "ID", "DEVICE", "PORT", "EXT.PORT", "TO_PEER", "STATUS", "LAST HB", "PUBLIC URL")
			for _, t := range tunnels {
				toPeer := "-"
				if t.ToPeerDeviceID != "" {
					toPeer = t.ToPeerDeviceID
				}
				publicURL := "-"
				if t.IsPublic && t.ExternalURL != "" {
					publicURL = t.ExternalURL
				}
				fmt.Printf("%-6d %-12s %-8d %-10d %-10s %-8s %-10s %s\n",
					t.ID, truncate(t.TargetDeviceID, 12), t.Port, t.ExternalPort, truncate(toPeer, 10), t.Status, formatHeartbeatAge(t.LastHeartbeatAt), publicURL)
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
				var derpErr error
				_ = ui.WithSpinner("Testing DERP connectivity...", func() error {
					runCtx, runCancel := context.WithTimeout(ctx, 15*time.Second)
					derpErr = derpClient.Run(runCtx)
					runCancel()
					derpClient.Close()
					return nil
				})
				if derpErr != nil && !errors.Is(derpErr, context.Canceled) && !errors.Is(derpErr, context.DeadlineExceeded) {
					fmt.Fprintf(os.Stderr, "DERP: FAIL — %v\n", derpErr)
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

			fmt.Println(style.Success.Render(fmt.Sprintf("Tunnel %s deleted", tunnelID)))
			return nil
		},
	}
	return cmd
}
