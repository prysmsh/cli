package api

import (
	"context"
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

// ExpiresAt returns the token expiry as time.
func (lr *LoginResponse) ExpiresAt() time.Time {
	if lr == nil || lr.ExpiresAtUnix == 0 {
		return time.Time{}
	}
	return time.Unix(lr.ExpiresAtUnix, 0)
}
