package api

import (
	"context"
	"net/url"
	"time"
)

// LoginRequest holds credentials for authentication.
type LoginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPCode   string `json:"totp_code,omitempty"`
	BackupCode string `json:"backup_code,omitempty"`
}

// LoginResponse represents the payload from /auth/login.
type LoginResponse struct {
	Message        string            `json:"message"`
	User           SessionUser       `json:"user"`
	Organization   SessionOrg        `json:"organization"`
	SessionID      string            `json:"session_id"`
	CSRFToken      string            `json:"csrf_token"`
	ExpiresAtUnix  int64             `json:"expires_at"`
	Token          string            `json:"token"`
	RefreshToken   string            `json:"refresh_token,omitempty"`
	RefreshExpires int64             `json:"refresh_expires_at,omitempty"`
	Features       map[string]string `json:"features,omitempty"`
}

// SessionUser is the user info embedded within login response.
type SessionUser struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	MFAEnabled bool   `json:"mfa_enabled"`
}

// SessionOrg identifies the active organization context.
type SessionOrg struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Login authenticates with the control plane.
func (c *Client) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	var resp LoginResponse
	if _, err := c.Do(ctx, "POST", "/auth/login", req, &resp); err != nil {
		return nil, err
	}
	c.SetToken(resp.Token)
	return &resp, nil
}

// Logout revokes tokens server-side.
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.Do(ctx, "POST", "/auth/logout", nil, nil)
	return err
}

// ProfileResponse is the response from GET /profile.
type ProfileResponse struct {
	User           ProfileUser   `json:"user"`
	Organizations  []ProfileOrg  `json:"organizations"`
	ApprovalStatus string        `json:"approval_status"`
}

// ProfileUser contains user info from the profile endpoint.
type ProfileUser struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	Role           string `json:"role"`
	EmailVerified  bool   `json:"email_verified"`
	MFAEnabled     bool   `json:"mfa_enabled"`
	ApprovalStatus string `json:"approval_status"`
}

// ProfileOrg contains organization info from the profile endpoint.
type ProfileOrg struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// GetProfile fetches the current user's profile (requires token).
func (c *Client) GetProfile(ctx context.Context) (*ProfileResponse, error) {
	var resp ProfileResponse
	if _, err := c.Do(ctx, "GET", "/profile", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DERPTunnelTokenResponse is the response from GET /auth/derp-tunnel-token.
type DERPTunnelTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// GetDERPTunnelToken fetches a signed DERP tunnel JWT (org binding cryptographically enforced).
// deviceID is optional; when provided, it is embedded in the token for tunnel target lookup.
func (c *Client) GetDERPTunnelToken(ctx context.Context, deviceID string) (*DERPTunnelTokenResponse, error) {
	endpoint := "/auth/derp-tunnel-token"
	if deviceID != "" {
		endpoint += "?device_id=" + url.QueryEscape(deviceID)
	}
	var resp DERPTunnelTokenResponse
	if _, err := c.Do(ctx, "GET", endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ExpiresAt returns the token expiry as time.
func (lr *LoginResponse) ExpiresAt() time.Time {
	if lr == nil || lr.ExpiresAtUnix == 0 {
		return time.Time{}
	}
	return time.Unix(lr.ExpiresAtUnix, 0)
}
