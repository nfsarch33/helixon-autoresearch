package autoresearch

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"
)

// RetryConfig controls exponential backoff behavior.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Multiplier  float64
	Jitter      float64
}

// DefaultRetryConfig returns sensible defaults for Engram HTTP retries.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.25,
	}
}

// RetryingEngramClient wraps an EngramClient with exponential backoff on errors.
type RetryingEngramClient struct {
	inner  EngramClient
	cfg    RetryConfig
	logger *slog.Logger
}

// NewRetryingEngramClient wraps the given client with retry logic.
func NewRetryingEngramClient(inner EngramClient, cfg RetryConfig, logger *slog.Logger) *RetryingEngramClient {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 500 * time.Millisecond
	}
	if cfg.Multiplier <= 0 {
		cfg.Multiplier = 2.0
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 10 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RetryingEngramClient{
		inner:  inner,
		cfg:    cfg,
		logger: logger,
	}
}

func (r *RetryingEngramClient) AddExperimentResult(ctx context.Context, result ExperimentResult) error {
	return r.retryOp(ctx, "AddExperimentResult", func(ctx context.Context) error {
		return r.inner.AddExperimentResult(ctx, result)
	})
}

func (r *RetryingEngramClient) SearchRelatedExperiments(ctx context.Context, query string, limit int) ([]Memory, error) {
	var memories []Memory
	err := r.retryOp(ctx, "SearchRelatedExperiments", func(ctx context.Context) error {
		var opErr error
		memories, opErr = r.inner.SearchRelatedExperiments(ctx, query, limit)
		return opErr
	})
	return memories, err
}

func (r *RetryingEngramClient) GetExperimentHistory(ctx context.Context, experimentID string) ([]Memory, error) {
	var memories []Memory
	err := r.retryOp(ctx, "GetExperimentHistory", func(ctx context.Context) error {
		var opErr error
		memories, opErr = r.inner.GetExperimentHistory(ctx, experimentID)
		return opErr
	})
	return memories, err
}

func (r *RetryingEngramClient) retryOp(ctx context.Context, opName string, op func(ctx context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= r.cfg.MaxAttempts; attempt++ {
		lastErr = op(ctx)
		if lastErr == nil {
			return nil
		}

		if attempt == r.cfg.MaxAttempts {
			break
		}

		if ctx.Err() != nil {
			return fmt.Errorf("%s: context cancelled during retry: %w", opName, ctx.Err())
		}

		delay := r.backoffDelay(attempt)
		r.logger.Warn("engram operation failed, retrying",
			"op", opName,
			"attempt", attempt,
			"max_attempts", r.cfg.MaxAttempts,
			"delay", delay.String(),
			"err", lastErr,
		)

		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: context cancelled waiting for retry: %w", opName, ctx.Err())
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("%s: all %d attempts failed: %w", opName, r.cfg.MaxAttempts, lastErr)
}

func (r *RetryingEngramClient) backoffDelay(attempt int) time.Duration {
	delay := float64(r.cfg.BaseDelay) * math.Pow(r.cfg.Multiplier, float64(attempt-1))
	if r.cfg.Jitter > 0 {
		jitterRange := delay * r.cfg.Jitter
		delay += (rand.Float64()*2 - 1) * jitterRange
	}
	if delay > float64(r.cfg.MaxDelay) {
		delay = float64(r.cfg.MaxDelay)
	}
	if delay < 0 {
		delay = float64(r.cfg.BaseDelay)
	}
	return time.Duration(delay)
}
