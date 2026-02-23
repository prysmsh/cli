package exit

import (
	"net"
	"testing"
	"time"
)

func TestDERPConn_ReadWrite(t *testing.T) {
	dc := newDERPConn("route-1", nil)

	// Feed data simulating DERP traffic handler.
	go func() {
		dc.Feed([]byte("hello"))
		dc.Feed([]byte("world"))
	}()

	buf := make([]byte, 32)

	n, err := dc.Read(buf)
	if err != nil {
		t.Fatalf("Read 1: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("Read 1 = %q, want hello", buf[:n])
	}

	n, err = dc.Read(buf)
	if err != nil {
		t.Fatalf("Read 2: %v", err)
	}
	if string(buf[:n]) != "world" {
		t.Errorf("Read 2 = %q, want world", buf[:n])
	}
}

func TestDERPConn_ReadPartial(t *testing.T) {
	dc := newDERPConn("route-1", nil)

	dc.Feed([]byte("abcdef"))

	// Read with small buffer to test leftover handling.
	buf := make([]byte, 3)
	n, err := dc.Read(buf)
	if err != nil {
		t.Fatalf("Read 1: %v", err)
	}
	if string(buf[:n]) != "abc" {
		t.Errorf("Read 1 = %q, want abc", buf[:n])
	}

	n, err = dc.Read(buf)
	if err != nil {
		t.Fatalf("Read 2: %v", err)
	}
	if string(buf[:n]) != "def" {
		t.Errorf("Read 2 = %q, want def", buf[:n])
	}
}

func TestDERPConn_Close(t *testing.T) {
	dc := newDERPConn("route-1", nil)
	dc.Close()

	// Read after close should return error.
	buf := make([]byte, 10)
	_, err := dc.Read(buf)
	if err != net.ErrClosed {
		t.Errorf("Read after close: got %v, want net.ErrClosed", err)
	}

	// Write after close should return error.
	_, err = dc.Write([]byte("test"))
	if err != net.ErrClosed {
		t.Errorf("Write after close: got %v, want net.ErrClosed", err)
	}

	// Double close should not panic.
	dc.Close()
}

func TestDERPConn_FeedAfterClose(t *testing.T) {
	dc := newDERPConn("route-1", nil)
	dc.Close()

	// Feed after close should not panic.
	dc.Feed([]byte("data"))
}

func TestDERPConn_Addr(t *testing.T) {
	dc := newDERPConn("route-42", nil)

	local := dc.LocalAddr()
	remote := dc.RemoteAddr()

	if local.Network() != "derp" {
		t.Errorf("LocalAddr.Network = %q, want derp", local.Network())
	}
	if local.String() != "derp:route-42" {
		t.Errorf("LocalAddr = %q, want derp:route-42", local.String())
	}
	if remote.String() != "derp:route-42" {
		t.Errorf("RemoteAddr = %q, want derp:route-42", remote.String())
	}
}

func TestDERPConn_Deadlines(t *testing.T) {
	dc := newDERPConn("route-1", nil)

	now := time.Now()
	if err := dc.SetDeadline(now); err != nil {
		t.Errorf("SetDeadline: %v", err)
	}
	if err := dc.SetReadDeadline(now); err != nil {
		t.Errorf("SetReadDeadline: %v", err)
	}
	if err := dc.SetWriteDeadline(now); err != nil {
		t.Errorf("SetWriteDeadline: %v", err)
	}
}

func TestDERPConn_NetConnInterface(t *testing.T) {
	// Verify it implements net.Conn at compile time.
	var _ net.Conn = (*derpConn)(nil)
}
