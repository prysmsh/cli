package exit

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
)

// SOCKS5 constants (RFC 1928).
const (
	socks5Version  = 0x05
	socks5AuthNone = 0x00
	socks5CmdConn  = 0x01
	socks5AtypIPv4 = 0x01
	socks5AtypFQDN = 0x03
	socks5AtypIPv6 = 0x04
	socks5RepOK    = 0x00
	socks5RepFail  = 0x01
)

// DialFunc is the function called to establish outbound connections.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Socks5Server is a minimal RFC 1928 CONNECT-only SOCKS5 proxy.
type Socks5Server struct {
	addr     string
	dialFunc DialFunc
	listener net.Listener

	activeConns atomic.Int64
	wg          sync.WaitGroup
}

// NewSocks5Server creates a new SOCKS5 server that dials outbound connections
// via dialFunc.
func NewSocks5Server(addr string, dialFunc DialFunc) *Socks5Server {
	return &Socks5Server{
		addr:     addr,
		dialFunc: dialFunc,
	}
}

// ListenAndServe starts the SOCKS5 server. It blocks until ctx is cancelled
// or an unrecoverable error occurs.
func (s *Socks5Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("socks5 listen: %w", err)
	}
	s.listener = ln

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return fmt.Errorf("socks5 accept: %w", err)
			}
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(ctx, conn)
		}()
	}
}

// Close stops the listener and waits for active connections to drain.
func (s *Socks5Server) Close() {
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
}

// Addr returns the listener's address, or "" if not listening.
func (s *Socks5Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

// ActiveConns returns the number of active proxied connections.
func (s *Socks5Server) ActiveConns() int64 {
	return s.activeConns.Load()
}

func (s *Socks5Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// --- Auth negotiation ---
	// Client: VER NMETHODS METHODS...
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	if header[0] != socks5Version {
		return
	}
	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}

	// We only support no-auth (0x00).
	hasNoAuth := false
	for _, m := range methods {
		if m == socks5AuthNone {
			hasNoAuth = true
			break
		}
	}
	if !hasNoAuth {
		conn.Write([]byte{socks5Version, 0xFF}) // no acceptable methods
		return
	}
	conn.Write([]byte{socks5Version, socks5AuthNone})

	// --- Request ---
	// VER CMD RSV ATYP DST.ADDR DST.PORT
	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil {
		return
	}
	if req[0] != socks5Version || req[1] != socks5CmdConn {
		s.sendReply(conn, socks5RepFail, nil, 0)
		return
	}

	var targetAddr string
	switch req[3] {
	case socks5AtypIPv4:
		ipBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, ipBuf); err != nil {
			return
		}
		targetAddr = net.IP(ipBuf).String()
	case socks5AtypFQDN:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return
		}
		domainBuf := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, domainBuf); err != nil {
			return
		}
		targetAddr = string(domainBuf)
	case socks5AtypIPv6:
		ipBuf := make([]byte, 16)
		if _, err := io.ReadFull(conn, ipBuf); err != nil {
			return
		}
		targetAddr = net.IP(ipBuf).String()
	default:
		s.sendReply(conn, socks5RepFail, nil, 0)
		return
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBuf)
	target := fmt.Sprintf("%s:%d", targetAddr, port)

	// --- Dial ---
	remote, err := s.dialFunc(ctx, "tcp", target)
	if err != nil {
		s.sendReply(conn, socks5RepFail, nil, 0)
		return
	}
	defer remote.Close()

	// Success reply — bind to 0.0.0.0:0
	s.sendReply(conn, socks5RepOK, net.IPv4zero, 0)

	s.activeConns.Add(1)
	defer s.activeConns.Add(-1)

	// --- Bidirectional copy ---
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(remote, conn)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(conn, remote)
		errc <- err
	}()

	// Wait for one direction to finish, then close both.
	<-errc
}

func (s *Socks5Server) sendReply(conn net.Conn, rep byte, bindAddr net.IP, bindPort uint16) {
	if bindAddr == nil {
		bindAddr = net.IPv4zero
	}
	ipv4 := bindAddr.To4()
	if ipv4 == nil {
		ipv4 = net.IPv4zero.To4()
	}
	reply := []byte{
		socks5Version, rep, 0x00, socks5AtypIPv4,
		ipv4[0], ipv4[1], ipv4[2], ipv4[3],
		byte(bindPort >> 8), byte(bindPort),
	}
	conn.Write(reply)
}

var errSocks5ServerClosed = errors.New("socks5 server closed")
