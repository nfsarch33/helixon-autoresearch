package autoresearch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

type mockEngramClient struct {
	mu           sync.Mutex
	addCalls     []ExperimentResult
	searchCalls  []string
	searchResult []Memory
	addErr       error
	searchErr    error
}

func (m *mockEngramClient) AddExperimentResult(_ context.Context, result ExperimentResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addCalls = append(m.addCalls, result)
	return m.addErr
}

func (m *mockEngramClient) SearchRelatedExperiments(_ context.Context, query string, _ int) ([]Memory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.searchCalls = append(m.searchCalls, query)
	return m.searchResult, m.searchErr
}

func (m *mockEngramClient) GetExperimentHistory(_ context.Context, experimentID string) ([]Memory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.searchCalls = append(m.searchCalls, "history:"+experimentID)
	return m.searchResult, m.searchErr
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRunExperiment_Success(t *testing.T) {
	mock := &mockEngramClient{}
	loop := NewExperimentLoop(mock, testLogger())

	config := ExperimentConfig{
		Name:       "lr-sweep",
		Hypothesis: "lower lr improves convergence",
	}

	result, err := loop.RunExperiment(context.Background(), config)
	if err != nil {
		t.Fatalf("RunExperiment: %v", err)
	}

	if result.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
	if result.Name != "lr-sweep" {
		t.Errorf("Name = %q, want lr-sweep", result.Name)
	}
	if result.ID == "" {
		t.Error("ID should be non-empty")
	}
	if result.Duration == 0 {
		t.Error("Duration should be non-zero")
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	// dedup search (1) + related search (1) + 6 phase logs + 1 final persist = 9 calls
	// search calls: dedup (1) + related (1) = 2
	if len(mock.searchCalls) != 2 {
		t.Errorf("searchCalls = %d, want 2 (dedup + related)", len(mock.searchCalls))
	}
}

func TestRunExperiment_InvalidConfig(t *testing.T) {
	mock := &mockEngramClient{}
	loop := NewExperimentLoop(mock, testLogger())

	_, err := loop.RunExperiment(context.Background(), ExperimentConfig{})
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestRunExperiment_ContextCancelled(t *testing.T) {
	mock := &mockEngramClient{
		searchResult: []Memory{{ID: "prior-1", Memory: "related work"}},
	}
	loop := NewExperimentLoop(mock, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := loop.RunExperiment(ctx, ExperimentConfig{
		Name:       "cancelled-exp",
		Hypothesis: "this will be cancelled",
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if result.Status != StatusFailed {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

func TestRunExperiment_SearchError(t *testing.T) {
	mock := &mockEngramClient{
		searchErr: fmt.Errorf("engram unreachable"),
	}
	cfg := DefaultLoopConfig()
	cfg.DeduplicateEnabled = false
	loop := NewExperimentLoopWithConfig(mock, testLogger(), nil, cfg)

	result, err := loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "search-fail",
		Hypothesis: "experiment should still run if search fails",
	})
	if err != nil {
		t.Fatalf("should succeed even when search fails: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
}

func TestRunExperiment_PhasesExecuteInOrder(t *testing.T) {
	mock := &mockEngramClient{}
	cfg := DefaultLoopConfig()
	cfg.DeduplicateEnabled = false
	loop := NewExperimentLoopWithConfig(mock, testLogger(), nil, cfg)

	_, err := loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "phase-order",
		Hypothesis: "phases run sequentially",
	})
	if err != nil {
		t.Fatalf("RunExperiment: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()

	expectedPhases := []ExperimentPhase{
		PhaseIdeation, PhaseImplementation, PhaseTraining,
		PhaseEvaluation, PhaseComparison, PhasePromotion,
	}
	for i, expected := range expectedPhases {
		if i >= len(mock.addCalls) {
			t.Fatalf("not enough addCalls: got %d, need at least %d", len(mock.addCalls), i+1)
		}
		got := mock.addCalls[i].Phase
		if got != expected {
			t.Errorf("addCalls[%d].Phase = %v, want %v", i, got, expected)
		}
	}
}

func TestRunExperiment_RelatedExperimentsFound(t *testing.T) {
	mock := &mockEngramClient{
		searchResult: []Memory{
			{ID: "m1", Memory: "prior lr sweep: diverged at 1e-6"},
			{ID: "m2", Memory: "prior lr sweep: best at 3e-5"},
		},
	}
	loop := NewExperimentLoop(mock, testLogger())

	result, err := loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "with-priors",
		Hypothesis: "lower lr improves convergence",
	})
	if err != nil {
		t.Fatalf("RunExperiment: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
}

func TestRunExperiment_WithTimeout(t *testing.T) {
	mock := &mockEngramClient{}
	loop := NewExperimentLoop(mock, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := loop.RunExperiment(ctx, ExperimentConfig{
		Name:       "timeout-test",
		Hypothesis: "runs within timeout",
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunExperiment: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
}

func TestNewExperimentLoop_NilLogger(t *testing.T) {
	mock := &mockEngramClient{}
	loop := NewExperimentLoop(mock, nil)
	if loop.logger == nil {
		t.Error("logger should default to slog.Default(), not nil")
	}
}

func TestMockEngramClientInterface(t *testing.T) {
	var _ EngramClient = (*mockEngramClient)(nil)
}

// --- Deduplication tests ---

func TestRunExperiment_DuplicateDetected(t *testing.T) {
	mock := &mockEngramClient{
		searchResult: []Memory{
			{ID: "existing", Memory: "lower lr improves convergence"},
		},
	}
	loop := NewExperimentLoop(mock, testLogger())

	result, err := loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "dup-test",
		Hypothesis: "lower lr improves convergence",
	})
	if err != ErrDuplicateExperiment {
		t.Fatalf("expected ErrDuplicateExperiment, got %v", err)
	}
	if result.Status != StatusSkipped {
		t.Errorf("Status = %q, want skipped", result.Status)
	}
}

func TestRunExperiment_DuplicateDetected_CaseInsensitive(t *testing.T) {
	mock := &mockEngramClient{
		searchResult: []Memory{
			{ID: "existing", Memory: "  Lower LR  Improves  Convergence  "},
		},
	}
	loop := NewExperimentLoop(mock, testLogger())

	_, err := loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "dup-case",
		Hypothesis: "lower lr improves convergence",
	})
	if err != ErrDuplicateExperiment {
		t.Fatalf("expected ErrDuplicateExperiment, got %v", err)
	}
}

func TestRunExperiment_NoDuplicateWhenDifferent(t *testing.T) {
	mock := &mockEngramClient{
		searchResult: []Memory{
			{ID: "different", Memory: "higher lr causes divergence"},
		},
	}
	loop := NewExperimentLoop(mock, testLogger())

	result, err := loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "no-dup",
		Hypothesis: "lower lr improves convergence",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
}

func TestRunExperiment_DeduplicationDisabled(t *testing.T) {
	mock := &mockEngramClient{
		searchResult: []Memory{
			{ID: "existing", Memory: "lower lr improves convergence"},
		},
	}
	cfg := DefaultLoopConfig()
	cfg.DeduplicateEnabled = false
	loop := NewExperimentLoopWithConfig(mock, testLogger(), nil, cfg)

	result, err := loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "dedup-off",
		Hypothesis: "lower lr improves convergence",
	})
	if err != nil {
		t.Fatalf("should succeed with dedup disabled: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
}

func TestRunExperiment_DedupSearchError_ContinuesRun(t *testing.T) {
	mock := &mockEngramClient{
		searchErr: fmt.Errorf("engram down"),
	}
	cfg := DefaultLoopConfig()
	cfg.DeduplicateEnabled = true
	loop := NewExperimentLoopWithConfig(mock, testLogger(), nil, cfg)

	result, err := loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "dedup-err",
		Hypothesis: "dedup error should not block experiment",
	})
	if err != nil {
		t.Fatalf("should succeed when dedup search fails: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
}

// --- Metrics integration tests ---

func TestRunExperiment_MetricsRecorded(t *testing.T) {
	mock := &mockEngramClient{}
	metrics := NewExperimentMetrics()
	cfg := DefaultLoopConfig()
	cfg.DeduplicateEnabled = false
	loop := NewExperimentLoopWithConfig(mock, testLogger(), metrics, cfg)

	_, err := loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "metrics-test",
		Hypothesis: "metrics are recorded",
	})
	if err != nil {
		t.Fatalf("RunExperiment: %v", err)
	}

	snap := metrics.Snapshot()
	if snap.TotalRuns != 1 {
		t.Errorf("TotalRuns = %d, want 1", snap.TotalRuns)
	}
	if snap.SuccessCount != 1 {
		t.Errorf("SuccessCount = %d, want 1", snap.SuccessCount)
	}
	if len(snap.PhaseDurations) != 6 {
		t.Errorf("PhaseDurations count = %d, want 6", len(snap.PhaseDurations))
	}
}

func TestRunExperiment_MetricsRecordedOnFailure(t *testing.T) {
	mock := &mockEngramClient{}
	metrics := NewExperimentMetrics()
	cfg := DefaultLoopConfig()
	cfg.DeduplicateEnabled = false
	loop := NewExperimentLoopWithConfig(mock, testLogger(), metrics, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = loop.RunExperiment(ctx, ExperimentConfig{
		Name:       "fail-metrics",
		Hypothesis: "metrics recorded on failure",
	})

	snap := metrics.Snapshot()
	if snap.TotalRuns != 1 {
		t.Errorf("TotalRuns = %d, want 1", snap.TotalRuns)
	}
	if snap.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", snap.FailureCount)
	}
}

func TestRunExperiment_DuplicateMetricsRecorded(t *testing.T) {
	mock := &mockEngramClient{
		searchResult: []Memory{
			{ID: "dup", Memory: "hypothesis x"},
		},
	}
	metrics := NewExperimentMetrics()
	loop := NewExperimentLoopWithConfig(mock, testLogger(), metrics, DefaultLoopConfig())

	_, _ = loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "dup-metrics",
		Hypothesis: "hypothesis x",
	})

	snap := metrics.Snapshot()
	if snap.DuplicateCount != 1 {
		t.Errorf("DuplicateCount = %d, want 1", snap.DuplicateCount)
	}
	if snap.SkipCount != 1 {
		t.Errorf("SkipCount = %d, want 1", snap.SkipCount)
	}
}

func TestExperimentLoop_MetricsAccessor(t *testing.T) {
	mock := &mockEngramClient{}
	metrics := NewExperimentMetrics()
	loop := NewExperimentLoopWithConfig(mock, testLogger(), metrics, DefaultLoopConfig())

	if loop.Metrics() != metrics {
		t.Error("Metrics() should return the injected metrics collector")
	}
}

// --- Phase timeout tests ---

func TestRunExperiment_PhaseTimeoutConfig(t *testing.T) {
	mock := &mockEngramClient{}
	cfg := DefaultLoopConfig()
	cfg.PhaseTimeout = 1 * time.Second
	cfg.DeduplicateEnabled = false
	loop := NewExperimentLoopWithConfig(mock, testLogger(), nil, cfg)

	result, err := loop.RunExperiment(context.Background(), ExperimentConfig{
		Name:       "timeout-cfg",
		Hypothesis: "uses custom phase timeout",
	})
	if err != nil {
		t.Fatalf("RunExperiment: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
}

func TestDefaultLoopConfig(t *testing.T) {
	cfg := DefaultLoopConfig()
	if cfg.PhaseTimeout != 5*time.Minute {
		t.Errorf("PhaseTimeout = %v, want 5m", cfg.PhaseTimeout)
	}
	if !cfg.DeduplicateEnabled {
		t.Error("DeduplicateEnabled should be true by default")
	}
}

func TestNormalizeHypothesis(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  Lower LR  Improves  Convergence  ", "lower lr improves convergence"},
		{"HELLO world", "hello world"},
		{"no change", "no change"},
		{"   spaces   everywhere   ", "spaces everywhere"},
	}
	for _, tt := range tests {
		got := normalizeHypothesis(tt.input)
		if got != tt.want {
			t.Errorf("normalizeHypothesis(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
