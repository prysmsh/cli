package api

import (
	"context"
	"fmt"
	"time"
)

type EdgeDomain struct {
	ID             uint       `json:"id"`
	OrganizationID uint       `json:"organization_id"`
	ClusterID      uint       `json:"cluster_id"`
	Domain         string     `json:"domain"`
	UpstreamTarget string     `json:"upstream_target"`
	UpstreamMode   string     `json:"upstream_mode"`
	Status         string     `json:"status"`
	CertExpiresAt  *time.Time `json:"cert_expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type EdgeRule struct {
	ID              uint   `json:"id"`
	DomainID        uint   `json:"domain_id"`
	Name            string `json:"name"`
	Priority        int    `json:"priority"`
	MatchExpression string `json:"match_expression"`
	Action          string `json:"action"`
	Enabled         bool   `json:"enabled"`
}

type EdgeDNSRecord struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type CreateEdgeDomainResponse struct {
	Domain    EdgeDomain `json:"domain"`
	NSRecords []string   `json:"ns_records"`
	Message   string     `json:"message"`
}

func (c *Client) CreateEdgeDomain(ctx context.Context, domain, upstream string, clusterID uint, mode string) (*CreateEdgeDomainResponse, error) {
	payload := map[string]interface{}{
		"domain":          domain,
		"upstream_target": upstream,
		"cluster_id":      clusterID,
	}
	if mode != "" {
		payload["upstream_mode"] = mode
	}
	var resp CreateEdgeDomainResponse
	if _, err := c.Do(ctx, "POST", "/edge/domains", payload, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListEdgeDomains(ctx context.Context) ([]EdgeDomain, error) {
	var resp struct {
		Domains []EdgeDomain `json:"domains"`
	}
	if _, err := c.Do(ctx, "GET", "/edge/domains", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Domains, nil
}

func (c *Client) DeleteEdgeDomain(ctx context.Context, domainID uint) error {
	_, err := c.Do(ctx, "DELETE", fmt.Sprintf("/edge/domains/%d", domainID), nil, nil)
	return err
}

func (c *Client) UpdateEdgeDomainUpstream(ctx context.Context, domainID uint, upstream string) error {
	payload := map[string]string{"upstream_target": upstream}
	_, err := c.Do(ctx, "PUT", fmt.Sprintf("/edge/domains/%d/upstream", domainID), payload, nil)
	return err
}

func (c *Client) GetEdgeDomainStatus(ctx context.Context, domainID uint) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if _, err := c.Do(ctx, "GET", fmt.Sprintf("/edge/domains/%d/status", domainID), nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CreateEdgeRule(ctx context.Context, domainID uint, name, matchExpr, action string, priority int, actionConfig map[string]interface{}) (*EdgeRule, error) {
	payload := map[string]interface{}{
		"name":             name,
		"match_expression": matchExpr,
		"action":           action,
		"priority":         priority,
	}
	if actionConfig != nil {
		payload["action_config"] = actionConfig
	}
	var resp struct {
		Rule EdgeRule `json:"rule"`
	}
	if _, err := c.Do(ctx, "POST", fmt.Sprintf("/edge/domains/%d/rules", domainID), payload, &resp); err != nil {
		return nil, err
	}
	return &resp.Rule, nil
}

func (c *Client) ListEdgeRules(ctx context.Context, domainID uint) ([]EdgeRule, error) {
	var resp struct {
		Rules []EdgeRule `json:"rules"`
	}
	if _, err := c.Do(ctx, "GET", fmt.Sprintf("/edge/domains/%d/rules", domainID), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Rules, nil
}

func (c *Client) DeleteEdgeRule(ctx context.Context, domainID, ruleID uint) error {
	_, err := c.Do(ctx, "DELETE", fmt.Sprintf("/edge/domains/%d/rules/%d", domainID, ruleID), nil, nil)
	return err
}

func (c *Client) AddEdgeDNSRecord(ctx context.Context, domainID uint, recordType, value string) (*EdgeDNSRecord, error) {
	payload := map[string]string{"type": recordType, "value": value}
	var resp struct {
		Record EdgeDNSRecord `json:"record"`
	}
	if _, err := c.Do(ctx, "POST", fmt.Sprintf("/edge/domains/%d/dns", domainID), payload, &resp); err != nil {
		return nil, err
	}
	return &resp.Record, nil
}

func (c *Client) ListEdgeDNSRecords(ctx context.Context, domainID uint) ([]EdgeDNSRecord, error) {
	var resp struct {
		Records []EdgeDNSRecord `json:"records"`
	}
	if _, err := c.Do(ctx, "GET", fmt.Sprintf("/edge/domains/%d/dns", domainID), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Records, nil
}

func (c *Client) DeleteEdgeDNSRecord(ctx context.Context, domainID uint, recordID string) error {
	_, err := c.Do(ctx, "DELETE", fmt.Sprintf("/edge/domains/%d/dns/%s", domainID, recordID), nil, nil)
	return err
}
