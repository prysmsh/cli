// Package exit implements the builtin "exit" plugin that starts a local SOCKS5
// proxy and tunnels connections through a DERP exit peer.
package exit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prysmsh/cli/internal/derp"
	"github.com/prysmsh/cli/internal/plugin"
)

// ExitPlugin is a builtin plugin that provides exit node proxy commands.
type ExitPlugin struct {
	host plugin.HostServices
}

// New creates a new exit plugin with the given host services.
// Pass nil for host if registering commands eagerly; call SetHost before Execute.
func New(host plugin.HostServices) *ExitPlugin {
	return &ExitPlugin{host: host}
}

// SetHost sets (or replaces) the host services used by this plugin.
func (p *ExitPlugin) SetHost(host plugin.HostServices) {
	p.host = host
}

// Manifest returns the plugin's metadata and command tree.
func (p *ExitPlugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:        "exit-proxy",
		Version:     "0.1.0",
		Description: "SOCKS5 proxy through DERP exit peers",
		Commands: []plugin.CommandSpec{
			{
				Name:               "use",
				Short:              "Start SOCKS5 proxy through an exit peer",
				DisableFlagParsing: true,
			},
			{
				Name:  "off",
				Short: "Stop the exit proxy",
			},
			{
				Name:               "status",
				Short:              "Show exit proxy status",
				DisableFlagParsing: true,
			},
		},
	}
}

// Execute dispatches the command to the appropriate handler.
func (p *ExitPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	if len(req.Args) == 0 {
		return plugin.ExecuteResponse{Error: "subcommand required: use, off, status"}
	}

	switch req.Args[0] {
	case "use":
		return p.execUse(ctx, req)
	case "off":
		return p.execOff(ctx, req)
	case "status":
		return p.execStatus(ctx, req)
	default:
		return plugin.ExecuteResponse{Error: fmt.Sprintf("unknown subcommand: %s", req.Args[0])}
	}
}

// errResp logs an error via host services and returns an ExecuteResponse with exit code 1.
func (p *ExitPlugin) errResp(ctx context.Context, msg string) plugin.ExecuteResponse {
	_ = p.host.Log(ctx, plugin.LogLevelError, msg)
	return plugin.ExecuteResponse{ExitCode: 1}
}

// execUse starts the SOCKS5 proxy through an exit peer.
func (p *ExitPlugin) execUse(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	// Parse args: [use] [peer] [--port 1080]
	var peerArg string
	port := 1080
	args := req.Args[1:] // skip "use"
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--port" && i+1 < len(args):
			i++
			parsedPort, err := strconv.Atoi(args[i])
			if err != nil {
				return p.errResp(ctx, fmt.Sprintf("Invalid port %q — must be a number", args[i]))
			}
			port = parsedPort
		case !strings.HasPrefix(args[i], "-"):
			peerArg = args[i]
		}
	}

	auth, err := p.host.GetAuthContext(ctx)
	if err != nil {
		return p.errResp(ctx, "Not authenticated — run `prysm login` first")
	}

	cfg, err := p.host.GetConfig(ctx)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("Failed to load config: %v", err))
	}

	// Query exit-enabled peers via API.
	statusCode, body, err := p.host.APIRequest(ctx, "GET", "/mesh/nodes", nil)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("Failed to list mesh nodes: %v", err))
	}
	if statusCode >= 400 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = fmt.Sprintf("API returned %d", statusCode)
		}
		if strings.Contains(strings.ToLower(msg), "invalid token") || strings.Contains(strings.ToLower(msg), "expired") || strings.Contains(strings.ToLower(msg), "unauthorized") {
			_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
			return p.errResp(ctx, "Session expired or invalid token. Run: prysm login")
		}
		return p.errResp(ctx, fmt.Sprintf("API error: %s", msg))
	}
	var nodesResp struct {
		Nodes []struct {
			DeviceID     string `json:"device_id"`
			ExitEnabled  bool   `json:"exit_enabled"`
			ExitPriority int    `json:"exit_priority"`
			Status       string `json:"status"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &nodesResp); err != nil {
		msg := strings.TrimSpace(string(body))
		if strings.Contains(strings.ToLower(msg), "invalid token") || strings.Contains(strings.ToLower(msg), "expired") || strings.Contains(strings.ToLower(msg), "unauthorized") {
			return p.errResp(ctx, "Session expired or invalid token. Run: prysm login")
		}
		return p.errResp(ctx, fmt.Sprintf("Failed to parse mesh node response: %v", err))
	}

	// Filter exit-enabled and connected peers.
	type exitPeer struct {
		DeviceID string
		Priority int
	}
	var exitPeers []exitPeer
	for _, n := range nodesResp.Nodes {
		if n.ExitEnabled && n.Status == "connected" {
			exitPeers = append(exitPeers, exitPeer{DeviceID: n.DeviceID, Priority: n.ExitPriority})
		}
	}
	if len(exitPeers) == 0 {
		_ = p.host.Log(ctx, plugin.LogLevelError, "No exit-enabled peers are online")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "To use an exit node, first enable one:")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "  prysm mesh exit enable <device-id>")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "Then verify it's connected:")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "  prysm mesh peers")
		return plugin.ExecuteResponse{ExitCode: 1}
	}

	// Sort by priority descending (higher priority first).
	sort.Slice(exitPeers, func(i, j int) bool {
		return exitPeers[i].Priority > exitPeers[j].Priority
	})

	// Select peer by arg or auto by highest priority.
	var selectedPeer string
	if peerArg != "" {
		found := false
		for _, ep := range exitPeers {
			if ep.DeviceID == peerArg {
				selectedPeer = ep.DeviceID
				found = true
				break
			}
		}
		if !found {
			_ = p.host.Log(ctx, plugin.LogLevelError, fmt.Sprintf("Peer %q is not an exit-enabled connected peer", peerArg))
			_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
			_ = p.host.Log(ctx, plugin.LogLevelPlain, "Available exit peers:")
			for _, ep := range exitPeers {
				_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  - %s (priority %d)", ep.DeviceID, ep.Priority))
			}
			_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
			_ = p.host.Log(ctx, plugin.LogLevelPlain, "Usage: prysm mesh exit use [peer] [--port 1080]")
			return plugin.ExecuteResponse{ExitCode: 1}
		}
	} else {
		selectedPeer = exitPeers[0].DeviceID
	}

	_ = p.host.Log(ctx, plugin.LogLevelInfo, fmt.Sprintf("Using exit peer: %s", selectedPeer))

	// Get DERP connection details.
	derpURL := cfg.DERPURL
	if derpURL == "" {
		return p.errResp(ctx, "DERP relay URL not configured — check your config or session")
	}

	// Get device ID.
	deviceID, err := derp.EnsureDeviceID(cfg.HomeDir)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("Failed to get device ID: %v", err))
	}

	// Get DERP tunnel token.
	var derpToken string
	_, tokenBody, tokenErr := p.host.APIRequest(ctx, "POST", "/mesh/derp-token", []byte(fmt.Sprintf(`{"device_id":"%s"}`, deviceID)))
	if tokenErr == nil && len(tokenBody) > 0 {
		var tokenResp struct {
			Token string `json:"token"`
		}
		if json.Unmarshal(tokenBody, &tokenResp) == nil {
			derpToken = tokenResp.Token
		}
	}

	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+auth.Token)
	headers.Set("X-Org-ID", fmt.Sprintf("%d", auth.OrgID))

	derpOpts := []derp.Option{
		derp.WithHeaders(headers),
	}
	if derpToken != "" {
		derpOpts = append(derpOpts, derp.WithDERPTunnelToken(derpToken))
	} else {
		derpOpts = append(derpOpts, derp.WithSessionToken(auth.Token))
	}

	derpClient := derp.NewClient(derpURL, deviceID, derpOpts...)

	listenAddr := fmt.Sprintf("127.0.0.1:%d", port)
	proxy := NewExitProxy(ProxyOptions{
		ListenAddr: listenAddr,
		ExitPeerID: selectedPeer,
		OrgID:      fmt.Sprintf("%d", auth.OrgID),
		DERPClient: derpClient,
	})

	// Write state file.
	if err := writeExitState(&ExitState{
		PID:        os.Getpid(),
		ExitPeer:   selectedPeer,
		ListenAddr: listenAddr,
		StartedAt:  time.Now(),
	}); err != nil {
		_ = p.host.Log(ctx, plugin.LogLevelWarning, fmt.Sprintf("Could not write state file: %v", err))
	}

	// Start DERP client in background.
	derpCtx, derpCancel := context.WithCancel(ctx)
	defer derpCancel()

	derpErrCh := make(chan error, 1)
	go func() {
		derpErrCh <- derpClient.Run(derpCtx)
	}()

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("SOCKS5 proxy listening on %s", listenAddr))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("Exit peer: %s", selectedPeer))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  curl --proxy socks5://127.0.0.1:%d https://example.com", port))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "Press Ctrl+C to stop.")

	// Start proxy in background.
	proxyErrCh := make(chan error, 1)
	go func() {
		proxyErrCh <- proxy.Start(derpCtx)
	}()

	// Signal handling.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case sig := <-sigCh:
		_ = p.host.Log(ctx, plugin.LogLevelWarning, fmt.Sprintf("Received %s, shutting down...", sig))
	case err := <-derpErrCh:
		if err != nil {
			_ = p.host.Log(ctx, plugin.LogLevelError, fmt.Sprintf("DERP connection lost: %v", err))
		}
	case err := <-proxyErrCh:
		if err != nil && err != context.Canceled {
			_ = p.host.Log(ctx, plugin.LogLevelError, fmt.Sprintf("SOCKS5 proxy error: %v", err))
		}
	}

	derpCancel()
	proxy.Stop()
	derpClient.Close()
	removeExitState()

	return plugin.ExecuteResponse{}
}

// execOff stops a running exit proxy.
func (p *ExitPlugin) execOff(ctx context.Context, _ plugin.ExecuteRequest) plugin.ExecuteResponse {
	st, err := readExitState()
	if err != nil {
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "No exit proxy is running")
		return plugin.ExecuteResponse{}
	}

	// Check if PID is still alive before sending signal.
	alive := false
	proc, findErr := os.FindProcess(st.PID)
	if findErr == nil {
		if err := proc.Signal(syscall.Signal(0)); err == nil {
			alive = true
		}
	}

	if alive {
		_ = proc.Signal(syscall.SIGTERM)
		_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Stopped exit proxy (PID %d)", st.PID))
		_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Was routing through %s on %s", st.ExitPeer, st.ListenAddr))
	} else {
		_ = p.host.Log(ctx, plugin.LogLevelWarning, fmt.Sprintf("Exit proxy (PID %d) is no longer running — cleaning up stale state", st.PID))
	}

	removeExitState()
	return plugin.ExecuteResponse{}
}

// execStatus shows the current exit proxy state.
func (p *ExitPlugin) execStatus(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	// Check --format json from raw args.
	wantJSON := req.OutputFormat == "json"
	for _, a := range req.Args[1:] {
		if a == "--format" || a == "json" {
			wantJSON = true
		}
	}

	st, err := readExitState()
	if err != nil {
		if wantJSON {
			return plugin.ExecuteResponse{Stdout: `{"running":false}` + "\n"}
		}
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "No exit proxy is running")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "Start one with: prysm mesh exit use [peer] [--port 1080]")
		return plugin.ExecuteResponse{}
	}

	// Check if PID is still alive.
	alive := false
	if proc, err := os.FindProcess(st.PID); err == nil {
		if err := proc.Signal(syscall.Signal(0)); err == nil {
			alive = true
		}
	}

	if wantJSON {
		out := map[string]interface{}{
			"running":     alive,
			"pid":         st.PID,
			"exit_peer":   st.ExitPeer,
			"listen_addr": st.ListenAddr,
			"started_at":  st.StartedAt.Format(time.RFC3339),
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		return plugin.ExecuteResponse{Stdout: string(data) + "\n"}
	}

	if !alive {
		removeExitState()
		_ = p.host.Log(ctx, plugin.LogLevelWarning, fmt.Sprintf("Exit proxy (PID %d) is no longer running — stale state cleaned up", st.PID))
		return plugin.ExecuteResponse{}
	}

	uptime := time.Since(st.StartedAt).Truncate(time.Second)
	_ = p.host.Log(ctx, plugin.LogLevelSuccess, "Exit proxy is running")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  PID:        %d", st.PID))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Exit Peer:  %s", st.ExitPeer))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Listen:     %s", st.ListenAddr))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Uptime:     %s", uptime))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  curl --proxy socks5://%s https://example.com", st.ListenAddr))
	return plugin.ExecuteResponse{}
}
