package autoresearch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SchedulerStatus represents the current state of the scheduler.
type SchedulerStatus string

const (
	SchedulerIdle     SchedulerStatus = "idle"
	SchedulerRunning  SchedulerStatus = "running"
	SchedulerStopped  SchedulerStatus = "stopped"
	SchedulerDraining SchedulerStatus = "draining"
)

// ErrQueueFull is returned when the scheduler's experiment queue is at capacity.
var ErrQueueFull = fmt.Errorf("scheduler queue is full")

// SchedulerConfig configures experiment scheduling behavior.
type SchedulerConfig struct {
	Interval    time.Duration // Fixed interval between runs (0 = one-shot)
	MaxQueue    int           // Max pending experiments (default: 10)
	Concurrency int           // Max parallel experiments (default: 1)
	Logger      *slog.Logger
}

func (c *SchedulerConfig) withDefaults() SchedulerConfig {
	cfg := *c
	if cfg.MaxQueue <= 0 {
		cfg.MaxQueue = 10
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return cfg
}

// Scheduler runs experiments on a configurable interval or cron-like schedule.
// It supports fixed-interval execution, one-shot execution, graceful shutdown
// via context cancellation, and an experiment queue with configurable concurrency.
type Scheduler struct {
	loop   *ExperimentLoop
	cfg    SchedulerConfig
	queue  chan ExperimentConfig
	mu     sync.Mutex
	status SchedulerStatus
}

// NewScheduler creates a scheduler bound to the given experiment loop.
func NewScheduler(loop *ExperimentLoop, cfg SchedulerConfig) *Scheduler {
	cfg = cfg.withDefaults()
	return &Scheduler{
		loop:   loop,
		cfg:    cfg,
		queue:  make(chan ExperimentConfig, cfg.MaxQueue),
		status: SchedulerIdle,
	}
}

// Enqueue adds an experiment configuration to the scheduler queue.
// Returns ErrQueueFull if the queue is at capacity.
func (s *Scheduler) Enqueue(config ExperimentConfig) error {
	select {
	case s.queue <- config:
		return nil
	default:
		return ErrQueueFull
	}
}

// Pending returns the number of experiments waiting in the queue.
func (s *Scheduler) Pending() int {
	return len(s.queue)
}

// Status returns the current scheduler status.
func (s *Scheduler) Status() SchedulerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Scheduler) setStatus(status SchedulerStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// Run starts the scheduler loop. It processes queued experiments respecting
// the concurrency limit and interval configuration. Blocks until the context
// is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	s.setStatus(SchedulerRunning)
	defer s.setStatus(SchedulerStopped)

	sem := make(chan struct{}, s.cfg.Concurrency)
	var wg sync.WaitGroup

	if s.cfg.Interval > 0 {
		return s.runInterval(ctx, sem, &wg)
	}
	return s.runDrain(ctx, sem, &wg)
}

func (s *Scheduler) runInterval(ctx context.Context, sem chan struct{}, wg *sync.WaitGroup) error {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.setStatus(SchedulerDraining)
			wg.Wait()
			return ctx.Err()
		case <-ticker.C:
			s.drainBatch(ctx, sem, wg)
		}
	}
}

func (s *Scheduler) runDrain(ctx context.Context, sem chan struct{}, wg *sync.WaitGroup) error {
	for {
		select {
		case <-ctx.Done():
			s.setStatus(SchedulerDraining)
			wg.Wait()
			return ctx.Err()
		case config, ok := <-s.queue:
			if !ok {
				wg.Wait()
				return nil
			}
			s.executeWithSem(ctx, config, sem, wg)
		}
	}
}

func (s *Scheduler) drainBatch(ctx context.Context, sem chan struct{}, wg *sync.WaitGroup) {
	for {
		select {
		case config := <-s.queue:
			s.executeWithSem(ctx, config, sem, wg)
		default:
			return
		}
	}
}

func (s *Scheduler) executeWithSem(ctx context.Context, config ExperimentConfig, sem chan struct{}, wg *sync.WaitGroup) {
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { <-sem }()

		_, err := s.loop.RunExperiment(ctx, config)
		if err != nil {
			s.cfg.Logger.Warn("scheduled experiment failed",
				"name", config.Name,
				"err", err,
			)
		}
	}()
}
