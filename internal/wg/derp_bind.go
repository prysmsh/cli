package wg

import (
	"encoding/base64"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"

	"golang.zx2c4.com/wireguard/conn"
)

// DERPSender sends a WireGuard packet to a peer via the DERP relay.
type DERPSender interface {
	SendWGPacket(targetPeerID string, data []byte) error
}

// derpEndpoint implements conn.Endpoint for DERP-relayed peers.
type derpEndpoint struct {
	peerID string // DERP device ID of the remote peer
}

func (e *derpEndpoint) ClearSrc()           {}
func (e *derpEndpoint) SrcToString() string { return "" }
func (e *derpEndpoint) DstToString() string { return e.peerID }
func (e *derpEndpoint) DstToBytes() []byte  { return []byte(e.peerID) }
func (e *derpEndpoint) DstIP() netip.Addr   { return netip.Addr{} }
func (e *derpEndpoint) SrcIP() netip.Addr   { return netip.Addr{} }

// DERPBind implements conn.Bind, routing WireGuard packets through the DERP relay
// instead of raw UDP sockets. This removes the need for direct UDP connectivity
// and works through NAT/firewalls.
type DERPBind struct {
	sender DERPSender
	closed bool
	mu     sync.Mutex

	// Inbound packet queue — DERP message handler pushes here,
	// wireguard-go's receive goroutine pulls from here.
	inbound chan derpPacket
	done    chan struct{}

	// The DERP relay rewrites "from" to relay-assigned IDs.
	// We track known relay→endpoint mappings to match responses.
	aliasMu   sync.RWMutex
	peerAlias map[string]string // relay ID → configured endpoint ID
	knownEPs  map[string]bool   // configured endpoint IDs we've sent to

	// Traffic counters.
	txBytes atomic.Int64
	rxBytes atomic.Int64
}

type derpPacket struct {
	data   []byte
	peerID string
}

// NewDERPBind creates a conn.Bind that transports WireGuard packets over DERP.
func NewDERPBind(sender DERPSender) *DERPBind {
	return &DERPBind{
		sender:    sender,
		inbound:   make(chan derpPacket, 256),
		done:      make(chan struct{}),
		peerAlias: make(map[string]string),
		knownEPs:  make(map[string]bool),
	}
}

func (b *DERPBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	// Reset state — wireguard-go calls Close() then Open() during reconfiguration.
	b.closed = false
	b.inbound = make(chan derpPacket, 256)
	b.done = make(chan struct{})
	b.mu.Unlock()

	recv := func(packets [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
		select {
		case pkt, ok := <-b.inbound:
			if !ok {
				return 0, net.ErrClosed
			}
			n := copy(packets[0], pkt.data)
			sizes[0] = n
			eps[0] = &derpEndpoint{peerID: pkt.peerID}
			return 1, nil
		case <-b.done:
			return 0, net.ErrClosed
		}
	}
	return []conn.ReceiveFunc{recv}, 0, nil
}

func (b *DERPBind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	close(b.done)
	close(b.inbound)
	return nil
}

func (b *DERPBind) SetMark(mark uint32) error { return nil }

func (b *DERPBind) Send(bufs [][]byte, ep conn.Endpoint) error {
	peerID := ep.DstToString()
	// Track this as a known configured endpoint
	b.aliasMu.Lock()
	b.knownEPs[peerID] = true
	b.aliasMu.Unlock()

	for _, buf := range bufs {
		if len(buf) == 0 {
			continue
		}
		b.txBytes.Add(int64(len(buf)))
		_ = b.sender.SendWGPacket(peerID, buf)
	}
	return nil
}

func (b *DERPBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	return &derpEndpoint{peerID: s}, nil
}

func (b *DERPBind) BatchSize() int { return 1 }

// DeliverPacket is called by the DERP message handler when a wg_packet arrives.
func (b *DERPBind) DeliverPacket(peerID string, data []byte) {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return
	}
	b.rxBytes.Add(int64(len(data)))
	// The DERP relay rewrites "from" to the sender's relay-assigned ID,
	// which differs from the endpoint we configured (e.g. "cluster_3").
	// Map it back so wireguard-go can match the response to the peer.
	b.aliasMu.Lock()
	if mapped, ok := b.peerAlias[peerID]; ok {
		peerID = mapped
	} else if !b.knownEPs[peerID] {
		// Unknown relay ID — auto-learn by mapping to a known endpoint.
		// Pick the first configured endpoint (covers single-peer case).
		for ep := range b.knownEPs {
			b.peerAlias[peerID] = ep
			peerID = ep
			break
		}
	}
	b.aliasMu.Unlock()

	select {
	case b.inbound <- derpPacket{data: data, peerID: peerID}:
	default:
	}
}

// TrafficStats returns cumulative tx/rx byte counts.
func (b *DERPBind) TrafficStats() (tx, rx int64) {
	return b.txBytes.Load(), b.rxBytes.Load()
}

// EncodeWGPacket encodes a WireGuard packet for DERP JSON transport.
func EncodeWGPacket(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeWGPacket decodes a base64-encoded WireGuard packet from DERP.
func DecodeWGPacket(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}
