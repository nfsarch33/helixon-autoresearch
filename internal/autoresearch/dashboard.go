package autoresearch

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// DashboardHandler serves experiment data for the dashboard UI.
type DashboardHandler struct {
	loop   *ExperimentLoop
	engram EngramClient
	logger *slog.Logger
}

// NewDashboardHandler creates an HTTP handler for autoresearch experiment data.
func NewDashboardHandler(loop *ExperimentLoop, engram EngramClient) *DashboardHandler {
	return &DashboardHandler{
		loop:   loop,
		engram: engram,
		logger: slog.Default(),
	}
}

// ServeHTTP routes requests to the appropriate dashboard endpoint.
func (h *DashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/autoresearch")
	path = strings.TrimSuffix(path, "/")

	switch {
	case path == "/experiments":
		h.handleListExperiments(w, r)
	case strings.HasPrefix(path, "/experiments/"):
		id := strings.TrimPrefix(path, "/experiments/")
		h.handleGetExperiment(w, r, id)
	case path == "/metrics":
		h.handleMetrics(w, r)
	case path == "/compare":
		h.handleCompare(w, r)
	default:
		h.writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *DashboardHandler) handleListExperiments(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	memories, err := h.engram.SearchRelatedExperiments(ctx, "experiment", 100)
	if err != nil {
		h.logger.Error("failed to list experiments", "err", err)
		h.writeError(w, http.StatusInternalServerError, "failed to retrieve experiments")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"experiments": memories,
		"total":       len(memories),
	})
}

func (h *DashboardHandler) handleGetExperiment(w http.ResponseWriter, _ *http.Request, id string) {
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "experiment id required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	history, err := h.engram.GetExperimentHistory(ctx, id)
	if err != nil {
		h.logger.Error("failed to get experiment", "id", id, "err", err)
		h.writeError(w, http.StatusInternalServerError, "failed to retrieve experiment")
		return
	}

	if len(history) == 0 {
		h.writeError(w, http.StatusNotFound, "experiment not found")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      id,
		"history": history,
	})
}

func (h *DashboardHandler) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	snapshot := h.loop.Metrics().Snapshot()
	h.writeJSON(w, http.StatusOK, snapshot)
}

func (h *DashboardHandler) handleCompare(w http.ResponseWriter, r *http.Request) {
	baselineID := r.URL.Query().Get("baseline")
	candidateID := r.URL.Query().Get("candidate")

	if baselineID == "" || candidateID == "" {
		h.writeError(w, http.StatusBadRequest, "baseline and candidate query params required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	baselineHistory, err := h.engram.GetExperimentHistory(ctx, baselineID)
	if err != nil || len(baselineHistory) == 0 {
		h.writeError(w, http.StatusNotFound, "baseline experiment not found")
		return
	}

	candidateHistory, err := h.engram.GetExperimentHistory(ctx, candidateID)
	if err != nil || len(candidateHistory) == 0 {
		h.writeError(w, http.StatusNotFound, "candidate experiment not found")
		return
	}

	baseline := memoryToResult(baselineHistory[0], baselineID)
	candidate := memoryToResult(candidateHistory[0], candidateID)

	result := CompareExperiments(baseline, candidate, 0.01)
	h.writeJSON(w, http.StatusOK, result)
}

func (h *DashboardHandler) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Error("failed to encode response", "err", err)
	}
}

func (h *DashboardHandler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]string{"error": message})
}

func memoryToResult(m Memory, id string) ExperimentResult {
	result := ExperimentResult{ID: id}
	_ = json.Unmarshal([]byte(m.Memory), &result)
	return result
}
