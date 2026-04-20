package meshd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prysmsh/cli/internal/mesh"
)

// Server listens on a Unix socket and dispatches commands to a mesh.Lifecycle.
type Server struct {
	socketPath string
	listener   net.Listener
	lifecycle  *mesh.Lifecycle
	mu         sync.Mutex
	running    bool
	logger     *log.Logger
}

// NewServer creates a daemon server bound to the given socket path.
func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		logger:     log.New(log.Writer(), "meshd: ", log.LstdFlags),
	}
}

// Serve creates the socket directory, removes any stale socket, and accepts
// connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}

	// Remove stale socket from a previous run.
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.listener = ln

	if err := os.Chmod(s.socketPath, 0660); err != nil {
		ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	s.logger.Printf("listening on %s", s.socketPath)

	// Close listener when context is done so Accept unblocks.
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
				return fmt.Errorf("accept: %w", err)
			}
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		s.writeResponse(conn, Response{Status: "error", Error: "invalid request: " + err.Error()})
		return
	}

	var resp Response
	switch req.Cmd {
	case "connect":
		resp = s.handleConnect(ctx, req)
	case "disconnect":
		resp = s.handleDisconnect()
	case "status":
		resp = s.handleStatus()
	case "refresh_token":
		resp = s.handleRefreshToken(req)
	case "wg_config":
		resp = s.handleWGConfig(ctx, req)
	default:
		resp = Response{Status: "error", Error: "unknown command: " + req.Cmd}
	}

	s.writeResponse(conn, resp)
}

func (s *Server) handleConnect(ctx context.Context, req Request) Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running && s.lifecycle != nil {
		st := s.lifecycle.GetStatus()
		return Response{
			Status:    st.State,
			OverlayIP: st.OverlayIP,
			Interface: st.Interface,
			PeerCount: st.PeerCount,
			Uptime:    int64(time.Since(st.StartedAt).Seconds()),
		}
	}

	cfg := mesh.Config{
		AuthToken: req.Token,
		APIURL:    req.APIURL,
		DERPURL:   req.DERPURL,
		DeviceID:  req.DeviceID,
		HomeDir:   req.HomeDir,
	}

	lc := mesh.New(cfg)
	s.lifecycle = lc
	s.running = true

	go func() {
		if err := lc.Start(ctx); err != nil {
			s.logger.Printf("lifecycle exited: %v", err)
		}
		s.mu.Lock()
		s.running = false
		s.lifecycle = nil
		s.mu.Unlock()
	}()

	// Give the lifecycle a moment to connect before returning status.
	time.Sleep(2 * time.Second)

	st := lc.GetStatus()
	return Response{
		Status:    st.State,
		OverlayIP: st.OverlayIP,
		Interface: st.Interface,
		PeerCount: st.PeerCount,
		Uptime:    int64(time.Since(st.StartedAt).Seconds()),
	}
}

func (s *Server) handleDisconnect() Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.lifecycle == nil {
		return Response{Status: "disconnected"}
	}

	s.lifecycle.Stop()
	s.running = false
	s.lifecycle = nil
	return Response{Status: "disconnected"}
}

func (s *Server) handleStatus() Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.lifecycle == nil {
		return Response{Status: "disconnected"}
	}

	st := s.lifecycle.GetStatus()
	return Response{
		Status:    st.State,
		OverlayIP: st.OverlayIP,
		Interface: st.Interface,
		PeerCount: st.PeerCount,
		Uptime:    int64(time.Since(st.StartedAt).Seconds()),
	}
}

func (s *Server) handleRefreshToken(req Request) Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.lifecycle == nil {
		return Response{Status: "error", Error: "not connected"}
	}

	s.lifecycle.RefreshToken(req.Token)
	return Response{Status: "ok"}
}

// handleWGConfig returns the WireGuard tunnel configuration (private key + peers)
// for the Network Extension to use. The tray app calls this after connect to get
// the crypto material needed to start the packet tunnel.
func (s *Server) handleWGConfig(_ context.Context, _ Request) Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.lifecycle == nil {
		return Response{Status: "error", Error: "not connected"}
	}

	wgCfg := s.lifecycle.GetWGConfig()
	if wgCfg == nil {
		return Response{Status: "error", Error: "wireguard not active"}
	}

	return Response{
		Status: "ok",
		WGConfig: &WGConfig{
			PrivateKey: wgCfg.PrivateKey,
			OverlayIP:  wgCfg.OverlayIP,
			DERPURL:    wgCfg.DERPURL,
			Peers:      wgCfg.Peers,
		},
	}
}

func (s *Server) writeResponse(conn net.Conn, resp Response) {
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		s.logger.Printf("write response: %v", err)
	}
}
