package autoresearch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func retryTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRetryingEngramClient_SuccessOnFirstAttempt(t *testing.T) {
	mock := &mockEngramClient{}
	client := NewRetryingEngramClient(mock, DefaultRetryConfig(), retryTestLogger())

	err := client.AddExperimentResult(context.Background(), ExperimentResult{ID: "ok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.addCalls) != 1 {
		t.Errorf("addCalls = %d, want 1", len(mock.addCalls))
	}
}

func TestRetryingEngramClient_RetryOnTransientError(t *testing.T) {
	var callCount atomic.Int32
	failTwice := &countingEngramClient{
		failUntil: 2,
		callCount: &callCount,
	}
	cfg := RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    50 * time.Millisecond,
		Multiplier:  2.0,
	}
	client := NewRetryingEngramClient(failTwice, cfg, retryTestLogger())

	err := client.AddExperimentResult(context.Background(), ExperimentResult{ID: "retry"})
	if err != nil {
		t.Fatalf("expected success after retries: %v", err)
	}
	if got := callCount.Load(); got != 3 {
		t.Errorf("call count = %d, want 3", got)
	}
}

func TestRetryingEngramClient_AllAttemptsExhausted(t *testing.T) {
	mock := &mockEngramClient{
		addErr: fmt.Errorf("permanent failure"),
	}
	cfg := RetryConfig{
		MaxAttempts: 2,
		BaseDelay:   5 * time.Millisecond,
		MaxDelay:    20 * time.Millisecond,
		Multiplier:  2.0,
	}
	client := NewRetryingEngramClient(mock, cfg, retryTestLogger())

	err := client.AddExperimentResult(context.Background(), ExperimentResult{ID: "fail"})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.addCalls) != 2 {
		t.Errorf("addCalls = %d, want 2", len(mock.addCalls))
	}
}

func TestRetryingEngramClient_ContextCancelledDuringRetry(t *testing.T) {
	mock := &mockEngramClient{
		addErr: fmt.Errorf("transient"),
	}
	cfg := RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		Multiplier:  2.0,
	}
	client := NewRetryingEngramClient(mock, cfg, retryTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := client.AddExperimentResult(ctx, ExperimentResult{ID: "cancel"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestRetryingEngramClient_SearchRetries(t *testing.T) {
	var callCount atomic.Int32
	failOnce := &countingEngramClient{
		failUntil:    1,
		callCount:    &callCount,
		searchResult: []Memory{{ID: "m1", Memory: "result"}},
	}
	cfg := RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   5 * time.Millisecond,
		MaxDelay:    20 * time.Millisecond,
		Multiplier:  2.0,
	}
	client := NewRetryingEngramClient(failOnce, cfg, retryTestLogger())

	results, err := client.SearchRelatedExperiments(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("results = %d, want 1", len(results))
	}
}

func TestRetryingEngramClient_GetHistoryRetries(t *testing.T) {
	var callCount atomic.Int32
	failOnce := &countingEngramClient{
		failUntil:    1,
		callCount:    &callCount,
		searchResult: []Memory{{ID: "h1", Memory: "history"}},
	}
	cfg := RetryConfig{
		MaxAttempts: 2,
		BaseDelay:   5 * time.Millisecond,
		MaxDelay:    20 * time.Millisecond,
		Multiplier:  2.0,
	}
	client := NewRetryingEngramClient(failOnce, cfg, retryTestLogger())

	results, err := client.GetExperimentHistory(context.Background(), "exp-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("results = %d, want 1", len(results))
	}
}

func TestRetryingEngramClient_InterfaceCompliance(t *testing.T) {
	var _ EngramClient = (*RetryingEngramClient)(nil)
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.BaseDelay != 500*time.Millisecond {
		t.Errorf("BaseDelay = %v, want 500ms", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 10*time.Second {
		t.Errorf("MaxDelay = %v, want 10s", cfg.MaxDelay)
	}
}

func TestNewRetryingEngramClient_DefaultsApplied(t *testing.T) {
	mock := &mockEngramClient{}
	client := NewRetryingEngramClient(mock, RetryConfig{}, nil)
	if client.cfg.MaxAttempts != 1 {
		t.Errorf("MaxAttempts = %d, want 1 (zero clamped to 1)", client.cfg.MaxAttempts)
	}
	if client.logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestBackoffDelay_Bounds(t *testing.T) {
	client := NewRetryingEngramClient(&mockEngramClient{}, RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    500 * time.Millisecond,
		Multiplier:  2.0,
		Jitter:      0.0,
	}, retryTestLogger())

	d1 := client.backoffDelay(1)
	if d1 != 100*time.Millisecond {
		t.Errorf("attempt 1 delay = %v, want 100ms", d1)
	}

	d2 := client.backoffDelay(2)
	if d2 != 200*time.Millisecond {
		t.Errorf("attempt 2 delay = %v, want 200ms", d2)
	}

	d4 := client.backoffDelay(4)
	if d4 != 500*time.Millisecond {
		t.Errorf("attempt 4 delay = %v, want 500ms (capped)", d4)
	}
}

// countingEngramClient fails for the first N calls, then succeeds.
type countingEngramClient struct {
	failUntil    int32
	callCount    *atomic.Int32
	searchResult []Memory
}

func (c *countingEngramClient) AddExperimentResult(_ context.Context, _ ExperimentResult) error {
	n := c.callCount.Add(1)
	if n <= c.failUntil {
		return fmt.Errorf("transient error (call %d)", n)
	}
	return nil
}

func (c *countingEngramClient) SearchRelatedExperiments(_ context.Context, _ string, _ int) ([]Memory, error) {
	n := c.callCount.Add(1)
	if n <= c.failUntil {
		return nil, fmt.Errorf("transient error (call %d)", n)
	}
	return c.searchResult, nil
}

func (c *countingEngramClient) GetExperimentHistory(_ context.Context, _ string) ([]Memory, error) {
	n := c.callCount.Add(1)
	if n <= c.failUntil {
		return nil, fmt.Errorf("transient error (call %d)", n)
	}
	return c.searchResult, nil
}
