package exit

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/prysmsh/cli/internal/derp"
)

// derpConn implements net.Conn backed by a DERP route.
type derpConn struct {
	routeID string
	client  *derp.Client

	// inbound channel fed by the proxy's TunnelTrafficHandler.
	inbound chan []byte

	// buf holds leftover bytes from the last inbound chunk.
	buf []byte

	mu     sync.Mutex
	closed bool

	// deadlines (informational; not enforced on DERP side)
	readDeadline  time.Time
	writeDeadline time.Time
}

// newDERPConn creates a new derpConn for the given route.
func newDERPConn(routeID string, client *derp.Client) *derpConn {
	return &derpConn{
		routeID: routeID,
		client:  client,
		inbound: make(chan []byte, 256),
	}
}

// Feed pushes inbound data from the DERP traffic handler into this connection.
func (c *derpConn) Feed(data []byte) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return
	}
	// Copy data to avoid aliasing with the caller's buffer.
	cp := make([]byte, len(data))
	copy(cp, data)
	select {
	case c.inbound <- cp:
	default:
		// Drop if buffer full — back-pressure.
	}
}

// Read reads data received from the DERP route.
func (c *derpConn) Read(b []byte) (int, error) {
	// Drain leftover buffer first.
	if len(c.buf) > 0 {
		n := copy(b, c.buf)
		c.buf = c.buf[n:]
		return n, nil
	}

	chunk, ok := <-c.inbound
	if !ok {
		return 0, net.ErrClosed
	}
	n := copy(b, chunk)
	if n < len(chunk) {
		c.buf = chunk[n:]
	}
	return n, nil
}

// Write sends data over the DERP route.
func (c *derpConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, net.ErrClosed
	}
	if err := c.client.SendTrafficData(c.routeID, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

// Close closes the connection and unblocks any pending reads.
func (c *derpConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.inbound)
	return nil
}

func (c *derpConn) LocalAddr() net.Addr  { return derpAddr{c.routeID} }
func (c *derpConn) RemoteAddr() net.Addr { return derpAddr{c.routeID} }

func (c *derpConn) SetDeadline(t time.Time) error {
	c.readDeadline = t
	c.writeDeadline = t
	return nil
}

func (c *derpConn) SetReadDeadline(t time.Time) error {
	c.readDeadline = t
	return nil
}

func (c *derpConn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline = t
	return nil
}

// derpAddr satisfies net.Addr for DERP route connections.
type derpAddr struct {
	routeID string
}

func (a derpAddr) Network() string { return "derp" }
func (a derpAddr) String() string  { return "derp:" + a.routeID }

var _ net.Conn = (*derpConn)(nil)

var errDERPConnClosed = errors.New("derp connection closed")
