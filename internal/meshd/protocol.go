package meshd

// Request is a command from CLI to daemon.
type Request struct {
	Cmd      string `json:"cmd"`               // "connect", "disconnect", "status", "refresh_token"
	Token    string `json:"token,omitempty"`    // session token (for connect, refresh_token)
	APIURL   string `json:"api_url,omitempty"`
	DERPURL  string `json:"derp_url,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	HomeDir  string `json:"home_dir,omitempty"`
}

// Response is a reply from daemon to CLI.
type Response struct {
	Status    string `json:"status"`              // "ok", "connected", "disconnected", "error"
	OverlayIP string `json:"overlay_ip,omitempty"`
	Interface string `json:"interface,omitempty"`
	PeerCount int    `json:"peer_count,omitempty"`
	Uptime    int64  `json:"uptime,omitempty"`     // seconds
	Error     string `json:"error,omitempty"`
}

const SocketPath = "/var/run/prysm/mesh.sock"
