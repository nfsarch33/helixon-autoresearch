package autoresearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultEngramURL = "http://127.0.0.1:<port>"
	defaultAppID     = "autoresearch"
	defaultUserID    = "nfsarch33"
)

// Memory represents a single Engram memory entry.
type Memory struct {
	ID        string                 `json:"id"`
	Memory    string                 `json:"memory"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt string                 `json:"created_at,omitempty"`
	UpdatedAt string                 `json:"updated_at,omitempty"`
}

// EngramClient defines the interface for Engram memory operations.
type EngramClient interface {
	AddExperimentResult(ctx context.Context, result ExperimentResult) error
	SearchRelatedExperiments(ctx context.Context, query string, limit int) ([]Memory, error)
	GetExperimentHistory(ctx context.Context, experimentID string) ([]Memory, error)
}

// HTTPEngramClient implements EngramClient via the Engram HTTP API.
type HTTPEngramClient struct {
	baseURL    string
	appID      string
	userID     string
	httpClient *http.Client
}

// EngramClientConfig configures the HTTP Engram client.
type EngramClientConfig struct {
	BaseURL string
	AppID   string
	UserID  string
	Timeout time.Duration
}

// NewHTTPEngramClient creates a new Engram HTTP client.
func NewHTTPEngramClient(cfg EngramClientConfig) *HTTPEngramClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultEngramURL
	}
	if cfg.AppID == "" {
		cfg.AppID = defaultAppID
	}
	if cfg.UserID == "" {
		cfg.UserID = defaultUserID
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &HTTPEngramClient{
		baseURL: cfg.BaseURL,
		appID:   cfg.AppID,
		userID:  cfg.UserID,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *HTTPEngramClient) AddExperimentResult(ctx context.Context, result ExperimentResult) error {
	memoryText, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal experiment result: %w", err)
	}

	body := map[string]interface{}{
		"messages": []map[string]string{
			{"role": "user", "content": string(memoryText)},
		},
		"user_id": c.userID,
		"metadata": map[string]interface{}{
			"experiment_id":   result.ID,
			"experiment_name": result.Name,
			"status":          string(result.Status),
			"phase":           result.Phase.String(),
			"app_id":          c.appID,
			"job_id":          JobIDFor(result.Name, result.Hypothesis),
		},
	}

	return c.doPost(ctx, "/v1/memories/", body)
}

func (c *HTTPEngramClient) SearchRelatedExperiments(ctx context.Context, query string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 10
	}

	body := map[string]interface{}{
		"query":   query,
		"user_id": c.userID,
		"limit":   limit,
	}

	return c.doSearch(ctx, "/v1/memories/search/", body)
}

func (c *HTTPEngramClient) GetExperimentHistory(ctx context.Context, experimentID string) ([]Memory, error) {
	query := fmt.Sprintf("experiment_id:%s", experimentID)
	return c.SearchRelatedExperiments(ctx, query, 50)
}

func (c *HTTPEngramClient) doPost(ctx context.Context, path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("engram POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("engram POST %s returned %d: %s", path, resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *HTTPEngramClient) doSearch(ctx context.Context, path string, body interface{}) ([]Memory, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("engram POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("engram POST %s returned %d: %s", path, resp.StatusCode, string(respBody))
	}

	var result struct {
		Results []Memory `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	return result.Results, nil
}
