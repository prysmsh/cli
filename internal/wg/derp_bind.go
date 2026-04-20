package wg

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/netip"
	"sync"

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
}

type derpPacket struct {
	data   []byte
	peerID string
}

// NewDERPBind creates a conn.Bind that transports WireGuard packets over DERP.
func NewDERPBind(sender DERPSender) *DERPBind {
	return &DERPBind{
		sender:  sender,
		inbound: make(chan derpPacket, 256),
		done:    make(chan struct{}),
	}
}

func (b *DERPBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
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
	for _, buf := range bufs {
		if len(buf) == 0 {
			continue
		}
		if err := b.sender.SendWGPacket(peerID, buf); err != nil {
			return fmt.Errorf("derp send to %s: %w", peerID, err)
		}
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
	select {
	case b.inbound <- derpPacket{data: data, peerID: peerID}:
	default:
		// Drop packet if queue is full — WireGuard handles retransmission.
	}
}

// EncodeWGPacket encodes a WireGuard packet for DERP JSON transport.
func EncodeWGPacket(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeWGPacket decodes a base64-encoded WireGuard packet from DERP.
func DecodeWGPacket(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}
