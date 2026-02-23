package subnet

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
)

// SubnetRouter redirects OS TCP traffic for one or more CIDRs transparently
// to a local listener, then forwards each connection via dialFunc over DERP.
//
// On Linux it uses iptables -t nat OUTPUT REDIRECT rules. Needs root /
// CAP_NET_ADMIN + CAP_NET_RAW.
type SubnetRouter struct {
	// peers maps CIDR string → exit peer device ID.
	peers    map[string]string
	dialFunc DialFunc
	bypass   []string

	listener  net.Listener
	localPort int
	rules     []string // CIDRs for which iptables rules were injected.
	bypassed  []string // CIDRs/IPs for which bypass rules were injected.
	cancel    context.CancelFunc
}

// New creates a SubnetRouter.
//
//   - cidrs: map[cidr]exitPeerDeviceID — which CIDRs to route and via which peer.
//   - dial: called per TCP connection; must select the peer and dial via DERP.
func New(cidrs map[string]string, dial DialFunc) *SubnetRouter {
	return &SubnetRouter{
		peers:    cidrs,
		dialFunc: dial,
	}
}

// NewWithBypass configures bypass CIDRs/IPs that should never be redirected.
// Useful for keeping control-plane traffic (DERP/API) off the exit path during bootstrap.
func NewWithBypass(cidrs map[string]string, dial DialFunc, bypass []string) *SubnetRouter {
	r := New(cidrs, dial)
	r.bypass = append([]string(nil), bypass...)
	return r
}

// Start opens a local TCP listener on a random port, inserts iptables REDIRECT
// rules for every CIDR, and begins accepting connections.
//
// Returns an error when iptables manipulation fails (typically not root).
// The caller should fall back to SOCKS5-only mode with a warning.
func (r *SubnetRouter) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("subnet router: listen: %w", err)
	}
	r.listener = ln
	r.localPort = ln.Addr().(*net.TCPAddr).Port

	bypassCIDRs := uniqueSortedStrings(r.bypass)
	peerCIDRs := mapKeysSorted(r.peers)

	// Insert high-priority bypass rules first so control-plane traffic is never
	// captured by broad redirects like 0.0.0.0/0.
	for _, cidr := range bypassCIDRs {
		if err := addBypass(cidr); err != nil {
			ln.Close()
			for _, added := range r.bypassed {
				_ = removeBypass(added)
			}
			return fmt.Errorf("subnet router: %w", err)
		}
		r.bypassed = append(r.bypassed, cidr)
	}

	// Remove any stale REDIRECT rules left by a previous killed process.
	for _, cidr := range peerCIDRs {
		cleanStaleRedirects(cidr)
	}

	// Insert iptables rules for each CIDR (fail fast on first error —
	// most likely not root).
	for _, cidr := range peerCIDRs {
		if err := addRedirect(cidr, r.localPort); err != nil {
			ln.Close()
			// Remove any rules we already added.
			for _, added := range r.rules {
				_ = removeRedirect(added, r.localPort)
			}
			for _, added := range r.bypassed {
				_ = removeBypass(added)
			}
			return fmt.Errorf("subnet router: %w (need root/CAP_NET_ADMIN)", err)
		}
		r.rules = append(r.rules, cidr)
	}

	lctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	go r.serve(lctx)
	return nil
}

// serve accepts connections from the listener until ctx is cancelled.
func (r *SubnetRouter) serve(ctx context.Context) {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
			default:
				log.Printf("[subnet] accept: %v", err)
			}
			return
		}
		go serveConn(conn, r.dialFunc)
	}
}

// Stop removes iptables rules and closes the listener. Safe to call even if
// Start was never called or returned an error.
func (r *SubnetRouter) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	if r.listener != nil {
		r.listener.Close()
		r.listener = nil
	}
	for i := len(r.rules) - 1; i >= 0; i-- {
		cidr := r.rules[i]
		if err := removeRedirect(cidr, r.localPort); err != nil {
			log.Printf("[subnet] remove redirect %s: %v", cidr, err)
		}
	}
	for i := len(r.bypassed) - 1; i >= 0; i-- {
		cidr := r.bypassed[i]
		if err := removeBypass(cidr); err != nil {
			log.Printf("[subnet] remove bypass %s: %v", cidr, err)
		}
	}
	r.rules = nil
	r.bypassed = nil
}

// MatchCIDR returns the exit peer device ID for ip, or "" if no CIDR matches.
// Convenience helper for constructing the dialFunc closure in cmd/mesh.go.
func MatchCIDR(peers map[string]string, ip net.IP) string {
	for cidr, peer := range peers {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipNet.Contains(ip) {
			return peer
		}
	}
	return ""
}

func uniqueSortedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		set[v] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func mapKeysSorted(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for k := range values {
		if k == "" {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
