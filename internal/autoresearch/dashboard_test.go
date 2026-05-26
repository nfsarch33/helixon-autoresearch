package autoresearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDashboardHandler_ListExperiments(t *testing.T) {
	engram := &mockEngram{
		searchFunc: func(_ context.Context, _ string, _ int) ([]Memory, error) {
			return []Memory{
				{ID: "mem-1", Memory: "exp result 1"},
				{ID: "mem-2", Memory: "exp result 2"},
			}, nil
		},
	}

	loop := NewExperimentLoop(engram, nil)
	handler := NewDashboardHandler(loop, engram)

	req := httptest.NewRequest(http.MethodGet, "/api/autoresearch/experiments", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp["total"].(float64) != 2 {
		t.Fatalf("expected total 2, got %v", resp["total"])
	}
}

func TestDashboardHandler_GetExperiment(t *testing.T) {
	engram := &dashboardMockEngram{
		history: map[string][]Memory{
			"exp-123": {{ID: "m1", Memory: `{"id":"exp-123","name":"test"}`}},
		},
	}

	loop := NewExperimentLoop(engram, nil)
	handler := NewDashboardHandler(loop, engram)

	req := httptest.NewRequest(http.MethodGet, "/api/autoresearch/experiments/exp-123", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp["id"] != "exp-123" {
		t.Fatalf("expected id exp-123, got %v", resp["id"])
	}
}

func TestDashboardHandler_GetExperiment_NotFound(t *testing.T) {
	engram := &dashboardMockEngram{history: map[string][]Memory{}}

	loop := NewExperimentLoop(engram, nil)
	handler := NewDashboardHandler(loop, engram)

	req := httptest.NewRequest(http.MethodGet, "/api/autoresearch/experiments/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDashboardHandler_Metrics(t *testing.T) {
	engram := &mockEngram{}
	loop := NewExperimentLoop(engram, nil)
	loop.Metrics().RecordRun(StatusCompleted, 100)

	handler := NewDashboardHandler(loop, engram)

	req := httptest.NewRequest(http.MethodGet, "/api/autoresearch/metrics", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var snap MetricsSnapshot
	if err := json.NewDecoder(w.Body).Decode(&snap); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if snap.TotalRuns != 1 {
		t.Fatalf("expected 1 total run, got %d", snap.TotalRuns)
	}
}

func TestDashboardHandler_Compare(t *testing.T) {
	baseResult := `{"id":"b","name":"baseline","metrics":{"before":{},"after":{"accuracy":0.8}}}`
	candResult := `{"id":"c","name":"candidate","metrics":{"before":{},"after":{"accuracy":0.9}}}`

	engram := &dashboardMockEngram{
		history: map[string][]Memory{
			"b": {{ID: "m1", Memory: baseResult}},
			"c": {{ID: "m2", Memory: candResult}},
		},
	}

	loop := NewExperimentLoop(engram, nil)
	handler := NewDashboardHandler(loop, engram)

	req := httptest.NewRequest(http.MethodGet, "/api/autoresearch/compare?baseline=b&candidate=c", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result CompareResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if result.Winner != "candidate" {
		t.Fatalf("expected candidate to win, got %s", result.Winner)
	}
}

func TestDashboardHandler_Compare_MissingParams(t *testing.T) {
	engram := &mockEngram{}
	loop := NewExperimentLoop(engram, nil)
	handler := NewDashboardHandler(loop, engram)

	req := httptest.NewRequest(http.MethodGet, "/api/autoresearch/compare", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDashboardHandler_MethodNotAllowed(t *testing.T) {
	engram := &mockEngram{}
	loop := NewExperimentLoop(engram, nil)
	handler := NewDashboardHandler(loop, engram)

	req := httptest.NewRequest(http.MethodPost, "/api/autoresearch/experiments", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestDashboardHandler_NotFound(t *testing.T) {
	engram := &mockEngram{}
	loop := NewExperimentLoop(engram, nil)
	handler := NewDashboardHandler(loop, engram)

	req := httptest.NewRequest(http.MethodGet, "/api/autoresearch/unknown", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

type dashboardMockEngram struct {
	history map[string][]Memory
}

func (m *dashboardMockEngram) AddExperimentResult(_ context.Context, _ ExperimentResult) error {
	return nil
}

func (m *dashboardMockEngram) SearchRelatedExperiments(_ context.Context, _ string, _ int) ([]Memory, error) {
	return nil, nil
}

func (m *dashboardMockEngram) GetExperimentHistory(_ context.Context, experimentID string) ([]Memory, error) {
	if hist, ok := m.history[experimentID]; ok {
		return hist, nil
	}
	return nil, nil
}
