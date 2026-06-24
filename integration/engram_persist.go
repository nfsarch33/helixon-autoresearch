// Package integration connects the helixon-autoresearch experiment runner
// to the eval harness, the 10-stage academic research pipeline, and the
// Engram persistence layer on wsl1:8280.
//
// Flow:
//  1. The autoresearch runner creates an experiment (10-stage pipeline).
//  2. The eval harness executes tasks on Helixon agents with different
//     LLM backends and scores outputs via G-Eval.
//  3. The integration layer feeds eval tasks into the runner and collects
//     scored results.
//  4. Experiment metadata + eval results are persisted to Engram.
//  5. Results are tracked in SprintBoard (documented in README).
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// EngramPersistor persists eval experiment metadata to the Engram memory
// engine. The Engram instance verified in Sprint A runs on wsl1:8280.
type EngramPersistor struct {
	EngramURL string // e.g. http://100.119.90.30:8280
	AppID     string // e.g. autoresearch-eval
	UserID    string // e.g. nfsarch33
	client    *http.Client
}

// NewEngramPersistor constructs a persistor with sensible defaults.
func NewEngramPersistor(engramURL, appID, userID string) *EngramPersistor {
	if engramURL == "" {
		engramURL = "http://100.119.90.30:8280"
	}
	if appID == "" {
		appID = "autoresearch-eval"
	}
	if userID == "" {
		userID = "nfsarch33"
	}
	return &EngramPersistor{
		EngramURL: engramURL,
		AppID:     appID,
		UserID:    userID,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

// engramMemory is the Engram /memories POST body shape.
type engramMemory struct {
	Messages []engramMessage `json:"messages"`
	UserID   string          `json:"user_id"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

type engramMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SaveExperiment persists an eval experiment and its report to Engram.
// The full report JSON is stored as the memory content so downstream
// searches can recall the comparative matrix and per-result scores.
func (e *EngramPersistor) SaveExperiment(ctx context.Context, exp EvalExperiment) error {
	if exp.ID == "" {
		return fmt.Errorf("experiment ID is required")
	}
	payload, err := json.Marshal(exp)
	if err != nil {
		return fmt.Errorf("marshal experiment: %w", err)
	}

	body := engramMemory{
		Messages: []engramMessage{
			{Role: "user", Content: string(payload)},
		},
		UserID: e.UserID,
		Metadata: map[string]any{
			"app_id":          e.AppID,
			"experiment_id":   exp.ID,
			"experiment_name": exp.Name,
			"stage":           exp.CurrentStage,
			"verdict":         exp.Report.Summary.OverallVerdict,
			"best_backend":    exp.Report.Summary.BestBackend,
			"total_results":   exp.Report.Summary.TotalResults,
			"rubric_version":  exp.Report.RubricVersion,
		},
	}
	return e.postMemories(ctx, body)
}

// SearchExperiments recalls prior eval experiments from Engram by query.
func (e *EngramPersistor) SearchExperiments(ctx context.Context, query string, limit int) ([]EngramHit, error) {
	if limit <= 0 {
		limit = 10
	}
	body := map[string]any{
		"query":   query,
		"user_id": e.UserID,
		"limit":   limit,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal search: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.EngramURL+"/memories/search", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("engram search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("engram search returned %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Results []EngramHit `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	return result.Results, nil
}

// postMemories is the low-level POST to /memories. It matches the
// Engram wire contract used by the autoresearch engram_client.go.
func (e *EngramPersistor) postMemories(ctx context.Context, body engramMemory) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal memories body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.EngramURL+"/memories", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create memories request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("engram POST /memories: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("engram POST /memories returned %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

// EngramHit is a single recalled memory from a search.
type EngramHit struct {
	ID        string         `json:"id"`
	Memory    string         `json:"memory"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Score     float64        `json:"score,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
}

// Ping checks Engram reachability. Returns nil if the server responds.
func (e *EngramPersistor) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.EngramURL+"/", nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("engram ping: %w", err)
	}
	defer resp.Body.Close()
	// Engram returns 404 on GET / but that still proves the server is up.
	if resp.StatusCode == 404 || resp.StatusCode < 400 {
		return nil
	}
	return fmt.Errorf("engram returned %d", resp.StatusCode)
}
