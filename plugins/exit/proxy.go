package exit

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/prysmsh/cli/internal/derp"
)

// ResolveExitPeerFunc returns the exit peer (device ID) to use for a given target address.
// For <route>.<cluster>.mesh:port, this should return the mesh node that is that cluster's exit router.
// If nil or if it returns an error, ExitPeerID is used.
type ResolveExitPeerFunc func(ctx context.Context, targetAddress string) (exitPeerID string, err error)

// ProxyOptions configures the exit proxy.
type ProxyOptions struct {
	// ListenAddr is the local SOCKS5 listen address (e.g. "127.0.0.1:1080").
	ListenAddr string
	// ExitPeerID is the device ID of the exit-enabled peer (used when ResolveExitPeer is nil or returns error).
	ExitPeerID string
	// OrgID is the organization ID for route requests.
	OrgID string
	// DERPClient is the connected DERP client to use.
	DERPClient *derp.Client
	// ResolveExitPeer optionally resolves the exit peer per target address (e.g. myapi.franklocal.mesh:80 → cluster's node).
	ResolveExitPeer ResolveExitPeerFunc
}

// ExitProxy wires a local SOCKS5 server to a DERP exit peer.
type ExitProxy struct {
	opts   ProxyOptions
	socks5 *Socks5Server

	mu     sync.RWMutex
	routes map[string]*derpConn // routeID → derpConn

	// pending tracks in-flight dial requests waiting for route_response.
	pendingMu sync.Mutex
	pending   map[string]chan string // routeID → chan status
}

// NewExitProxy creates an exit proxy.
func NewExitProxy(opts ProxyOptions) *ExitProxy {
	p := &ExitProxy{
		opts:    opts,
		routes:  make(map[string]*derpConn),
		pending: make(map[string]chan string),
	}
	p.socks5 = NewSocks5Server(opts.ListenAddr, p.dialViaDERP)
	return p
}

// HandleRouteResponse is called when a route_response is received from the relay.
// Use this when integrating the proxy with a shared DERP client (e.g. mesh connect).
func (p *ExitProxy) HandleRouteResponse(routeID, status string) {
	p.pendingMu.Lock()
	ch, ok := p.pending[routeID]
	if ok {
		delete(p.pending, routeID)
	}
	p.pendingMu.Unlock()
	if ok {
		select {
		case ch <- status:
		default:
		}
	}
}

// HandleTrafficData is called when traffic_data is received for an exit route.
// Use this when integrating the proxy with a shared DERP client (e.g. mesh connect).
func (p *ExitProxy) HandleTrafficData(routeID string, data []byte) {
	if data == nil {
		return
	}
	p.mu.RLock()
	dc := p.routes[routeID]
	p.mu.RUnlock()
	if dc != nil {
		dc.Feed(data)
	}
}

// ListenAndServe starts only the SOCKS5 listener. The caller must set
// RouteResponseHandler and TunnelTrafficHandler on the DERP client to call
// HandleRouteResponse and HandleTrafficData so the proxy works.
func (p *ExitProxy) ListenAndServe(ctx context.Context) error {
	return p.socks5.ListenAndServe(ctx)
}

// Start configures DERP handlers and starts the SOCKS5 server.
// It blocks until ctx is cancelled.
func (p *ExitProxy) Start(ctx context.Context) error {
	client := p.opts.DERPClient

	client.RouteResponseHandler = p.HandleRouteResponse
	client.TunnelTrafficHandler = func(routeID string, targetPort, externalPort int, data []byte) {
		p.HandleTrafficData(routeID, data)
	}

	return p.socks5.ListenAndServe(ctx)
}

// Stop gracefully shuts down the proxy.
func (p *ExitProxy) Stop() {
	p.socks5.Close()

	// Close all derpConns.
	p.mu.Lock()
	for id, dc := range p.routes {
		dc.Close()
		delete(p.routes, id)
	}
	p.mu.Unlock()
}

// Addr returns the SOCKS5 listen address.
func (p *ExitProxy) Addr() string {
	return p.socks5.Addr()
}

// dialViaDERP is the SOCKS5 server's dial function. It resolves the exit peer
// for the target address and delegates to DialViaDERP.
func (p *ExitProxy) dialViaDERP(ctx context.Context, network, addr string) (net.Conn, error) {
	exitPeerID := p.opts.ExitPeerID
	if p.opts.ResolveExitPeer != nil {
		if peer, err := p.opts.ResolveExitPeer(ctx, addr); err == nil && peer != "" {
			exitPeerID = peer
		}
	}
	return p.DialViaDERP(ctx, exitPeerID, addr)
}

// DialViaDERP dials addr through the specified exit peer over DERP.
// It is exported so the subnet router can forward raw TUN traffic without
// going through SOCKS5.
func (p *ExitProxy) DialViaDERP(ctx context.Context, exitPeerID, addr string) (net.Conn, error) {
	client := p.opts.DERPClient

	routeID, err := client.SendExitRouteRequest(p.opts.OrgID, exitPeerID, addr)
	if err != nil {
		log.Printf("[exit proxy] send exit route request (peer=%s addr=%s): %v", exitPeerID, addr, err)
		return nil, fmt.Errorf("send exit route request: %w", err)
	}

	// Register pending response channel.
	ch := make(chan string, 1)
	p.pendingMu.Lock()
	p.pending[routeID] = ch
	p.pendingMu.Unlock()

	// Wait for route_response or timeout.
	select {
	case status := <-ch:
		if status != "ok" {
			log.Printf("[exit proxy] route %s rejected by peer %s: %s", routeID, exitPeerID, status)
			return nil, fmt.Errorf("exit route rejected: %s", status)
		}
	case <-ctx.Done():
		p.pendingMu.Lock()
		delete(p.pending, routeID)
		p.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
		p.pendingMu.Lock()
		delete(p.pending, routeID)
		p.pendingMu.Unlock()
		log.Printf("[exit proxy] route %s to peer %s addr %s timed out", routeID, exitPeerID, addr)
		return nil, fmt.Errorf("exit route request timed out")
	}

	dc := newDERPConn(routeID, client)
	p.mu.Lock()
	p.routes[routeID] = dc
	p.mu.Unlock()

	return dc, nil
}
