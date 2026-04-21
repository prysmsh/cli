package cmd

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newLocalTLSServer returns an httptest.Server configured with a throwaway
// self-signed cert for 127.0.0.1. The dialUpstream test needs a real TLS
// endpoint — httptest.NewTLSServer already does this, but we wrap it here so
// the test reads clearly as "localhost HTTPS server".
func newLocalTLSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(handler)
	s.StartTLS()
	return s
}

func TestDialUpstream_PlainHTTP(t *testing.T) {
	// Plain TCP listener that echoes the first read.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		c.Write(buf[:n])
	}()

	conn, err := dialUpstream(ln.Addr().String(), "http", false)
	if err != nil {
		t.Fatalf("dial http: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, err := io.ReadAll(io.LimitReader(conn, 4))
	if err != nil && err != io.EOF {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "ping" {
		t.Fatalf("got %q want %q", got, "ping")
	}
	<-done
}

func TestDialUpstream_HTTPSHandshake(t *testing.T) {
	srv := newLocalTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello-tls"))
	}))
	defer srv.Close()

	// srv.Listener.Addr() gives us the 127.0.0.1:PORT we can dial directly.
	conn, err := dialUpstream(srv.Listener.Addr().String(), "https", true)
	if err != nil {
		t.Fatalf("dial https: %v", err)
	}
	defer conn.Close()
	if _, ok := conn.(*tls.Conn); !ok {
		t.Fatalf("expected *tls.Conn, got %T", conn)
	}

	// Issue a minimal HTTP request over the tls.Conn to make sure the server
	// actually decrypts us.
	req := "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !containsAll(resp, "HTTP/1.1 200", "hello-tls") {
		t.Fatalf("unexpected response: %q", resp)
	}
}

func TestDialUpstream_HTTPSVerifyFails(t *testing.T) {
	srv := newLocalTLSServer(t, http.NotFoundHandler())
	defer srv.Close()

	// Default pool — won't trust the throwaway cert. Must fail.
	_, err := dialUpstream(srv.Listener.Addr().String(), "https", false)
	if err == nil {
		t.Fatalf("expected handshake failure without insecure skip")
	}
}

// Shared helper for the "contains all substrings" assertion.
func containsAll(haystack []byte, needles ...string) bool {
	s := string(haystack)
	for _, n := range needles {
		if !contains(s, n) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

