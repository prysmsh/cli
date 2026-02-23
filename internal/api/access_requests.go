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

// AccessRequest represents a policy-gated access request.
type AccessRequest struct {
	ID           FlexibleID             `json:"id"`
	RequestID    string                 `json:"request_id,omitempty"`
	Status       string                 `json:"status"`
	Resource     string                 `json:"resource,omitempty"`
	ResourceType string                 `json:"resource_type,omitempty"`
	Reason       string                 `json:"reason,omitempty"`
	ExpiresAt    *time.Time             `json:"expires_at,omitempty"`
	CreatedAt    *time.Time             `json:"created_at,omitempty"`
	UpdatedAt    *time.Time             `json:"updated_at,omitempty"`
	RequestedBy  string                 `json:"requested_by,omitempty"`
	ReviewedBy   string                 `json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time             `json:"reviewed_at,omitempty"`
	ReviewNote   string                 `json:"review_note,omitempty"`
	AuditFields  map[string]string      `json:"audit_fields,omitempty"`
	PolicyChecks map[string]interface{} `json:"policy_checks,omitempty"`
}

// Identifier returns the best identifier for display and follow-up actions.
func (r AccessRequest) Identifier() string {
	if strings.TrimSpace(r.RequestID) != "" {
		return strings.TrimSpace(r.RequestID)
	}
	return r.ID.String()
}

// AccessRequestCreateRequest is the payload to create a request.
type AccessRequestCreateRequest struct {
	Resource     string            `json:"resource"`
	ResourceType string            `json:"resource_type,omitempty"`
	Reason       string            `json:"reason"`
	ExpiresAt    *time.Time        `json:"expires_at"`
	AuditFields  map[string]string `json:"audit_fields,omitempty"`
}

// AccessRequestListOptions controls list filtering.
type AccessRequestListOptions struct {
	Status       string
	ResourceType string
	Mine         bool
	Limit        int
}

// CreateAccessRequest creates a new access request with required reason/expiry metadata.
func (c *Client) CreateAccessRequest(ctx context.Context, req AccessRequestCreateRequest) (*AccessRequest, error) {
	if strings.TrimSpace(req.Resource) == "" {
		return nil, fmt.Errorf("resource is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, fmt.Errorf("reason is required")
	}
	if req.ExpiresAt == nil || req.ExpiresAt.IsZero() {
		return nil, fmt.Errorf("expires_at is required")
	}

	return doAccessRequestWithFallback(ctx, c, "POST", []string{"/access/requests", "/requests"}, req)
}

// ListAccessRequests returns access requests with optional filters.
func (c *Client) ListAccessRequests(ctx context.Context, opts AccessRequestListOptions) ([]AccessRequest, error) {
	q := url.Values{}
	if s := strings.TrimSpace(opts.Status); s != "" {
		q.Set("status", s)
	}
	if rt := strings.TrimSpace(opts.ResourceType); rt != "" {
		q.Set("resource_type", rt)
	}
	if opts.Mine {
		q.Set("mine", "true")
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}

	endpoint := "/access/requests"
	if encoded := q.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	fallback := "/requests"
	if encoded := q.Encode(); encoded != "" {
		fallback += "?" + encoded
	}

	return doAccessRequestListWithFallback(ctx, c, []string{endpoint, fallback})
}

// GetAccessRequest returns a request by ID.
func (c *Client) GetAccessRequest(ctx context.Context, id string) (*AccessRequest, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("request id is required")
	}
	escaped := url.PathEscape(id)
	return doAccessRequestWithFallback(ctx, c, "GET", []string{
		"/access/requests/" + escaped,
		"/requests/" + escaped,
	}, nil)
}

// ApproveAccessRequest approves a request.
func (c *Client) ApproveAccessRequest(ctx context.Context, id string, note string) (*AccessRequest, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("request id is required")
	}

	payload := map[string]string{}
	if trimmed := strings.TrimSpace(note); trimmed != "" {
		payload["note"] = trimmed
	}

	escaped := url.PathEscape(id)
	return doAccessRequestWithFallback(ctx, c, "POST", []string{
		"/access/requests/" + escaped + "/approve",
		"/requests/" + escaped + "/approve",
	}, payload)
}

// DenyAccessRequest denies a request with a required reason.
func (c *Client) DenyAccessRequest(ctx context.Context, id string, reason string) (*AccessRequest, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("request id is required")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("reason is required")
	}

	payload := map[string]string{"reason": reason}
	escaped := url.PathEscape(id)
	return doAccessRequestWithFallback(ctx, c, "POST", []string{
		"/access/requests/" + escaped + "/deny",
		"/requests/" + escaped + "/deny",
	}, payload)
}

func doAccessRequestWithFallback(ctx context.Context, c *Client, method string, endpoints []string, payload interface{}) (*AccessRequest, error) {
	var lastErr error
	for i, endpoint := range endpoints {
		var raw json.RawMessage
		if _, err := c.Do(ctx, method, endpoint, payload, &raw); err != nil {
			if i < len(endpoints)-1 && isEndpointUnavailable(err) {
				lastErr = err
				continue
			}
			return nil, err
		}
		req, err := decodeAccessRequest(raw)
		if err != nil {
			return nil, err
		}
		return req, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("request failed")
}

func doAccessRequestListWithFallback(ctx context.Context, c *Client, endpoints []string) ([]AccessRequest, error) {
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
		return decodeAccessRequestList(raw)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("request failed")
}

func decodeAccessRequest(raw json.RawMessage) (*AccessRequest, error) {
	var wrapped struct {
		Request       *AccessRequest `json:"request"`
		AccessRequest *AccessRequest `json:"access_request"`
		Data          *AccessRequest `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		if wrapped.Request != nil {
			return wrapped.Request, nil
		}
		if wrapped.AccessRequest != nil {
			return wrapped.AccessRequest, nil
		}
		if wrapped.Data != nil {
			return wrapped.Data, nil
		}
	}

	var direct AccessRequest
	if err := json.Unmarshal(raw, &direct); err != nil {
		return nil, fmt.Errorf("decode access request: %w", err)
	}
	return &direct, nil
}

func decodeAccessRequestList(raw json.RawMessage) ([]AccessRequest, error) {
	var wrapped struct {
		Requests       []AccessRequest `json:"requests"`
		AccessRequests []AccessRequest `json:"access_requests"`
		Items          []AccessRequest `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		switch {
		case wrapped.Requests != nil:
			return wrapped.Requests, nil
		case wrapped.AccessRequests != nil:
			return wrapped.AccessRequests, nil
		case wrapped.Items != nil:
			return wrapped.Items, nil
		}
	}

	var direct []AccessRequest
	if err := json.Unmarshal(raw, &direct); err != nil {
		return nil, fmt.Errorf("decode access requests: %w", err)
	}
	if direct == nil {
		return []AccessRequest{}, nil
	}
	return direct, nil
}
