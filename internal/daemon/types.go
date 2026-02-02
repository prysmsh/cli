package daemon

import "time"

// ApplyConfigRequest mirrors the mesh daemon apply payload.
type ApplyConfigRequest struct {
	Interface InterfaceConfig `json:"interface"`
	Peers     []PeerConfig    `json:"peers"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// InterfaceConfig describes the WireGuard interface configuration.
type InterfaceConfig struct {
	PrivateKey string   `json:"private_key"`
	Address    string   `json:"address"`
	DNS        []string `json:"dns,omitempty"`
	MTU        int      `json:"mtu,omitempty"`
}

// PeerConfig describes a WireGuard peer.
type PeerConfig struct {
	PublicKey   string   `json:"public_key"`
	Endpoint    string   `json:"endpoint,omitempty"`
	AllowedIPs  []string `json:"allowed_ips,omitempty"`
	Keepalive   int      `json:"persistent_keepalive,omitempty"`
}

// StatusResponse mirrors the daemon status payload.
type StatusResponse struct {
	InterfaceUp bool          `json:"interface_up"`
	LastApply   time.Time     `json:"last_apply"`
	PeerCount   int           `json:"peer_count"`
	Warnings    []string      `json:"warnings"`
	Peers       []PeerSummary `json:"peers,omitempty"`
}

// PeerSummary reports runtime metrics for a peer.
type PeerSummary struct {
	PublicKey     string `json:"public_key"`
	Endpoint      string `json:"endpoint"`
	LastHandshake string `json:"last_handshake"`
	BytesReceived int64  `json:"bytes_received"`
	BytesSent     int64  `json:"bytes_transmitted"`
}
