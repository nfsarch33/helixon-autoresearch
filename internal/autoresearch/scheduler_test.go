package autoresearch

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestScheduler_EnqueueAndPending(t *testing.T) {
	loop := NewExperimentLoop(&mockEngram{}, nil)
	s := NewScheduler(loop, SchedulerConfig{MaxQueue: 3})

	if got := s.Pending(); got != 0 {
		t.Fatalf("expected 0 pending, got %d", got)
	}

	cfg := ExperimentConfig{Name: "exp1", Hypothesis: "test"}
	if err := s.Enqueue(cfg); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if got := s.Pending(); got != 1 {
		t.Fatalf("expected 1 pending, got %d", got)
	}
}

func TestScheduler_QueueFullRejection(t *testing.T) {
	loop := NewExperimentLoop(&mockEngram{}, nil)
	s := NewScheduler(loop, SchedulerConfig{MaxQueue: 2})

	cfg := ExperimentConfig{Name: "exp", Hypothesis: "h"}
	_ = s.Enqueue(cfg)
	_ = s.Enqueue(cfg)

	if err := s.Enqueue(cfg); err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
}

func TestScheduler_GracefulShutdown(t *testing.T) {
	loop := NewExperimentLoop(&mockEngram{}, nil)
	s := NewScheduler(loop, SchedulerConfig{Concurrency: 1})

	cfg := ExperimentConfig{Name: "exp", Hypothesis: "h"}
	_ = s.Enqueue(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := s.Run(ctx)
	if err == nil {
		t.Fatal("expected context error on shutdown")
	}

	if s.Status() != SchedulerStopped {
		t.Fatalf("expected stopped status, got %s", s.Status())
	}
}

func TestScheduler_ConcurrencyLimit(t *testing.T) {
	var mu sync.Mutex
	maxConcurrent := 0
	current := 0

	engram := &mockEngram{
		addFunc: func(_ context.Context, _ ExperimentResult) error {
			mu.Lock()
			current++
			if current > maxConcurrent {
				maxConcurrent = current
			}
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			current--
			mu.Unlock()
			return nil
		},
	}

	loop := NewExperimentLoop(engram, nil)
	s := NewScheduler(loop, SchedulerConfig{
		Concurrency: 2,
		MaxQueue:    10,
	})

	for i := 0; i < 6; i++ {
		_ = s.Enqueue(ExperimentConfig{
			Name:       "exp",
			Hypothesis: "h",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = s.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if maxConcurrent > 2 {
		t.Fatalf("concurrency limit exceeded: max was %d, expected <=2", maxConcurrent)
	}
}

func TestScheduler_FixedIntervalExecution(t *testing.T) {
	var mu sync.Mutex
	runCount := 0

	engram := &mockEngram{
		addFunc: func(_ context.Context, _ ExperimentResult) error {
			mu.Lock()
			runCount++
			mu.Unlock()
			return nil
		},
	}

	loop := NewExperimentLoop(engram, nil)
	s := NewScheduler(loop, SchedulerConfig{
		Interval:    50 * time.Millisecond,
		Concurrency: 1,
		MaxQueue:    10,
	})

	for i := 0; i < 3; i++ {
		_ = s.Enqueue(ExperimentConfig{Name: "exp", Hypothesis: "h"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = s.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if runCount == 0 {
		t.Fatal("expected at least one experiment to execute on interval")
	}
}

func TestScheduler_StatusTransitions(t *testing.T) {
	loop := NewExperimentLoop(&mockEngram{}, nil)
	s := NewScheduler(loop, SchedulerConfig{})

	if s.Status() != SchedulerIdle {
		t.Fatalf("expected idle, got %s", s.Status())
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	if s.Status() != SchedulerRunning {
		t.Fatalf("expected running, got %s", s.Status())
	}

	cancel()
	<-done

	if s.Status() != SchedulerStopped {
		t.Fatalf("expected stopped, got %s", s.Status())
	}
}

type mockEngram struct {
	addFunc    func(context.Context, ExperimentResult) error
	searchFunc func(context.Context, string, int) ([]Memory, error)
}

func (m *mockEngram) AddExperimentResult(ctx context.Context, result ExperimentResult) error {
	if m.addFunc != nil {
		return m.addFunc(ctx, result)
	}
	return nil
}

func (m *mockEngram) SearchRelatedExperiments(ctx context.Context, query string, limit int) ([]Memory, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, query, limit)
	}
	return nil, nil
}

func (m *mockEngram) GetExperimentHistory(ctx context.Context, experimentID string) ([]Memory, error) {
	return nil, nil
}
