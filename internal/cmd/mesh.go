package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/derp"
	"github.com/prysmsh/cli/internal/meshd"
	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
	"github.com/prysmsh/cli/internal/util"
	"github.com/prysmsh/cli/internal/wg"
	"github.com/prysmsh/cli/plugins/exit"
	"github.com/prysmsh/cli/plugins/subnet"
)

// routeHostSlug returns a hostname-safe slug from the route name (must match backend routeHostSlug).
func routeHostSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r == ' ' || r == '_' || r == '/' || r == '.':
			if b.Len() > 0 && b.String()[b.Len()-1] != '-' {
				b.WriteByte('-')
			}
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Trim(strings.TrimSpace(b.String()), "-")
}

const derpConnectPidFile = "derp-connect.pid"

var cleanupSubnetStaleRedirects = subnet.CleanupStaleRedirectsForCIDRs

// getPrysmHome returns the Prysm home directory (PRYSM_HOME or $HOME/.prysm).
// Deprecated: use util.PrysmHome() instead.
func getPrysmHome() string {
	return util.PrysmHome()
}

func writeDerpPidfile(home string, pid int) error {
	path := filepath.Join(home, derpConnectPidFile)
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

func removeDerpPidfile(home string) {
	_ = os.Remove(filepath.Join(home, derpConnectPidFile))
}

func newMeshCommand() *cobra.Command {
	meshCmd := &cobra.Command{
		Use:   "mesh",
		Short: "Interact with the DERP mesh network",
	}

	meshCmd.AddCommand(
		newMeshConnectCommand(),
		newMeshDisconnectCommand(),
		newMeshDoctorCommand(),
		newMeshPeersCommand(),
		newMeshRoutesCommand(),
		newCrossClusterRoutesCommand(),
		newMeshExitCommand(),
	)

	return meshCmd
}

// buildCIDRMap builds a map[cidr]exitPeerDeviceID from nodes that are
// exit-enabled, connected, and have AdvertisedCIDRs (cluster nodes only).
// This works with the updated backend that returns advertised_cidrs.
func buildCIDRMap(nodes []api.MeshNode) map[string]string {
	m := make(map[string]string)
	for _, n := range nodes {
		if !n.ExitEnabled || n.ClusterID == nil || n.Status != "connected" {
			continue
		}
		for _, cidr := range n.AdvertisedCIDRs {
			if cidr != "" {
				m[cidr] = n.DeviceID
			}
		}
	}
	return m
}

// buildCIDRMapFromClusters is a fallback for backends that don't yet return
// advertised_cidrs. It uses the clusters API directly: any cluster that is
// marked as an exit router and has a WGOverlayCIDR is included.
// The DeviceID for dialing is taken from the corresponding mesh node, falling
// back to the deterministic "cluster_<id>" pattern used by ensureClusterMeshPeer.
func buildCIDRMapFromClusters(nodes []api.MeshNode, clusters []api.Cluster) map[string]string {
	// Index connected mesh nodes by cluster ID to get their DeviceID.
	deviceByClusterID := make(map[int64]string)
	for _, n := range nodes {
		if n.ClusterID != nil && n.Status == "connected" {
			deviceByClusterID[*n.ClusterID] = n.DeviceID
		}
	}
	m := make(map[string]string)
	for _, cl := range clusters {
		if !cl.IsExitRouter || cl.WGOverlayCIDR == "" {
			continue
		}
		deviceID := deviceByClusterID[cl.ID]
		if deviceID == "" {
			// Deterministic device ID assigned by ensureClusterMeshPeer.
			deviceID = fmt.Sprintf("cluster_%d", cl.ID)
		}
		m[cl.WGOverlayCIDR] = deviceID
	}
	return m
}

func sortedStringMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if strings.TrimSpace(k) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func newMeshConnectCommand() *cobra.Command {
	var foreground bool
	var socks5Port int
	var subnetEnabled bool

	c := &cobra.Command{
		Use:   "connect",
		Short: "Join the DERP mesh network and stream peer updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Delegate to daemon if it's running (no sudo, no background fork).
			if meshd.IsRunning() {
				return runMeshConnectViaDaemon()
			}
			if foreground {
				return runMeshConnect(cmd)
			}
			return runMeshConnectBackground(cmd)
		},
	}
	c.Flags().BoolVarP(&foreground, "foreground", "f", false, "run in foreground (stay in terminal; default is background)")
	c.Flags().IntVar(&socks5Port, "socks5-port", 0, "local port for SOCKS5 proxy to reach mesh routes (0 = disabled)")
	c.Flags().BoolVar(&subnetEnabled, "subnet", true, "inject OS routes for cluster CIDRs (transparent routing; needs root/sudo)")
	c.Flags().Bool("wireguard", true, "enable WireGuard tunnel for direct peer connectivity (requires sudo)")
	return c
}

func newMeshDisconnectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect",
		Short: "Leave the DERP mesh network and stop the background process",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Try daemon first.
			if meshd.IsRunning() {
				resp, err := meshd.Disconnect()
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render(fmt.Sprintf("meshd disconnect: %v", err)))
				} else {
					fmt.Println(style.Success.Render("Disconnected from mesh (via daemon)."))
					if resp.Error != "" {
						fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render(resp.Error))
					}
					return nil
				}
			}

			app := MustApp()
			home := getPrysmHome()

			pid, running := readDerpPidAndCheckRunning()
			if running && pid > 0 {
				proc, err := os.FindProcess(pid)
				if err == nil {
					_ = proc.Signal(syscall.SIGTERM)
					fmt.Println(style.Success.Render(fmt.Sprintf("Sent SIGTERM to DERP process (PID %d)", pid)))
				}
			}
			removeDerpPidfile(home)

			// Best-effort stale subnet cleanup: if a previous process was killed
			// without Stop(), stale REDIRECT rules can remain and break new sessions.
			cleanupCtx, cleanupCancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cleanupCancel()
			if meshNodes, err := app.API.ListMeshNodes(cleanupCtx); err == nil {
				cidrMap := buildCIDRMap(meshNodes)
				if len(cidrMap) == 0 {
					if clusters, clustersErr := app.API.ListClusters(cleanupCtx); clustersErr == nil {
						cidrMap = buildCIDRMapFromClusters(meshNodes, clusters)
					}
				}
				if len(cidrMap) > 0 {
					removed := cleanupSubnetStaleRedirects(sortedStringMapKeys(cidrMap))
					if removed > 0 {
						fmt.Println(style.MutedStyle.Render(fmt.Sprintf("Removed %d stale subnet redirect rule(s).", removed)))
					}
				}
			}

			// Notify backend that this device is disconnected
			deviceID, err := derp.EnsureDeviceID(app.Config.HomeDir)
			if err == nil && deviceID != "" {
				ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
				defer cancel()
				if err := app.API.DisconnectMeshNode(ctx, deviceID); err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render(fmt.Sprintf("Could not notify backend: %v", err)))
				}
			}

			if !running {
				fmt.Println(style.MutedStyle.Render("No active mesh connection found."))
			} else {
				fmt.Println(style.Success.Render("Disconnected from DERP mesh."))
			}
			return nil
		},
	}
}

func newMeshDoctorCommand() *cobra.Command {
	var fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose mesh routing state and stale subnet redirects",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			pid, running := readDerpPidAndCheckRunning()
			if running && pid > 0 {
				fmt.Println(style.Success.Render(fmt.Sprintf("mesh process: running (PID %d)", pid)))
			} else if pid > 0 {
				fmt.Println(style.Warning.Render(fmt.Sprintf("mesh process: stale pidfile (PID %d not running)", pid)))
			} else {
				fmt.Println(style.Warning.Render("mesh process: not running"))
			}

			meshNodes, nodesErr := app.API.ListMeshNodes(ctx)
			if nodesErr != nil {
				fmt.Println(style.Warning.Render(fmt.Sprintf("mesh nodes: lookup failed: %v", nodesErr)))
			}

			exitConnected := 0
			for _, n := range meshNodes {
				if n.ExitEnabled && n.Status == "connected" {
					exitConnected++
				}
			}
			if nodesErr == nil {
				fmt.Println(style.MutedStyle.Render(fmt.Sprintf("mesh nodes: %d total, %d exit-enabled connected", len(meshNodes), exitConnected)))
			}

			cidrMap := buildCIDRMap(meshNodes)
			if len(cidrMap) == 0 {
				if clusters, err := app.API.ListClusters(ctx); err == nil {
					cidrMap = buildCIDRMapFromClusters(meshNodes, clusters)
				}
			}
			cidrs := sortedStringMapKeys(cidrMap)
			if len(cidrs) == 0 {
				fmt.Println(style.Warning.Render("subnet CIDRs: none discovered"))
			} else {
				fmt.Println(style.MutedStyle.Render(fmt.Sprintf("subnet CIDRs: %d discovered", len(cidrs))))
			}

			if fix && len(cidrs) > 0 {
					removed := cleanupSubnetStaleRedirects(cidrs)
					fmt.Println(style.MutedStyle.Render(fmt.Sprintf("subnet stale redirects removed: %d", removed)))
				}

			routes, routesErr := app.API.ListRoutes(ctx, nil)
			if routesErr != nil {
				fmt.Println(style.Warning.Render(fmt.Sprintf("mesh routes: lookup failed: %v", routesErr)))
			} else {
				active := 0
				for _, r := range routes {
					if strings.EqualFold(r.Status, "active") {
						active++
					}
				}
				fmt.Println(style.MutedStyle.Render(fmt.Sprintf("mesh routes: %d total, %d active", len(routes), active)))
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", true, "remove stale subnet REDIRECT rules for discovered mesh CIDRs")
	return cmd
}

func runMeshConnectViaDaemon() error {
	app := MustApp()
	sess, err := app.Sessions.Load()
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("no active session; run `prysm login`")
	}
	deviceID, err := derp.EnsureDeviceID(app.Config.HomeDir)
	if err != nil {
		return err
	}
	relay := app.Config.DERPServerURL
	if relay == "" {
		relay = sess.DERPServerURL
	}
	apiURL := app.Config.APIBaseURL

	resp, err := meshd.Connect(
		sess.Token, apiURL, relay, deviceID, app.Config.HomeDir,
	)
	if err != nil {
		return fmt.Errorf("meshd connect: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("meshd: %s", resp.Error)
	}
	fmt.Println(style.Success.Render(fmt.Sprintf("Mesh connected via daemon (%s on %s)", resp.OverlayIP, resp.Interface)))
	fmt.Println(style.MutedStyle.Render("Daemon manages the tunnel — this CLI can exit safely."))

	// Launch tray app if installed and not already running.
	launchTrayApp()
	return nil
}

func runMeshConnectBackground(cmd *cobra.Command) error {
	if pid, running := readDerpPidAndCheckRunning(); running {
		fmt.Println(style.Warning.Render(fmt.Sprintf("DERP mesh already connected (PID %d).", pid)))
		fmt.Println(style.MutedStyle.Render("Use `prysm mesh disconnect` before starting a new connection."))
		return nil
	} else if pid > 0 {
		// Stale pidfile from a previous crashed/terminated process.
		removeDerpPidfile(getPrysmHome())
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find executable: %w", err)
	}
	home := getPrysmHome()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create prysm home: %w", err)
	}
	logPath := filepath.Join(home, "derp-connect.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	args := []string{"mesh", "connect", "--foreground"}
	if port, _ := cmd.Flags().GetInt("socks5-port"); port > 0 {
		args = append(args, "--socks5-port", strconv.Itoa(port))
	}
	if subnet, _ := cmd.Flags().GetBool("subnet"); !subnet {
		args = append(args, "--subnet=false")
	}
	if wg, _ := cmd.Flags().GetBool("wireguard"); !wg {
		args = append(args, "--wireguard=false")
	}
	child := exec.Command(exe, args...)
	child.Stdin = nil
	child.Stdout = logFile
	child.Stderr = logFile
	child.Env = os.Environ()
	child.Dir = "/"
	if child.SysProcAttr == nil {
		child.SysProcAttr = &syscall.SysProcAttr{}
	}
	setSysProcAttrSetsid(child.SysProcAttr)

	if err := child.Start(); err != nil {
		return fmt.Errorf("start background process: %w", err)
	}
	fmt.Println(style.Success.Render(fmt.Sprintf("DERP mesh running in background (PID %d)", child.Process.Pid)))
	fmt.Println(style.MutedStyle.Render(fmt.Sprintf("Log: %s", logPath)))
	fmt.Println(style.MutedStyle.Render(fmt.Sprintf("Stop: kill %d", child.Process.Pid)))
	_ = child.Process.Release()
	return nil
}

func runMeshConnect(cmd *cobra.Command) error {
	home := getPrysmHome()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create prysm home: %w", err)
	}
	if pid, running := readDerpPidAndCheckRunning(); running && pid != os.Getpid() {
		return fmt.Errorf("DERP mesh already connected (PID %d); run `prysm mesh disconnect` first", pid)
	} else if pid > 0 && !running {
		// Stale pidfile from a previous crashed/terminated process.
		removeDerpPidfile(home)
	}
	if err := writeDerpPidfile(home, os.Getpid()); err != nil {
		return fmt.Errorf("write DERP pidfile: %w", err)
	}
	defer removeDerpPidfile(home)

	app := MustApp()
	sess, err := app.Sessions.Load()
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("no active session; run `prysm login`")
	}

	// Config takes priority (includes CLI flag overrides), then session, then default
	relay := app.Config.DERPServerURL
	if relay == "" {
		relay = sess.DERPServerURL
	}
	if relay == "" {
		return fmt.Errorf("DERP relay URL not configured")
	}

	deviceID, err := derp.EnsureDeviceID(app.Config.HomeDir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	capabilities := map[string]interface{}{
		"platform":   "cli",
		"features":   []string{"service_discovery", "health_check"},
		"registered": time.Now().UTC().Format(time.RFC3339),
	}

	if err := ui.WithSpinner("Connecting to mesh...", func() error {
		registerPayload := map[string]interface{}{
			"device_id":    deviceID,
			"peer_type":    "client",
			"status":       "connected",
			"capabilities": capabilities,
		}

		if _, err := app.API.RegisterMeshNode(ctx, registerPayload); err != nil {
			return fmt.Errorf("register mesh node: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	wgEnabled, _ := cmd.Flags().GetBool("wireguard")

	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+sess.Token)
	headers.Set("X-Session-ID", sess.SessionID)
	headers.Set("X-Org-ID", fmt.Sprintf("%d", sess.Organization.ID))

	// Tunnel traffic: routeID -> local conn for exposed ports
	routeConns := make(map[string]net.Conn)
	routeConnsMu := sync.RWMutex{}
	var derpClient *derp.Client

	derpOpts := []derp.Option{
		derp.WithHeaders(headers),
		derp.WithCapabilities(capabilities),
		derp.WithInsecure(app.InsecureTLS),
		derp.WithTunnelTrafficHandler(func(routeID string, targetPort, _ int, data []byte) {
			if data != nil {
				// traffic_data: forward to local conn
				routeConnsMu.RLock()
				conn := routeConns[routeID]
				routeConnsMu.RUnlock()
				if conn != nil {
					conn.Write(data)
				}
				return
			}
			// route_setup: dial localhost:targetPort and start forwarding
			addr := fmt.Sprintf("127.0.0.1:%d", targetPort)
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", style.Error.Render(fmt.Sprintf("tunnel dial %s: %v", addr, err)))
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
					n, err := conn.Read(buf)
					if n > 0 {
						if sendErr := derpClient.SendTrafficData(routeID, buf[:n]); sendErr != nil {
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
		}),
	}
	// Use session token for DERP registration in mesh connect for compatibility
	// with relay deployments that reject derp_tunnel_token.
	derpOpts = append(derpOpts, derp.WithSessionToken(sess.Token))
	derpClient = derp.NewClient(relay, deviceID, derpOpts...)
	client := derpClient

	// WireGuard mesh tunnel: register key, get overlay IP, bring up interface.
	// Uses DERP as transport — WireGuard packets flow through the DERP WebSocket relay.
	var wgTunnel *wg.Tunnel
	if wgEnabled {
		tun, bind, wgErr := wg.SetupMeshWireGuardDERP(ctx, app.API, app.Config.HomeDir, deviceID, derpClient)
		if wgErr != nil {
			fmt.Println(style.Warning.Render(fmt.Sprintf("WireGuard tunnel disabled: %v", wgErr)))
		} else {
			wgTunnel = tun
			defer wgTunnel.Stop()
			// Wire inbound WireGuard packets from DERP to the bind.
			derpClient.WGPacketHandler = func(fromPeerID string, packet []byte) {
				bind.DeliverPacket(fromPeerID, packet)
			}
			fmt.Println(style.Success.Render(fmt.Sprintf("WireGuard tunnel active (%s on %s) via DERP", wgTunnel.OverlayIP(), wgTunnel.InterfaceName())))
		}
	}
	// After DERP connects, re-trigger WG handshake for peers that were added
	// before the DERP WebSocket was live.
	if wgTunnel != nil {
		derpClient.OnConnected = func() {
			time.Sleep(500 * time.Millisecond)
			for _, p := range wgTunnel.Peers() {
				if err := wgTunnel.RetriggerHandshake(p); err != nil {
					fmt.Fprintf(os.Stderr, "wireguard: retrigger handshake %s: %v\n", p.PublicKey[:8], err)
				}
			}
		}
	}

	socks5Port, _ := cmd.Flags().GetInt("socks5-port")
	subnetEnabled, _ := cmd.Flags().GetBool("subnet")
	orgID := fmt.Sprintf("%d", sess.Organization.ID)

	// List mesh nodes when SOCKS5 or subnet routing needs exit peers.
	var meshNodes []api.MeshNode
	var meshListErr error
	if socks5Port > 0 || subnetEnabled {
		meshNodes, meshListErr = app.API.ListMeshNodes(ctx)
	}

	// Build an exit proxy when we have exit-enabled peers. The proxy handles
	// route_response messages from DERP and exposes DialViaDERP for the
	// subnet router so raw TUN traffic can bypass SOCKS5.
	var exitProxy *exit.ExitProxy
	if meshListErr == nil {
		var defaultExitPeer string
		for _, n := range meshNodes {
			if n.ExitEnabled && n.Status == "connected" {
				defaultExitPeer = n.DeviceID
				break
			}
		}
		if defaultExitPeer != "" {
			proxy := exit.NewExitProxy(exit.ProxyOptions{
				ListenAddr: fmt.Sprintf("127.0.0.1:%d", socks5Port),
				ExitPeerID: defaultExitPeer,
				OrgID:      orgID,
				DERPClient: derpClient,
				ResolveExitPeer: func(ctx context.Context, targetAddress string) (string, error) {
					host, _, err := net.SplitHostPort(targetAddress)
					if err != nil {
						return "", err
					}
					listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()

					var clusterID int64
					if strings.HasSuffix(host, ".mesh") {
						// <route-slug>.<cluster-slug>.mesh — resolve by cluster slug
						parts := strings.Split(strings.TrimSuffix(host, ".mesh"), ".")
						if len(parts) == 2 {
							clusterSlug := parts[1]
							clusters, err := app.API.ListClusters(listCtx)
							if err != nil {
								return "", err
							}
							for _, c := range clusters {
								if routeHostSlug(c.Name) == clusterSlug {
									clusterID = c.ID
									break
								}
							}
						}
					}
					if clusterID == 0 {
						return "", nil
					}
					nodes, err := app.API.ListMeshNodes(listCtx)
					if err != nil {
						return "", err
					}
					for _, n := range nodes {
						if n.ClusterID != nil && *n.ClusterID == clusterID && n.ExitEnabled && n.Status == "connected" {
							return n.DeviceID, nil
						}
					}
					return "", nil
				},
			})
			exitProxy = proxy
			derpClient.RouteResponseHandler = proxy.HandleRouteResponse
			origTunnel := derpClient.TunnelTrafficHandler
			derpClient.TunnelTrafficHandler = func(routeID string, targetPort, externalPort int, data []byte) {
				if data != nil {
					proxy.HandleTrafficData(routeID, data)
				}
				if origTunnel != nil {
					origTunnel(routeID, targetPort, externalPort, data)
				}
			}
			if socks5Port > 0 {
				go func() {
					_ = proxy.ListenAndServe(ctx)
				}()
				fmt.Println(style.Success.Render(fmt.Sprintf("SOCKS5 proxy for routes: 127.0.0.1:%d", socks5Port)))
			}
		} else if socks5Port > 0 {
			fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render("SOCKS5 proxy disabled: no exit-enabled connected mesh peer found."))
		}
	} else if socks5Port > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render(fmt.Sprintf("SOCKS5 proxy disabled: %v", meshListErr)))
	}

	// Subnet routing: iptables REDIRECT rules intercept TCP to cluster CIDRs
	// and forward each connection transparently over DERP → agent.
	if subnetEnabled {
		if exitProxy == nil {
			fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render("subnet router: no exit-enabled connected cluster peer found"))
		} else {
			meshBindings, err := buildMeshRouteBindings(ctx, app, meshNodes)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render(fmt.Sprintf("mesh route discovery failed: %v", err)))
			}

			// On macOS, provide transparent .mesh access without SOCKS by
			// running local listeners on route external ports and resolving
			// route hosts to 127.0.0.1 via split DNS.
			if runtime.GOOS == "darwin" && len(meshBindings) > 0 {
				stopProxy, err := startMeshRouteProxy(exitProxy, meshBindings)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render(fmt.Sprintf("mesh local route proxy disabled: %v", err)))
				} else if stopProxy != nil {
					defer stopProxy()
					fmt.Println(style.Success.Render("mesh local route proxy: enabled"))
				}
			}

			var cidrByCluster map[int64]string
			if runtime.GOOS != "darwin" {
				cidrByCluster, _ = clusterCIDRMap(ctx, app, meshNodes)
			}
			hostToIP := buildMeshRouteHostIPs(meshBindings, cidrByCluster)
			if len(hostToIP) > 0 {
				stopDNS, err := startMeshSplitDNS(hostToIP)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render(fmt.Sprintf("mesh split DNS disabled: %v", err)))
				} else if stopDNS != nil {
					defer stopDNS()
					fmt.Println(style.Success.Render("mesh split DNS: *.mesh -> local resolver enabled"))
				}
			}

			cidrMap := buildCIDRMap(meshNodes)
			if len(cidrMap) == 0 {
				// Fallback: backend may not yet return advertised_cidrs — derive
				// CIDRs from the clusters API (WGOverlayCIDR of exit-enabled clusters).
				if clusters, err := app.API.ListClusters(ctx); err == nil {
					cidrMap = buildCIDRMapFromClusters(meshNodes, clusters)
				}
			}
			if len(cidrMap) == 0 {
				fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render("subnet router: no cluster CIDRs found (no WGOverlayCIDR on exit clusters?)"))
			} else {
				localProxy := exitProxy // capture for the dial closure
				targetByPeerAndPort := buildRouteTargetByPeerAndPort(meshBindings)
				dialFn := func(dialCtx context.Context, _ string, addr string) (net.Conn, error) {
					host, portStr, err := net.SplitHostPort(addr)
					if err != nil {
						return nil, fmt.Errorf("invalid addr %s: %w", addr, err)
					}
					port, err := strconv.Atoi(portStr)
					if err != nil || port <= 0 || port > 65535 {
						return nil, fmt.Errorf("invalid destination port %q", portStr)
					}
					ip := net.ParseIP(host)
					if ip == nil {
						return nil, fmt.Errorf("invalid IP %s", host)
					}
					peer := subnet.MatchCIDR(cidrMap, ip)
					if peer == "" {
						return nil, fmt.Errorf("no exit peer for %s", addr)
					}
					target, ok := routeTargetForPeerPort(targetByPeerAndPort, peer, port)
					if !ok {
						return nil, fmt.Errorf("destination port %d is not published by exit peer %s", port, peer)
					}
					// Subnet interception gives us a synthetic service-CIDR IP (e.g. 10.233.x.x).
					// Translate back to the concrete mesh route target host:port before DERP dial.
					return localProxy.DialViaDERP(dialCtx, peer, target)
				}
				bypassCIDRs := controlPlaneBypassCIDRs(ctx, relay, app.Config.APIBaseURL)
				sr := subnet.NewWithBypass(cidrMap, dialFn, bypassCIDRs)
				if err := sr.Start(ctx); err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", style.Warning.Render(fmt.Sprintf("subnet router disabled (need root): %v", err)))
				} else {
					defer sr.Stop()
					for _, b := range bypassCIDRs {
						fmt.Println(style.MutedStyle.Render(fmt.Sprintf("subnet bypass: %s", b)))
					}
					for cidr, peer := range cidrMap {
						fmt.Println(style.Success.Render(fmt.Sprintf("subnet router: %s → %s", cidr, peer)))
					}
				}
			}
		}
	}

	fmt.Println(style.Success.Render(fmt.Sprintf("🔌 Joining DERP mesh as %s", deviceID)))
	fmt.Println(style.MutedStyle.Render(fmt.Sprintf("Relay: %s", relay)))

	// Keepalive: ping backend every 60s so UI shows connected; when we stop, backend marks disconnected
	pingTicker := time.NewTicker(60 * time.Second)
	defer pingTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pingTicker.C:
				pingCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				if err := app.API.PingMeshNode(pingCtx, deviceID); err != nil {
					// Log but don't fail - network may be transient
					msg := fmt.Sprintf("mesh ping: %v", err)
					if strings.Contains(err.Error(), "Invalid token") || strings.Contains(err.Error(), "401") {
						msg += " — run `prysm login` then `prysm mesh disconnect` and `prysm mesh connect` to use a fresh session"
					}
					fmt.Fprintf(os.Stderr, "%s\n", style.MutedStyle.Render(msg))
				}
				cancel()
			}
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
		fmt.Println(style.Warning.Render(fmt.Sprintf("Received %s, disconnecting...", sig)))
		client.Close()
		return nil
	case err := <-errCh:
		client.Close()
		return err
	}
}

func newMeshPeersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "peers",
		Short: "List mesh peers visible to your organization",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			nodes, err := app.API.ListMeshNodes(ctx)
			if err != nil {
				return err
			}

			// Include clusters as mesh peers (cluster agents may or may not be in mesh nodes)
			clusters, _ := app.API.ListClusters(ctx)
			rows := meshNodesToRows(nodes)
			clusterIDsInMesh := make(map[int64]bool)
			for _, n := range nodes {
				if n.ClusterID != nil {
					clusterIDsInMesh[*n.ClusterID] = true
				}
			}
			for _, c := range clusters {
				if clusterIDsInMesh[c.ID] {
					continue
				}
				lastPing := "-"
				if c.LastPing != nil {
					lastPing = c.LastPing.Format(time.RFC3339)
				}
				exit := "-"
				if c.IsExitRouter {
					exit = "yes"
				}
				rows = append(rows, meshPeerRow{
					DeviceID: c.Name,
					PeerType: "cluster",
					Status:   c.Status,
					LastPing: lastPing,
					Exit:     exit,
				})
			}

			if len(rows) == 0 {
				fmt.Println(style.Warning.Render("No mesh peers registered for your organization."))
				return nil
			}

			renderMeshPeerRows(rows)
			return nil
		},
	}
}

// controlPlaneBypassCIDRs resolves DERP/API hosts and returns /32 CIDRs that
// must never be redirected through exit routing.
func controlPlaneBypassCIDRs(ctx context.Context, relayURL, apiBaseURL string) []string {
	hosts := []string{}
	if h := hostFromURL(relayURL); h != "" {
		hosts = append(hosts, h)
	}
	if h := hostFromURL(apiBaseURL); h != "" {
		hosts = append(hosts, h)
	}
	uniqHosts := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		uniqHosts[h] = struct{}{}
	}
	out := []string{}
	seen := map[string]struct{}{}
	for h := range uniqHosts {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", h)
		if err != nil {
			continue
		}
		for _, ip := range ips {
			v4 := ip.To4()
			if v4 == nil {
				continue
			}
			cidr := v4.String() + "/32"
			if _, ok := seen[cidr]; ok {
				continue
			}
			seen[cidr] = struct{}{}
			out = append(out, cidr)
		}
	}
	return out
}

func hostFromURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	u, err := neturl.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func clusterCIDRMap(ctx context.Context, app *App, meshNodes []api.MeshNode) (map[int64]string, error) {
	cidrMap := buildCIDRMap(meshNodes)
	if len(cidrMap) == 0 {
		clusters, err := app.API.ListClusters(ctx)
		if err != nil {
			return nil, err
		}
		cidrMap = buildCIDRMapFromClusters(meshNodes, clusters)
	}

	peerCIDR := map[string]string{}
	for cidr, peer := range cidrMap {
		// Prefer specific prefixes over defaults.
		if existing, ok := peerCIDR[peer]; ok {
			if isDefaultCIDR(existing) && !isDefaultCIDR(cidr) {
				peerCIDR[peer] = cidr
			}
			continue
		}
		peerCIDR[peer] = cidr
	}

	clusterCIDR := map[int64]string{}
	for _, n := range meshNodes {
		if n.ClusterID == nil {
			continue
		}
		if cidr, ok := peerCIDR[n.DeviceID]; ok {
			clusterCIDR[*n.ClusterID] = cidr
		}
	}
	return clusterCIDR, nil
}

func isDefaultCIDR(cidr string) bool {
	return cidr == "0.0.0.0/0" || cidr == "::/0"
}

func pickCIDRIPv4(cidr string) net.IP {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil
	}
	base := ip.To4()
	if base == nil {
		return nil
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 {
		return nil
	}
	if ones == 32 {
		return base
	}
	// Return first host address in subnet.
	out := make(net.IP, len(base))
	copy(out, base)
	out[3]++
	return out
}

type meshRouteBinding struct {
	Host     string
	Port     int
	PeerID   string
	Target   string
	Cluster  int64
	RouteID  int64
	RouteRaw api.Route
}

func buildMeshRouteBindings(ctx context.Context, app *App, meshNodes []api.MeshNode) ([]meshRouteBinding, error) {
	routes, err := app.API.ListRoutes(ctx, nil)
	if err != nil {
		return nil, err
	}
	clusters, err := app.API.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	clusterNameByID := map[int64]string{}
	for _, c := range clusters {
		clusterNameByID[c.ID] = c.Name
	}

	peerByCluster := map[int64]string{}
	for _, n := range meshNodes {
		if n.ClusterID != nil && n.ExitEnabled && n.Status == "connected" {
			peerByCluster[*n.ClusterID] = n.DeviceID
		}
	}

	bindings := make([]meshRouteBinding, 0, len(routes))
	for _, r := range routes {
		clusterName := ""
		if r.Cluster != nil && strings.TrimSpace(r.Cluster.Name) != "" {
			clusterName = r.Cluster.Name
		} else {
			clusterName = clusterNameByID[r.ClusterID]
		}
		routeSlug := routeHostSlug(r.Name)
		clusterSlug := routeHostSlug(clusterName)
		if routeSlug == "" || clusterSlug == "" {
			continue
		}
		host := strings.ToLower(fmt.Sprintf("%s.%s.mesh", routeSlug, clusterSlug))
		peerID := peerByCluster[r.ClusterID]
		if peerID == "" {
			continue
		}
		target := host
		if r.ExternalPort > 0 {
			target = fmt.Sprintf("%s:%d", host, r.ExternalPort)
		}
		bindings = append(bindings, meshRouteBinding{
			Host:     host,
			Port:     r.ExternalPort,
			PeerID:   peerID,
			Target:   target,
			Cluster:  r.ClusterID,
			RouteID:  r.ID,
			RouteRaw: r,
		})
	}
	return bindings, nil
}

func buildMeshRouteHostIPs(bindings []meshRouteBinding, cidrByCluster map[int64]string) map[string]net.IP {
	out := map[string]net.IP{}
	for _, b := range bindings {
		if b.Host == "" {
			continue
		}
		if runtime.GOOS == "darwin" {
			out[b.Host] = net.ParseIP("127.0.0.1")
			continue
		}
		cidr := cidrByCluster[b.Cluster]
		if cidr == "" {
			continue
		}
		ip := pickCIDRIPv4(cidr)
		if ip == nil {
			continue
		}
		if _, exists := out[b.Host]; !exists {
			out[b.Host] = ip
		}
	}
	return out
}

func buildRouteTargetByPeerAndPort(bindings []meshRouteBinding) map[string]map[int]string {
	targetByPeer := make(map[string]map[int]string)
	for _, b := range bindings {
		if b.PeerID == "" || b.Port <= 0 || strings.TrimSpace(b.Target) == "" {
			continue
		}
		if targetByPeer[b.PeerID] == nil {
			targetByPeer[b.PeerID] = make(map[int]string)
		}
		targetByPeer[b.PeerID][b.Port] = b.Target
	}
	return targetByPeer
}

func routeTargetForPeerPort(targetByPeer map[string]map[int]string, peerID string, port int) (string, bool) {
	if port <= 0 {
		return "", false
	}
	ports := targetByPeer[peerID]
	if len(ports) == 0 {
		return "", false
	}
	target, ok := ports[port]
	return target, ok
}

func startMeshRouteProxy(proxy *exit.ExitProxy, bindings []meshRouteBinding) (func(), error) {
	if proxy == nil || len(bindings) == 0 {
		return nil, nil
	}
	// Route external ports are org-scoped unique in backend, but de-dupe defensively.
	byPort := map[int]meshRouteBinding{}
	for _, b := range bindings {
		if b.Port <= 0 || b.PeerID == "" || b.Target == "" {
			continue
		}
		if _, exists := byPort[b.Port]; !exists {
			byPort[b.Port] = b
		}
	}
	if len(byPort) == 0 {
		return nil, nil
	}

	type lnPair struct {
		ln net.Listener
		b  meshRouteBinding
	}
	var listeners []lnPair
	for port, b := range byPort {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			for _, p := range listeners {
				_ = p.ln.Close()
			}
			return nil, fmt.Errorf("listen route port %d: %w", port, err)
		}
		listeners = append(listeners, lnPair{ln: ln, b: b})
		go func(l net.Listener, binding meshRouteBinding) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					remote, err := proxy.DialViaDERP(ctx, binding.PeerID, binding.Target)
					if err != nil {
						return
					}
					defer remote.Close()
					bridgeConns(c, remote)
				}(conn)
			}
		}(ln, b)
	}

	return func() {
		for _, p := range listeners {
			_ = p.ln.Close()
		}
	}, nil
}

func bridgeConns(a, b net.Conn) {
	done := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	_, _ = io.Copy(b, a)
	<-done
}
