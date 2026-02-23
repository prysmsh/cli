package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SSHConnectRequest requests a policy-checked SSH connection.
type SSHConnectRequest struct {
	Target    string   `json:"target"`
	Reason    string   `json:"reason"`
	RequestID string   `json:"request_id,omitempty"`
	Port      int      `json:"port,omitempty"`
	Command   []string `json:"command,omitempty"`
	DryRun    bool     `json:"dry_run,omitempty"`
}

// SSHSessionInfo captures the issued SSH access session metadata.
type SSHSessionInfo struct {
	ID        FlexibleID `json:"id"`
	SessionID string     `json:"session_id,omitempty"`
	Status    string     `json:"status,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
}

// SSHConnectionInfo contains the SSH client connection details.
type SSHConnectionInfo struct {
	Target       string   `json:"target,omitempty"`
	Host         string   `json:"host,omitempty"`
	User         string   `json:"user,omitempty"`
	Port         int      `json:"port,omitempty"`
	ProxyCommand string   `json:"proxy_command,omitempty"`
	IdentityFile string   `json:"identity_file,omitempty"`
	Options      []string `json:"options,omitempty"`
	SSHArgs      []string `json:"ssh_args,omitempty"`
}

// SSHConnectResponse contains issued session details and enforced policy metadata.
type SSHConnectResponse struct {
	Session      SSHSessionInfo         `json:"session"`
	Connection   SSHConnectionInfo      `json:"connection"`
	PolicyChecks map[string]interface{} `json:"policy_checks,omitempty"`
	Recording    map[string]interface{} `json:"recording,omitempty"`
}

// ConnectSSH authorizes and prepares an SSH connection through the control plane.
func (c *Client) ConnectSSH(ctx context.Context, req SSHConnectRequest) (*SSHConnectResponse, error) {
	if strings.TrimSpace(req.Target) == "" {
		return nil, fmt.Errorf("target is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, fmt.Errorf("reason is required")
	}

	endpoints := []string{"/connect/ssh", "/ssh/connect"}
	var lastErr error

	for i, endpoint := range endpoints {
		var raw json.RawMessage
		if _, err := c.Do(ctx, "POST", endpoint, req, &raw); err != nil {
			if i < len(endpoints)-1 && isEndpointUnavailable(err) {
				lastErr = err
				continue
			}
			return nil, err
		}
		return decodeSSHConnectResponse(raw)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("request failed")
}

func decodeSSHConnectResponse(raw json.RawMessage) (*SSHConnectResponse, error) {
	var resp SSHConnectResponse
	if err := json.Unmarshal(raw, &resp); err == nil && (resp.Connection.Target != "" || resp.Connection.Host != "" || resp.Session.SessionID != "") {
		return &resp, nil
	}

	var wrapped struct {
		Connection   SSHConnectionInfo      `json:"connection"`
		Session      SSHSessionInfo         `json:"session"`
		PolicyChecks map[string]interface{} `json:"policy_checks"`
		Recording    map[string]interface{} `json:"recording"`
		Host         string                 `json:"host"`
		User         string                 `json:"user"`
		Port         int                    `json:"port"`
		Target       string                 `json:"target"`
		SessionID    string                 `json:"session_id"`
		ProxyCommand string                 `json:"proxy_command"`
		IdentityFile string                 `json:"identity_file"`
		Options      []string               `json:"options"`
		SSHArgs      []string               `json:"ssh_args"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("decode ssh connect response: %w", err)
	}

	if wrapped.Connection.Target == "" {
		wrapped.Connection.Target = wrapped.Target
	}
	if wrapped.Connection.Host == "" {
		wrapped.Connection.Host = wrapped.Host
	}
	if wrapped.Connection.User == "" {
		wrapped.Connection.User = wrapped.User
	}
	if wrapped.Connection.Port == 0 {
		wrapped.Connection.Port = wrapped.Port
	}
	if wrapped.Connection.ProxyCommand == "" {
		wrapped.Connection.ProxyCommand = wrapped.ProxyCommand
	}
	if wrapped.Connection.IdentityFile == "" {
		wrapped.Connection.IdentityFile = wrapped.IdentityFile
	}
	if len(wrapped.Connection.Options) == 0 {
		wrapped.Connection.Options = wrapped.Options
	}
	if len(wrapped.Connection.SSHArgs) == 0 {
		wrapped.Connection.SSHArgs = wrapped.SSHArgs
	}
	if wrapped.Session.SessionID == "" {
		wrapped.Session.SessionID = wrapped.SessionID
	}

	return &SSHConnectResponse{
		Session:      wrapped.Session,
		Connection:   wrapped.Connection,
		PolicyChecks: wrapped.PolicyChecks,
		Recording:    wrapped.Recording,
	}, nil
}
