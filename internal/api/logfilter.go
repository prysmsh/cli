package api

import (
	"context"
	"fmt"
	"net/url"
)

// LogFilterLabel is a single labeled log message for training.
type LogFilterLabel struct {
	ID        uint   `json:"id"`
	Message   string `json:"message"`
	Label     string `json:"label"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
}

// LogFilterListResponse is the response from GET /log-filter/labels.
type LogFilterListResponse struct {
	Labels []LogFilterLabel `json:"labels"`
	Total  int64            `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

// LogFilterExportResponse is the response from GET /log-filter/labels/export.
type LogFilterExportResponse struct {
	Labels []LogFilterLabel `json:"labels"`
}

// LogFilterLabelsList fetches labels (paginated).
func (c *Client) LogFilterLabelsList(ctx context.Context, limit, offset int) (*LogFilterListResponse, error) {
	params := url.Values{}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", offset))
	}
	endpoint := "/log-filter/labels"
	if q := params.Encode(); q != "" {
		endpoint += "?" + q
	}
	var resp LogFilterListResponse
	if _, err := c.Do(ctx, "GET", endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LogFilterLabelsExport fetches all labels for the org (for training).
func (c *Client) LogFilterLabelsExport(ctx context.Context) (*LogFilterExportResponse, error) {
	var resp LogFilterExportResponse
	if _, err := c.Do(ctx, "GET", "/log-filter/labels/export", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LogFilterLabelCreate creates a single label.
func (c *Client) LogFilterLabelCreate(ctx context.Context, message, label, source string) error {
	payload := map[string]string{"message": message, "label": label}
	if source != "" {
		payload["source"] = source
	}
	_, err := c.Do(ctx, "POST", "/log-filter/labels", payload, nil)
	return err
}

// LogFilterLabelBatchItem is one item for batch create.
type LogFilterLabelBatchItem struct {
	Message string `json:"message"`
	Label   string `json:"label"`
	Source  string `json:"source,omitempty"`
}

// LogFilterLabelsCreateBatch creates multiple labels.
func (c *Client) LogFilterLabelsCreateBatch(ctx context.Context, labels []LogFilterLabelBatchItem) error {
	payload := map[string]interface{}{"labels": labels}
	_, err := c.Do(ctx, "POST", "/log-filter/labels/batch", payload, nil)
	return err
}
