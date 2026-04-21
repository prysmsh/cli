package mesh

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/derp"
	"github.com/prysmsh/cli/internal/wg"
)

// Config holds the parameters needed to run a mesh lifecycle.
type Config struct {
	AuthToken   string
	SessionID   string
	OrgID       string
	APIURL      string
	DERPURL     string
	DeviceID    string
	HomeDir     string
	InsecureTLS bool
	WireGuard   bool
}

// Status represents the current state of the mesh lifecycle.
type Status struct {
	State     string    `json:"state"`
	OverlayIP string    `json:"overlay_ip"`
	Interface string    `json:"interface"`
	PeerCount int       `json:"peer_count"`
	StartedAt time.Time `json:"started_at"`
	TxBytes   int64     `json:"tx_bytes"`
	RxBytes   int64     `json:"rx_bytes"`
}

// Lifecycle owns the DERP client, WireGuard tunnel, and keepalive ping loop.
// It does NOT own exit proxy, subnet routing, or SOCKS5 — those remain in the
// CLI command layer.
type Lifecycle struct {
	mu         sync.RWMutex
	cfg        Config
	apiClient  *api.Client
	derpClient *derp.Client
	wgTunnel   *wg.Tunnel
	wgBind     *wg.DERPBind
	cancel     context.CancelFunc
	status     Status
	done       chan struct{}
	logger     *log.Logger
}

// New creates a Lifecycle in the disconnected state.
func New(cfg Config) *Lifecycle {
	return &Lifecycle{
		cfg:    cfg,
		done:   make(chan struct{}),
		status: Status{State: "disconnected"},
		logger: log.New(log.Writer(), "mesh: ", log.LstdFlags),
	}
}

// Start connects to the mesh: registers with the API, sets up DERP and
// optionally WireGuard, then runs the keepalive loop. It blocks until the
// context is cancelled, DERP errors out, or Stop is called.
func (l *Lifecycle) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	l.mu.Lock()
	l.cancel = cancel
	l.mu.Unlock()

	defer func() {
		l.shutdown()
		cancel()
	}()

	// 1. API client
	apiClient := api.NewClient(l.cfg.APIURL, api.WithInsecureSkipVerify(l.cfg.InsecureTLS))
	apiClient.SetToken(l.cfg.AuthToken)
	l.mu.Lock()
	l.apiClient = apiClient
	l.mu.Unlock()

	// 2. Register mesh node
	registerPayload := map[string]interface{}{
		"device_id": l.cfg.DeviceID,
		"peer_type": "client",
		"status":    "connected",
		"capabilities": map[string]interface{}{
			"platform":   "cli",
			"features":   []string{"service_discovery", "health_check"},
			"registered": time.Now().UTC().Format(time.RFC3339),
		},
	}
	if _, err := apiClient.RegisterMeshNode(ctx, registerPayload); err != nil {
		return fmt.Errorf("register mesh node: %w", err)
	}

	// 3. Build DERP client
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+l.cfg.AuthToken)
	headers.Set("X-Session-ID", l.cfg.SessionID)
	headers.Set("X-Org-ID", l.cfg.OrgID)

	capabilities := map[string]interface{}{
		"platform":   "cli",
		"features":   []string{"service_discovery", "health_check"},
		"registered": time.Now().UTC().Format(time.RFC3339),
	}

	derpClient := derp.NewClient(l.cfg.DERPURL, l.cfg.DeviceID,
		derp.WithHeaders(headers),
		derp.WithCapabilities(capabilities),
		derp.WithInsecure(l.cfg.InsecureTLS),
		derp.WithSessionToken(l.cfg.AuthToken),
	)
	l.mu.Lock()
	l.derpClient = derpClient
	l.mu.Unlock()

	// 4. WireGuard tunnel (optional)
	if l.cfg.WireGuard {
		tun, bind, err := wg.SetupMeshWireGuardDERP(ctx, apiClient, l.cfg.HomeDir, l.cfg.DeviceID, derpClient)
		if err != nil {
			l.logger.Printf("WireGuard tunnel disabled: %v", err)
		} else {
			l.mu.Lock()
			l.wgTunnel = tun
			l.wgBind = bind
			l.mu.Unlock()

			derpClient.WGPacketHandler = func(fromPeerID string, packet []byte) {
				bind.DeliverPacket(fromPeerID, packet)
			}
			derpClient.OnConnected = func() {
				time.Sleep(500 * time.Millisecond)
				for _, p := range tun.Peers() {
					if err := tun.RetriggerHandshake(p); err != nil {
						l.logger.Printf("retrigger handshake %s: %v", p.PublicKey[:8], err)
					}
				}
			}
			l.logger.Printf("WireGuard tunnel active (%s on %s) via DERP", tun.OverlayIP(), tun.InterfaceName())
		}
	}

	// 5. Update status to connected
	l.mu.Lock()
	l.status = Status{
		State:     "connected",
		StartedAt: time.Now(),
	}
	if l.wgTunnel != nil {
		l.status.OverlayIP = l.wgTunnel.OverlayIP()
		l.status.Interface = l.wgTunnel.InterfaceName()
	}
	l.mu.Unlock()

	// 6. Keepalive ticker — ping backend every 60s
	pingTicker := time.NewTicker(60 * time.Second)
	defer pingTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pingTicker.C:
				pingCtx, pingCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				if err := apiClient.PingMeshNode(pingCtx, l.cfg.DeviceID); err != nil {
					msg := fmt.Sprintf("mesh ping: %v", err)
					if strings.Contains(err.Error(), "Invalid token") || strings.Contains(err.Error(), "401") {
						msg += " - token may be expired"
					}
					l.logger.Printf("%s", msg)
				}
				pingCancel()
			}
		}
	}()

	// 7. Run DERP client in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- derpClient.Run(ctx)
	}()

	// 8. Block on context cancellation or DERP error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// Stop cancels the lifecycle context and waits for shutdown to complete.
func (l *Lifecycle) Stop() {
	l.mu.RLock()
	cancel := l.cancel
	l.mu.RUnlock()

	if cancel != nil {
		cancel()
	}

	// Wait for Start to finish its cleanup.
	<-l.done
}

// GetStatus returns the current lifecycle status.
func (l *Lifecycle) GetStatus() Status {
	l.mu.RLock()
	defer l.mu.RUnlock()
	st := l.status
	if l.wgBind != nil {
		st.TxBytes, st.RxBytes = l.wgBind.TrafficStats()
	}
	return st
}

// RefreshToken updates the auth token on both the API client and stored config.
func (l *Lifecycle) RefreshToken(token string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cfg.AuthToken = token
	if l.apiClient != nil {
		l.apiClient.SetToken(token)
	}
}

// WGConfigData holds WireGuard config for the macOS Network Extension.
type WGConfigData struct {
	PrivateKey string              `json:"private_key"`
	OverlayIP  string              `json:"overlay_ip"`
	DERPURL    string              `json:"derp_url"`
	Peers      []map[string]string `json:"peers"`
}

// GetWGConfig returns the active WireGuard configuration (key + peers) for use
// by the macOS Network Extension. Returns nil if WireGuard is not active.
func (l *Lifecycle) GetWGConfig() *WGConfigData {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.wgTunnel == nil {
		return nil
	}

	peers := make([]map[string]string, 0, len(l.wgTunnel.GetPeers()))
	for _, p := range l.wgTunnel.GetPeers() {
		peer := map[string]string{
			"public_key": p.PublicKey,
			"endpoint":   p.Endpoint,
		}
		if len(p.AllowedIPs) > 0 {
			peer["allowed_ips"] = strings.Join(p.AllowedIPs, ",")
		}
		peers = append(peers, peer)
	}

	return &WGConfigData{
		PrivateKey: l.wgTunnel.PrivateKeyBase64(),
		OverlayIP:  l.wgTunnel.OverlayIP(),
		DERPURL:    l.cfg.DERPURL,
		Peers:      peers,
	}
}

// shutdown tears down DERP, WireGuard, and signals completion via done channel.
func (l *Lifecycle) shutdown() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.derpClient != nil {
		l.derpClient.Close()
		l.derpClient = nil
	}
	if l.wgBind != nil {
		l.wgBind.Close()
		l.wgBind = nil
	}
	if l.wgTunnel != nil {
		_ = l.wgTunnel.Stop()
		l.wgTunnel = nil
	}

	l.status.State = "disconnected"

	select {
	case <-l.done:
		// already closed
	default:
		close(l.done)
	}
}
