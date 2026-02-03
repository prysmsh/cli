package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/derp"
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
	)

	return tunnelCmd
}

func newTunnelExposeCommand() *cobra.Command {
	var (
		port         int
		name         string
		toPeer       string
		externalPort int
	)

	cmd := &cobra.Command{
		Use:   "expose",
		Short: "Expose a local port to authenticated mesh peers",
		Long:  "Expose a local port so other authenticated peers can connect via the mesh. Requires `prysm mesh connect` to be running.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if port <= 0 || port > 65535 {
				return errors.New("port must be between 1-65535")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			deviceID, err := derp.EnsureDeviceID(app.Config.HomeDir)
			if err != nil {
				return fmt.Errorf("ensure device id: %w", err)
			}

			req := api.TunnelCreateRequest{
				Port:           port,
				Name:           strings.TrimSpace(name),
				TargetDeviceID: deviceID,
				ToPeerDeviceID: strings.TrimSpace(toPeer),
				ExternalPort:   externalPort,
				Protocol:       "tcp",
			}

			tunnel, err := app.API.CreateTunnel(ctx, req)
			if err != nil {
				return err
			}

			color.New(color.FgGreen).Printf("Tunnel created: port %d exposed as device %s\n", port, deviceID)
			fmt.Printf("  ID: %d\n", tunnel.ID)
			fmt.Printf("  External port: %d\n", tunnel.ExternalPort)
			fmt.Printf("  Status: %s\n", tunnel.Status)
			if tunnel.ToPeerDeviceID != "" {
				fmt.Printf("  Restricted to peer: %s\n", tunnel.ToPeerDeviceID)
			}
			color.New(color.FgHiBlack).Printf("\nPeers can connect with: prysm tunnel connect --peer %s --port %d\n", deviceID, port)
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "local port to expose")
	cmd.Flags().StringVar(&name, "name", "", "optional tunnel name")
	cmd.Flags().StringVar(&toPeer, "to-peer", "", "restrict access to specific peer device ID")
	cmd.Flags().IntVar(&externalPort, "external-port", 0, "external port (auto-allocated if omitted)")
	_ = cmd.MarkFlagRequired("port")

	return cmd
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

			fmt.Printf("%-6s %-12s %-8s %-10s %-10s %-8s\n", "ID", "DEVICE", "PORT", "EXT.PORT", "TO_PEER", "STATUS")
			for _, t := range tunnels {
				toPeer := "-"
				if t.ToPeerDeviceID != "" {
					toPeer = t.ToPeerDeviceID
				}
				fmt.Printf("%-6d %-12s %-8d %-10d %-10s %-8s\n",
					t.ID, truncate(t.TargetDeviceID, 12), t.Port, t.ExternalPort, truncate(toPeer, 10), t.Status)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&deviceFilter, "device", "", "filter by target device ID")
	return cmd
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

			if err := app.API.DeleteTunnelByID(ctx, args[0]); err != nil {
				return err
			}

			color.New(color.FgGreen).Printf("Tunnel %s deleted\n", args[0])
			return nil
		},
	}
	return cmd
}
