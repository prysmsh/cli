package exit

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func TestSocks5Server_ListenAndServe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a real dialer for test — connect to a simple TCP echo server.
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	defer echoLn.Close()

	go func() {
		for {
			conn, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()

	echoAddr := echoLn.Addr().String()

	srv := NewSocks5Server("127.0.0.1:0", func(ctx context.Context, network, addr string) (net.Conn, error) {
		return net.Dial(network, addr)
	})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	// Wait for listener to be ready.
	time.Sleep(50 * time.Millisecond)

	listenAddr := srv.Addr()
	if listenAddr == "" {
		t.Fatal("server not listening")
	}

	// Connect as SOCKS5 client.
	conn, err := net.Dial("tcp", listenAddr)
	if err != nil {
		t.Fatalf("dial socks5: %v", err)
	}
	defer conn.Close()

	// Auth handshake: version 5, 1 method (no-auth).
	conn.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	if _, err := io.ReadFull(conn, authReply); err != nil {
		t.Fatalf("read auth reply: %v", err)
	}
	if authReply[0] != 0x05 || authReply[1] != 0x00 {
		t.Fatalf("unexpected auth reply: %x", authReply)
	}

	// CONNECT to echo server via domain name.
	host, port, _ := net.SplitHostPort(echoAddr)
	portNum := 0
	fmt.Sscanf(port, "%d", &portNum)

	// Build CONNECT request with IPv4.
	ip := net.ParseIP(host).To4()
	connectReq := []byte{0x05, 0x01, 0x00, 0x01}
	connectReq = append(connectReq, ip...)
	connectReq = append(connectReq, byte(portNum>>8), byte(portNum))
	conn.Write(connectReq)

	connReply := make([]byte, 10)
	if _, err := io.ReadFull(conn, connReply); err != nil {
		t.Fatalf("read connect reply: %v", err)
	}
	if connReply[1] != 0x00 {
		t.Fatalf("CONNECT failed with reply code: %d", connReply[1])
	}

	// Send data through and verify echo.
	testData := []byte("hello via socks5")
	conn.Write(testData)
	buf := make([]byte, len(testData))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != string(testData) {
		t.Errorf("echo mismatch: got %q, want %q", buf, testData)
	}

	cancel()
}

func TestSocks5Server_FQDN(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a local TCP server as the destination.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	portNum := 0
	fmt.Sscanf(portStr, "%d", &portNum)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Write([]byte("ok"))
			conn.Close()
		}
	}()

	srv := NewSocks5Server("127.0.0.1:0", func(ctx context.Context, network, addr string) (net.Conn, error) {
		// Rewrite "localhost" to actual address.
		return net.Dial(network, ln.Addr().String())
	})
	go srv.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Auth.
	conn.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	io.ReadFull(conn, authReply)

	// CONNECT with FQDN.
	domain := "localhost"
	connectReq := []byte{0x05, 0x01, 0x00, 0x03, byte(len(domain))}
	connectReq = append(connectReq, []byte(domain)...)
	connectReq = append(connectReq, byte(portNum>>8), byte(portNum))
	conn.Write(connectReq)

	connReply := make([]byte, 10)
	io.ReadFull(conn, connReply)
	if connReply[1] != 0x00 {
		t.Fatalf("CONNECT via FQDN failed: %d", connReply[1])
	}

	buf := make([]byte, 2)
	io.ReadFull(conn, buf)
	if string(buf) != "ok" {
		t.Errorf("got %q, want ok", buf)
	}
}

func TestSocks5Server_DialError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewSocks5Server("127.0.0.1:0", func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, fmt.Errorf("dial refused")
	})
	go srv.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	conn.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	io.ReadFull(conn, authReply)

	// CONNECT to a fake target.
	connectReq := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0x00, 0x50}
	conn.Write(connectReq)

	connReply := make([]byte, 10)
	io.ReadFull(conn, connReply)
	if connReply[1] != socks5RepFail {
		t.Errorf("expected failure reply, got %d", connReply[1])
	}
}

func TestSocks5Server_BadVersion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewSocks5Server("127.0.0.1:0", func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, nil
	})
	go srv.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send wrong version.
	conn.Write([]byte{0x04, 0x01, 0x00})

	// Connection should close without a valid reply.
	buf := make([]byte, 2)
	conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected connection close for bad version")
	}
}

func TestSocks5Server_ActiveConns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewSocks5Server("127.0.0.1:0", func(ctx context.Context, network, addr string) (net.Conn, error) {
		return net.Dial(network, addr)
	})

	if got := srv.ActiveConns(); got != 0 {
		t.Errorf("ActiveConns = %d before start, want 0", got)
	}
	if got := srv.Addr(); got != "" {
		t.Errorf("Addr = %q before start, want empty", got)
	}

	go srv.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	if got := srv.Addr(); got == "" {
		t.Error("Addr should not be empty after start")
	}
}

func TestSocks5Server_NoAcceptableAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewSocks5Server("127.0.0.1:0", func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, nil
	})
	go srv.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Offer only user/pass auth (0x02), no no-auth.
	conn.Write([]byte{0x05, 0x01, 0x02})
	reply := make([]byte, 2)
	io.ReadFull(conn, reply)
	if reply[1] != 0xFF {
		t.Errorf("expected 0xFF (no acceptable method), got 0x%02x", reply[1])
	}
}
