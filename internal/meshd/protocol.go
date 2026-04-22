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

// PeerInfo describes a mesh peer for display purposes.
type PeerInfo struct {
	Name      string `json:"name"`
	OverlayIP string `json:"overlay_ip"`
	Endpoint  string `json:"endpoint"`
}

// Response is a reply from daemon to CLI.
type Response struct {
	Status    string     `json:"status"`              // "ok", "connected", "disconnected", "error"
	OverlayIP string     `json:"overlay_ip,omitempty"`
	Interface string     `json:"interface,omitempty"`
	PeerCount int        `json:"peer_count,omitempty"`
	Peers     []PeerInfo `json:"peers,omitempty"`
	Uptime    int64      `json:"uptime,omitempty"`     // seconds
	TxBytes   int64      `json:"tx_bytes,omitempty"`
	RxBytes   int64      `json:"rx_bytes,omitempty"`
	Error     string     `json:"error,omitempty"`
	WGConfig  *WGConfig  `json:"wg_config,omitempty"`  // returned by "wg_config" command
}

// WGConfig contains WireGuard tunnel configuration for the Network Extension.
type WGConfig struct {
	PrivateKey string              `json:"private_key"` // base64
	OverlayIP  string              `json:"overlay_ip"`
	DERPURL    string              `json:"derp_url"`
	Peers      []map[string]string `json:"peers"`
}

const SocketPath = "/var/run/prysm/mesh.sock"
