package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/derp"
	"github.com/warp-run/prysm-cli/internal/util"
)

const derpConnectPidFile = "derp-connect.pid"

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
		newMeshPeersCommand(),
		newMeshRoutesCommand(),
		newMeshExitCommand(),
	)

	return meshCmd
}

func newMeshConnectCommand() *cobra.Command {
	var foreground bool

	c := &cobra.Command{
		Use:   "connect",
		Short: "Join the DERP mesh network and stream peer updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			if foreground {
				return runMeshConnect(cmd)
			}
			return runMeshConnectBackground(cmd)
		},
	}
	c.Flags().BoolVarP(&foreground, "foreground", "f", false, "run in foreground (stay in terminal; default is background)")
	return c
}

func runMeshConnectBackground(cmd *cobra.Command) error {
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

	child := exec.Command(exe, "mesh", "connect", "--foreground")
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
	color.New(color.FgGreen).Printf("DERP mesh running in background (PID %d)\n", child.Process.Pid)
	color.New(color.FgHiBlack).Printf("Log: %s\n", logPath)
	color.New(color.FgHiBlack).Printf("Stop: kill %d\n", child.Process.Pid)
	_ = child.Process.Release()
	return nil
}

func runMeshConnect(cmd *cobra.Command) error {
	home := getPrysmHome()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create prysm home: %w", err)
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

			// Prefer signed DERP tunnel token (org binding cryptographically enforced)
			var derpToken string
			if tokResp, err := app.API.GetDERPTunnelToken(ctx, deviceID); err == nil && tokResp != nil && tokResp.Token != "" {
				derpToken = tokResp.Token
			}

			capabilities := map[string]interface{}{
				"platform":   "cli",
				"features":   []string{"service_discovery", "health_check"},
				"registered": time.Now().UTC().Format(time.RFC3339),
			}

			registerPayload := map[string]interface{}{
				"device_id":    deviceID,
				"peer_type":    "client",
				"status":       "connected",
				"capabilities": capabilities,
			}

			if _, err := app.API.RegisterMeshNode(ctx, registerPayload); err != nil {
				return fmt.Errorf("register mesh node: %w", err)
			}

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
						color.New(color.FgRed).Fprintf(os.Stderr, "tunnel dial %s: %v\n", addr, err)
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
									color.New(color.FgHiBlack).Fprintf(os.Stderr, "tunnel read: %v\n", err)
								}
								return
							}
						}
					}()
				}),
			}
			if derpToken != "" {
				derpOpts = append(derpOpts, derp.WithDERPTunnelToken(derpToken))
			} else {
				derpOpts = append(derpOpts, derp.WithSessionToken(sess.Token))
			}
			derpClient = derp.NewClient(relay, deviceID, derpOpts...)
			client := derpClient

			color.New(color.FgGreen).Printf("🔌 Joining DERP mesh as %s\n", deviceID)
			color.New(color.FgHiBlack).Printf("Relay: %s\n", relay)

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
							color.New(color.FgHiBlack).Fprintf(os.Stderr, "mesh ping: %v\n", err)
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
				color.New(color.FgYellow).Printf("Received %s, disconnecting...\n", sig)
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
				color.New(color.FgYellow).Println("No mesh peers registered for your organization.")
				return nil
			}

			renderMeshPeerRows(rows)
			return nil
		},
	}
}
