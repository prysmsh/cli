package meshd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// IsRunning checks if the daemon socket exists and is connectable.
func IsRunning() bool {
	if _, err := os.Stat(SocketPath); os.IsNotExist(err) {
		return false
	}
	conn, err := net.DialTimeout("unix", SocketPath, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Send sends a request to the daemon and returns the response.
func Send(req Request) (*Response, error) {
	conn, err := net.DialTimeout("unix", SocketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to meshd: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, fmt.Errorf("set deadline: %w", err)
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &resp, nil
}

// Connect tells the daemon to start the mesh.
func Connect(token, apiURL, derpURL, deviceID, homeDir string) (*Response, error) {
	return Send(Request{
		Cmd:      "connect",
		Token:    token,
		APIURL:   apiURL,
		DERPURL:  derpURL,
		DeviceID: deviceID,
		HomeDir:  homeDir,
	})
}

// Disconnect tells the daemon to stop the mesh.
func Disconnect() (*Response, error) {
	return Send(Request{Cmd: "disconnect"})
}

// GetStatus queries the daemon's current state.
func GetStatus() (*Response, error) {
	return Send(Request{Cmd: "status"})
}

// RefreshToken sends a new auth token to the daemon.
func RefreshToken(token string) (*Response, error) {
	return Send(Request{
		Cmd:   "refresh_token",
		Token: token,
	})
}
