package autoresearch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

const defaultPhaseTimeout = 5 * time.Minute

// LoopConfig tunes experiment loop behavior.
type LoopConfig struct {
	PhaseTimeout       time.Duration
	DeduplicateEnabled bool
	DedupSimilarity    float64
}

// DefaultLoopConfig returns production defaults.
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{
		PhaseTimeout:       defaultPhaseTimeout,
		DeduplicateEnabled: true,
		DedupSimilarity:    0.95,
	}
}

// ErrDuplicateExperiment is returned when deduplication detects an identical hypothesis.
var ErrDuplicateExperiment = fmt.Errorf("duplicate experiment")

// ErrGuardrailTerminated is returned when a guardrail prevents further execution.
var ErrGuardrailTerminated = fmt.Errorf("guardrail terminated loop")

// ExperimentLoop orchestrates the experiment lifecycle through all phases.
type ExperimentLoop struct {
	engram     EngramClient
	logger     *slog.Logger
	metrics    *ExperimentMetrics
	cfg        LoopConfig
	guardrails *LoopController
}

// NewExperimentLoop creates a loop that persists results to Engram.
func NewExperimentLoop(engram EngramClient, logger *slog.Logger) *ExperimentLoop {
	return NewExperimentLoopWithConfig(engram, logger, nil, DefaultLoopConfig())
}

// NewExperimentLoopWithConfig creates a loop with full configuration.
func NewExperimentLoopWithConfig(engram EngramClient, logger *slog.Logger, metrics *ExperimentMetrics, cfg LoopConfig) *ExperimentLoop {
	if logger == nil {
		logger = slog.Default()
	}
	if metrics == nil {
		metrics = NewExperimentMetrics()
	}
	if cfg.PhaseTimeout <= 0 {
		cfg.PhaseTimeout = defaultPhaseTimeout
	}
	return &ExperimentLoop{
		engram:  engram,
		logger:  logger,
		metrics: metrics,
		cfg:     cfg,
	}
}

// SetGuardrails configures loop termination guardrails.
func (l *ExperimentLoop) SetGuardrails(guardrails *LoopController) {
	l.guardrails = guardrails
}

// Guardrails returns the configured guardrails, or nil if not set.
func (l *ExperimentLoop) Guardrails() *LoopController {
	return l.guardrails
}

// Metrics returns the metrics collector for external inspection.
func (l *ExperimentLoop) Metrics() *ExperimentMetrics {
	return l.metrics
}

// RunExperiment executes all phases for a single experiment.
// It checks for duplicates, searches for related prior work,
// runs through each phase with timeout enforcement,
// and persists the final result to Engram.
func (l *ExperimentLoop) RunExperiment(ctx context.Context, config ExperimentConfig) (ExperimentResult, error) {
	if l.guardrails != nil {
		status := l.guardrails.ShouldContinue()
		if !status.ShouldContinue {
			l.logger.Warn("guardrails prevented experiment execution",
				"reasons", status.ActiveReasons,
				"actions", status.Actions,
			)
			return ExperimentResult{}, fmt.Errorf("%w: %v", ErrGuardrailTerminated, status.ActiveReasons)
		}
		l.logger.Info("guardrails check passed",
			"stagnation_window", status.Stagnation.WindowSize,
			"circuit_state", status.CircuitState,
			"budget_remaining_credits", status.Budget.CreditsRemaining,
		)
	}

	if err := config.Validate(); err != nil {
		return ExperimentResult{}, fmt.Errorf("invalid config: %w", err)
	}

	expStart := time.Now()
	expLogger := l.logger.With(
		"experiment_name", config.Name,
		"hypothesis", config.Hypothesis,
	)

	if l.cfg.DeduplicateEnabled {
		if dup, err := l.checkDuplicate(ctx, config); err != nil {
			expLogger.Warn("deduplication check failed, continuing", "err", err)
		} else if dup {
			l.metrics.RecordDuplicate()
			l.metrics.RecordRun(StatusSkipped, time.Since(expStart))
			expLogger.Info("experiment skipped: duplicate hypothesis detected")
			return ExperimentResult{
				ID:         uuid.New().String(),
				Name:       config.Name,
				Hypothesis: config.Hypothesis,
				Status:     StatusSkipped,
				Timestamp:  expStart,
				Duration:   time.Since(expStart),
				Error:      "duplicate hypothesis",
			}, ErrDuplicateExperiment
		}
	}

	result := ExperimentResult{
		ID:         uuid.New().String(),
		Name:       config.Name,
		Hypothesis: config.Hypothesis,
		Status:     StatusRunning,
		Phase:      PhaseIdeation,
		Timestamp:  expStart,
	}
	expLogger = expLogger.With("experiment_id", result.ID)

	expLogger.Info("experiment started",
		"started_at", expStart.Format(time.RFC3339),
	)

	related, err := l.engram.SearchRelatedExperiments(ctx, config.Hypothesis, 5)
	if err != nil {
		expLogger.Warn("failed to search related experiments, continuing", "err", err)
	} else if len(related) > 0 {
		expLogger.Info("found related prior experiments", "count", len(related))
	}

	phases := []ExperimentPhase{
		PhaseIdeation,
		PhaseImplementation,
		PhaseTraining,
		PhaseEvaluation,
		PhaseComparison,
		PhasePromotion,
	}

	for _, phase := range phases {
		if ctx.Err() != nil {
			result.Status = StatusFailed
			result.Error = ctx.Err().Error()
			result.Duration = time.Since(expStart)
			l.metrics.RecordRun(StatusFailed, result.Duration)
			l.persistResult(ctx, result)
			if l.guardrails != nil {
				l.guardrails.RecordExperiment(0, 0, false)
			}
			return result, ctx.Err()
		}

		result.Phase = phase
		phaseStart := time.Now()
		expLogger.Info("phase started",
			"phase", phase.String(),
			"phase_started_at", phaseStart.Format(time.RFC3339),
		)

		if err := l.runPhaseWithTimeout(ctx, phase, &result, expLogger); err != nil {
			phaseDur := time.Since(phaseStart)
			l.metrics.RecordPhase(phase, phaseDur)
			expLogger.Error("phase failed",
				"phase", phase.String(),
				"duration", phaseDur.String(),
				"err", err,
			)
			result.Status = StatusFailed
			result.Error = err.Error()
			result.Duration = time.Since(expStart)
			l.metrics.RecordRun(StatusFailed, result.Duration)
			l.persistResult(ctx, result)
			if l.guardrails != nil {
				l.guardrails.RecordExperiment(0, 0, false)
			}
			return result, fmt.Errorf("phase %s failed: %w", phase.String(), err)
		}

		phaseDur := time.Since(phaseStart)
		l.metrics.RecordPhase(phase, phaseDur)
		expLogger.Info("phase completed",
			"phase", phase.String(),
			"duration", phaseDur.String(),
		)
	}

	result.Status = StatusCompleted
	result.Duration = time.Since(expStart)
	l.metrics.RecordRun(StatusCompleted, result.Duration)
	l.persistResult(ctx, result)

	if l.guardrails != nil {
		score := computeScore(result)
		l.guardrails.RecordExperiment(score, 0, true)
	}

	expLogger.Info("experiment completed",
		"duration", result.Duration.String(),
		"status", string(result.Status),
		"completed_at", time.Now().Format(time.RFC3339),
	)

	return result, nil
}

func (l *ExperimentLoop) checkDuplicate(ctx context.Context, config ExperimentConfig) (bool, error) {
	results, err := l.engram.SearchRelatedExperiments(ctx, config.Hypothesis, 10)
	if err != nil {
		return false, err
	}
	for _, mem := range results {
		if normalizeHypothesis(mem.Memory) == normalizeHypothesis(config.Hypothesis) {
			return true, nil
		}
	}
	return false, nil
}

func normalizeHypothesis(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	return strings.Join(strings.Fields(s), " ")
}

func (l *ExperimentLoop) runPhaseWithTimeout(ctx context.Context, phase ExperimentPhase, result *ExperimentResult, logger *slog.Logger) error {
	phaseCtx, cancel := context.WithTimeout(ctx, l.cfg.PhaseTimeout)
	defer cancel()

	phaseResult := ExperimentResult{
		ID:        result.ID,
		Name:      result.Name,
		Status:    StatusRunning,
		Phase:     phase,
		Timestamp: time.Now(),
	}

	if err := l.engram.AddExperimentResult(phaseCtx, phaseResult); err != nil {
		logger.Warn("failed to log phase start to engram", "phase", phase.String(), "err", err)
	}

	return nil
}

func (l *ExperimentLoop) persistResult(ctx context.Context, result ExperimentResult) {
	if err := l.engram.AddExperimentResult(ctx, result); err != nil {
		l.logger.Error("failed to persist experiment result", "id", result.ID, "err", err)
	}
}
