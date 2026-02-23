package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AccessSession represents an audited access session.
type AccessSession struct {
	ID              FlexibleID             `json:"id"`
	SessionID       string                 `json:"session_id,omitempty"`
	RequestID       string                 `json:"request_id,omitempty"`
	Status          string                 `json:"status,omitempty"`
	Type            string                 `json:"type,omitempty"`
	Protocol        string                 `json:"protocol,omitempty"`
	Resource        string                 `json:"resource,omitempty"`
	ResourceType    string                 `json:"resource_type,omitempty"`
	User            string                 `json:"user,omitempty"`
	Reason          string                 `json:"reason,omitempty"`
	StartedAt       *time.Time             `json:"started_at,omitempty"`
	EndedAt         *time.Time             `json:"ended_at,omitempty"`
	DurationSeconds int64                  `json:"duration_seconds,omitempty"`
	AuditFields     map[string]string      `json:"audit_fields,omitempty"`
	PolicyChecks    map[string]interface{} `json:"policy_checks,omitempty"`
	Recording       map[string]interface{} `json:"recording,omitempty"`
}

// Identifier returns a display identifier for a session.
func (s AccessSession) Identifier() string {
	if strings.TrimSpace(s.SessionID) != "" {
		return strings.TrimSpace(s.SessionID)
	}
	return s.ID.String()
}

// AccessSessionListOptions controls list filtering.
type AccessSessionListOptions struct {
	Status       string
	Type         string
	ResourceType string
	User         string
	Limit        int
}

// SessionReplayEvent represents a single replay event from a recorded session.
type SessionReplayEvent struct {
	Timestamp *time.Time             `json:"timestamp,omitempty"`
	Actor     string                 `json:"actor,omitempty"`
	Type      string                 `json:"type,omitempty"`
	Message   string                 `json:"message,omitempty"`
	Command   string                 `json:"command,omitempty"`
	ExitCode  *int                   `json:"exit_code,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// SessionReplay is the replay/export payload for a session.
type SessionReplay struct {
	SessionID   string                 `json:"session_id,omitempty"`
	Format      string                 `json:"format,omitempty"`
	GeneratedAt *time.Time             `json:"generated_at,omitempty"`
	DownloadURL string                 `json:"download_url,omitempty"`
	Transcript  string                 `json:"transcript,omitempty"`
	Events      []SessionReplayEvent   `json:"events,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ListAccessSessions lists audited sessions.
func (c *Client) ListAccessSessions(ctx context.Context, opts AccessSessionListOptions) ([]AccessSession, error) {
	q := url.Values{}
	if s := strings.TrimSpace(opts.Status); s != "" {
		q.Set("status", s)
	}
	if t := strings.TrimSpace(opts.Type); t != "" {
		q.Set("type", t)
	}
	if rt := strings.TrimSpace(opts.ResourceType); rt != "" {
		q.Set("resource_type", rt)
	}
	if u := strings.TrimSpace(opts.User); u != "" {
		q.Set("user", u)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}

	primary := "/sessions"
	if encoded := q.Encode(); encoded != "" {
		primary += "?" + encoded
	}

	fallback := "/access/sessions"
	if encoded := q.Encode(); encoded != "" {
		fallback += "?" + encoded
	}

	return doSessionListWithFallback(ctx, c, []string{primary, fallback})
}

// GetAccessSession fetches one session by identifier.
func (c *Client) GetAccessSession(ctx context.Context, id string) (*AccessSession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("session id is required")
	}
	escaped := url.PathEscape(id)
	return doSessionWithFallback(ctx, c, []string{
		"/sessions/" + escaped,
		"/access/sessions/" + escaped,
	})
}

// ReplayAccessSession fetches a replay/export payload for a session.
func (c *Client) ReplayAccessSession(ctx context.Context, id string, format string) (*SessionReplay, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("session id is required")
	}
	escaped := url.PathEscape(id)

	q := url.Values{}
	if f := strings.TrimSpace(format); f != "" {
		q.Set("format", f)
	}

	withQuery := func(path string) string {
		if encoded := q.Encode(); encoded != "" {
			return path + "?" + encoded
		}
		return path
	}

	return doReplayWithFallback(ctx, c, []string{
		withQuery("/sessions/" + escaped + "/replay"),
		withQuery("/sessions/" + escaped + "/export"),
		withQuery("/access/sessions/" + escaped + "/replay"),
		withQuery("/access/sessions/" + escaped + "/export"),
	})
}

func doSessionWithFallback(ctx context.Context, c *Client, endpoints []string) (*AccessSession, error) {
	var lastErr error
	for i, endpoint := range endpoints {
		var raw json.RawMessage
		if _, err := c.Do(ctx, "GET", endpoint, nil, &raw); err != nil {
			if i < len(endpoints)-1 && isEndpointUnavailable(err) {
				lastErr = err
				continue
			}
			return nil, err
		}
		return decodeSession(raw)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("request failed")
}

func doSessionListWithFallback(ctx context.Context, c *Client, endpoints []string) ([]AccessSession, error) {
	var lastErr error
	for i, endpoint := range endpoints {
		var raw json.RawMessage
		if _, err := c.Do(ctx, "GET", endpoint, nil, &raw); err != nil {
			if i < len(endpoints)-1 && isEndpointUnavailable(err) {
				lastErr = err
				continue
			}
			return nil, err
		}
		return decodeSessionList(raw)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("request failed")
}

func doReplayWithFallback(ctx context.Context, c *Client, endpoints []string) (*SessionReplay, error) {
	var lastErr error
	for i, endpoint := range endpoints {
		var raw json.RawMessage
		if _, err := c.Do(ctx, "GET", endpoint, nil, &raw); err != nil {
			if i < len(endpoints)-1 && isEndpointUnavailable(err) {
				lastErr = err
				continue
			}
			return nil, err
		}
		return decodeReplay(raw)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("request failed")
}

func decodeSession(raw json.RawMessage) (*AccessSession, error) {
	var wrapped struct {
		Session *AccessSession `json:"session"`
		Data    *AccessSession `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		if wrapped.Session != nil {
			return wrapped.Session, nil
		}
		if wrapped.Data != nil {
			return wrapped.Data, nil
		}
	}

	var direct AccessSession
	if err := json.Unmarshal(raw, &direct); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	return &direct, nil
}

func decodeSessionList(raw json.RawMessage) ([]AccessSession, error) {
	var wrapped struct {
		Sessions []AccessSession `json:"sessions"`
		Items    []AccessSession `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		switch {
		case wrapped.Sessions != nil:
			return wrapped.Sessions, nil
		case wrapped.Items != nil:
			return wrapped.Items, nil
		}
	}

	var direct []AccessSession
	if err := json.Unmarshal(raw, &direct); err != nil {
		return nil, fmt.Errorf("decode sessions: %w", err)
	}
	if direct == nil {
		return []AccessSession{}, nil
	}
	return direct, nil
}

func decodeReplay(raw json.RawMessage) (*SessionReplay, error) {
	var wrapped struct {
		Replay *SessionReplay `json:"replay"`
		Data   *SessionReplay `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		if wrapped.Replay != nil {
			return wrapped.Replay, nil
		}
		if wrapped.Data != nil {
			return wrapped.Data, nil
		}
	}

	var direct SessionReplay
	if err := json.Unmarshal(raw, &direct); err != nil {
		return nil, fmt.Errorf("decode replay: %w", err)
	}
	return &direct, nil
}
