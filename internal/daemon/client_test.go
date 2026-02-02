package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultSocket(t *testing.T) {
	// Save original euid check by testing the logic
	socket := DefaultSocket()
	if socket == "" {
		t.Error("DefaultSocket() returned empty string")
	}

	// For non-root users, should use home directory
	if os.Geteuid() != 0 {
		home := os.Getenv("HOME")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		expected := filepath.Join(home, ".prysm", "meshd.sock")
		if socket != expected {
			t.Errorf("DefaultSocket() = %q, want %q for non-root", socket, expected)
		}
	}
}

func TestNewClientWithEnvOverride(t *testing.T) {
	customSocket := "/custom/path/meshd.sock"
	t.Setenv("PRYSM_MESHD_SOCKET", customSocket)

	client := NewClient("")
	if client.SocketPath() != customSocket {
		t.Errorf("SocketPath() = %q, want %q", client.SocketPath(), customSocket)
	}
}

func TestNewClientWithExplicitSocket(t *testing.T) {
	socket := "/explicit/socket.sock"
	client := NewClient(socket)
	if client.SocketPath() != socket {
		t.Errorf("SocketPath() = %q, want %q", client.SocketPath(), socket)
	}
}

func TestClientStatus(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create unix listener: %v", err)
	}
	defer listener.Close()

	expectedStatus := StatusResponse{
		InterfaceUp: true,
		PeerCount:   5,
		LastApply:   time.Now(),
		Warnings:    []string{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		json.NewEncoder(w).Encode(expectedStatus)
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)
	defer srv.Close()

	client := NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}

	if status.InterfaceUp != expectedStatus.InterfaceUp {
		t.Errorf("InterfaceUp = %v, want %v", status.InterfaceUp, expectedStatus.InterfaceUp)
	}
	if status.PeerCount != expectedStatus.PeerCount {
		t.Errorf("PeerCount = %d, want %d", status.PeerCount, expectedStatus.PeerCount)
	}
}

func TestClientApply(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create unix listener: %v", err)
	}
	defer listener.Close()

	var receivedConfig ApplyConfigRequest

	mux := http.NewServeMux()
	mux.HandleFunc("/apply", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		json.NewDecoder(r.Body).Decode(&receivedConfig)
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)
	defer srv.Close()

	client := NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := ApplyConfigRequest{
		Interface: InterfaceConfig{
			Address:    "100.64.0.5/32",
			PrivateKey: "test-private-key",
		},
		Peers: []PeerConfig{
			{
				PublicKey:  "peer-pub-key",
				Endpoint:   "10.0.0.1:51820",
				AllowedIPs: []string{"100.64.0.0/24"},
			},
		},
	}

	err = client.Apply(ctx, config)
	if err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	if receivedConfig.Interface.Address != config.Interface.Address {
		t.Errorf("Interface.Address = %q, want %q", receivedConfig.Interface.Address, config.Interface.Address)
	}
}

func TestClientStartStop(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create unix listener: %v", err)
	}
	defer listener.Close()

	startCalled := false
	stopCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		startCalled = true
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		stopCalled = true
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)
	defer srv.Close()

	client := NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if !startCalled {
		t.Error("Start() did not call /start endpoint")
	}

	if err := client.Stop(ctx); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	if !stopCalled {
		t.Error("Stop() did not call /stop endpoint")
	}
}

func TestClientErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create unix listener: %v", err)
	}
	defer listener.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "daemon crashed",
			"hint":  "restart the daemon",
		})
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)
	defer srv.Close()

	client := NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Status(ctx)
	if err == nil {
		t.Fatal("Expected error for 500 response")
	}

	if errStr := err.Error(); errStr == "" {
		t.Error("Error message should not be empty")
	}
}

func TestClientConnectionError(t *testing.T) {
	// Use non-existent socket
	client := NewClient("/nonexistent/path/socket.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Status(ctx)
	if err == nil {
		t.Fatal("Expected connection error")
	}
}

func TestFormatDaemonError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		data       []byte
		wantSubstr string
	}{
		{
			name:       "empty body",
			statusCode: 500,
			data:       []byte{},
			wantSubstr: "HTTP 500",
		},
		{
			name:       "plain text",
			statusCode: 400,
			data:       []byte("bad request"),
			wantSubstr: "bad request",
		},
		{
			name:       "json with error",
			statusCode: 400,
			data:       []byte(`{"error": "invalid config"}`),
			wantSubstr: "invalid config",
		},
		{
			name:       "json with error and hint",
			statusCode: 400,
			data:       []byte(`{"error": "missing key", "hint": "generate a new key"}`),
			wantSubstr: "Hint: generate a new key",
		},
		{
			name:       "json with derp_only",
			statusCode: 400,
			data:       []byte(`{"derp_only": "true"}`),
			wantSubstr: "DERP-only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDaemonError(tt.statusCode, tt.data)
			if got == "" {
				t.Error("formatDaemonError returned empty string")
			}
			if tt.wantSubstr != "" && !contains(got, tt.wantSubstr) {
				t.Errorf("formatDaemonError() = %q, want substring %q", got, tt.wantSubstr)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && s != "" && substr != "" && 
		findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
