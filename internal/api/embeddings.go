package api

import (
	"context"
	"fmt"
)

// EmbedModel describes an available embedding model.
type EmbedModel struct {
	Name       string   `json:"name"`
	Backend    string   `json:"backend"`
	Dimensions int      `json:"dimensions"`
	DataTypes  []string `json:"data_types"`
	Healthy    bool     `json:"healthy"`
}

// EmbedSource describes an indexable data source with row count.
type EmbedSource struct {
	Name        string `json:"name"`
	Table       string `json:"table"`
	Description string `json:"description"`
	RowCount    int    `json:"row_count"`
}

// EmbedCollection describes a Qdrant collection.
type EmbedCollection struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	Source     string `json:"source"`
	Dimensions int    `json:"dimensions"`
	PointCount int64  `json:"point_count"`
}

// EmbedJob describes an embedding indexing job.
type EmbedJob struct {
	ID             int64  `json:"id"`
	OrganizationID uint   `json:"organization_id"`
	DataSource     string `json:"data_source"`
	ModelName      string `json:"model_name"`
	CollectionName string `json:"collection_name"`
	Status         string `json:"status"`
	TotalRecords   int    `json:"total_records"`
	Processed      int    `json:"processed"`
	FailedRecords  int    `json:"failed_records"`
	ErrorMessage   string `json:"error_message,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// EmbedJobCreateRequest is the payload for creating an indexing job.
type EmbedJobCreateRequest struct {
	DataSource string `json:"data_source"`
	ModelName  string `json:"model_name"`
}

// EmbedQueryResult is a single result from semantic search.
type EmbedQueryResult struct {
	Text       string  `json:"text"`
	Score      float64 `json:"score"`
	Source     string  `json:"source"`
	Collection string  `json:"collection"`
}

// ListEmbedModels returns available embedding models.
func (c *Client) ListEmbedModels(ctx context.Context) ([]EmbedModel, error) {
	var resp struct {
		Models []EmbedModel `json:"models"`
	}
	if _, err := c.Do(ctx, "GET", "/embed/models", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Models, nil
}

// ListEmbedSources returns indexable data sources with row counts.
func (c *Client) ListEmbedSources(ctx context.Context) ([]EmbedSource, error) {
	var resp struct {
		Sources []EmbedSource `json:"sources"`
	}
	if _, err := c.Do(ctx, "GET", "/embed/sources", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Sources, nil
}

// ListEmbedCollections returns all embedding collections.
func (c *Client) ListEmbedCollections(ctx context.Context) ([]EmbedCollection, error) {
	var resp struct {
		Collections []EmbedCollection `json:"collections"`
	}
	if _, err := c.Do(ctx, "GET", "/embed/collections", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Collections, nil
}

// ListEmbedJobs returns embedding jobs.
func (c *Client) ListEmbedJobs(ctx context.Context) ([]EmbedJob, error) {
	var resp struct {
		Jobs []EmbedJob `json:"jobs"`
	}
	if _, err := c.Do(ctx, "GET", "/embed/jobs", nil, &resp); err != nil {
		return nil, err
	}
	if resp.Jobs == nil {
		return []EmbedJob{}, nil
	}
	return resp.Jobs, nil
}

// GetEmbedJob returns a single embedding job.
func (c *Client) GetEmbedJob(ctx context.Context, id int64) (*EmbedJob, error) {
	var resp struct {
		Job EmbedJob `json:"job"`
	}
	if _, err := c.Do(ctx, "GET", fmt.Sprintf("/embed/jobs/%d", id), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Job, nil
}

// CreateEmbedJob creates a new indexing job.
func (c *Client) CreateEmbedJob(ctx context.Context, req EmbedJobCreateRequest) (*EmbedJob, error) {
	var resp struct {
		Job EmbedJob `json:"job"`
	}
	if _, err := c.Do(ctx, "POST", "/embed/jobs", req, &resp); err != nil {
		return nil, err
	}
	return &resp.Job, nil
}

// CancelEmbedJob cancels a running indexing job.
func (c *Client) CancelEmbedJob(ctx context.Context, id int64) error {
	_, err := c.Do(ctx, "POST", fmt.Sprintf("/embed/jobs/%d/cancel", id), nil, nil)
	return err
}

// DeleteEmbedCollection deletes a Qdrant collection.
func (c *Client) DeleteEmbedCollection(ctx context.Context, name string) error {
	_, err := c.Do(ctx, "DELETE", fmt.Sprintf("/embed/collections/%s", name), nil, nil)
	return err
}

// EmbedQuery performs semantic search.
func (c *Client) EmbedQuery(ctx context.Context, query string) ([]EmbedQueryResult, error) {
	var resp struct {
		Results []EmbedQueryResult `json:"results"`
		Count   int                `json:"count"`
	}
	if _, err := c.Do(ctx, "POST", "/embed/query", map[string]string{"query": query}, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}
