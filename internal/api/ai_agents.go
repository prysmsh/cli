package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// AIAgent describes a deployed AI agent.
type AIAgent struct {
	ID                uint            `json:"id"`
	OrganizationID    uint            `json:"organization_id"`
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	Type              string          `json:"type"`
	Runtime           string          `json:"runtime"`
	ClusterID         *uint           `json:"cluster_id"`
	Config            json.RawMessage `json:"config"`
	Status            string          `json:"status"`
	StatusMessage     string          `json:"status_message"`
	EndpointURL       string          `json:"endpoint_url"`
	Replicas          int             `json:"replicas"`
	ReadyReplicas     int             `json:"ready_replicas"`
	LastReconcileTime *time.Time      `json:"last_reconcile_time"`
	Tags              []string        `json:"tags"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// AIAgentCreateRequest encapsulates payload for AI agent creation.
type AIAgentCreateRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Type        string          `json:"type"`
	Runtime     string          `json:"runtime"`
	ClusterID   *uint           `json:"cluster_id,omitempty"`
	Config      json.RawMessage `json:"config,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Replicas    int             `json:"replicas,omitempty"`
}

// ListAIAgents returns AI agents for the authenticated organization.
func (c *Client) ListAIAgents(ctx context.Context) ([]AIAgent, error) {
	var resp struct {
		Agents []AIAgent `json:"agents"`
		Total  int       `json:"total"`
	}
	if _, err := c.Do(ctx, "GET", "/ai-agents", nil, &resp); err != nil {
		return nil, err
	}
	if resp.Agents == nil {
		return []AIAgent{}, nil
	}
	return resp.Agents, nil
}

// GetAIAgent returns a single AI agent by ID.
func (c *Client) GetAIAgent(ctx context.Context, id uint) (*AIAgent, error) {
	var agent AIAgent
	if _, err := c.Do(ctx, "GET", fmt.Sprintf("/ai-agents/%d", id), nil, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// CreateAIAgent creates a new AI agent.
func (c *Client) CreateAIAgent(ctx context.Context, req AIAgentCreateRequest) (*AIAgent, error) {
	var agent AIAgent
	if _, err := c.Do(ctx, "POST", "/ai-agents", req, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// DeployAIAgent initiates deployment of an AI agent.
func (c *Client) DeployAIAgent(ctx context.Context, id uint) error {
	_, err := c.Do(ctx, "POST", fmt.Sprintf("/ai-agents/%d/deploy", id), nil, nil)
	return err
}

// UndeployAIAgent undeploys an AI agent.
func (c *Client) UndeployAIAgent(ctx context.Context, id uint) error {
	_, err := c.Do(ctx, "POST", fmt.Sprintf("/ai-agents/%d/undeploy", id), nil, nil)
	return err
}

// DeleteAIAgent deletes an AI agent.
func (c *Client) DeleteAIAgent(ctx context.Context, id uint) error {
	_, err := c.Do(ctx, "DELETE", fmt.Sprintf("/ai-agents/%d", id), nil, nil)
	return err
}

// GetAIAgentLogs returns recent log lines for an agent.
func (c *Client) GetAIAgentLogs(ctx context.Context, id uint, tail int) ([]string, error) {
	endpoint := fmt.Sprintf("/ai-agents/%d/logs?tail=%d", id, tail)
	var resp struct {
		Logs []struct {
			Line      string    `json:"line"`
			Source    string     `json:"source"`
			Timestamp time.Time `json:"timestamp"`
		} `json:"logs"`
	}
	if _, err := c.Do(ctx, "GET", endpoint, nil, &resp); err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range resp.Logs {
		lines = append(lines, fmt.Sprintf("[%s] %s", l.Timestamp.Format("15:04:05"), l.Line))
	}
	return lines, nil
}

// ParseAIAgentID parses a string agent ID.
func ParseAIAgentID(s string) (uint, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid agent ID: %w", err)
	}
	return uint(id), nil
}
