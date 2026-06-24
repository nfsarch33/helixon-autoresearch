package autoresearch

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// --- Ralph Wiggum Detector (Stagnation Detection) ---

// RalphWiggumConfig configures stagnation detection parameters.
type RalphWiggumConfig struct {
	WindowSize         int     // Number of recent results to track (default: 10)
	MinImprovement     float64 // Minimum improvement to avoid stagnation (default: 0.001)
	MaxStagnationCount int     // How many stagnant windows before alerting (default: 5)
}

// DefaultRalphWiggumConfig returns production defaults.
func DefaultRalphWiggumConfig() RalphWiggumConfig {
	return RalphWiggumConfig{
		WindowSize:         10,
		MinImprovement:     0.001,
		MaxStagnationCount: 5,
	}
}

// StagnationReport describes a detected stagnation event.
type StagnationReport struct {
	Stagnant       bool
	Reason         string
	Recommendation string
	WindowSize     int
	Improvement    float64
}

// RalphWiggumDetector tracks recent experiment scores and detects stagnation.
type RalphWiggumDetector struct {
	cfg             RalphWiggumConfig
	mu              sync.Mutex
	scores          []float64
	stagnationCount int
}

// NewRalphWiggumDetector creates a detector with the given config.
func NewRalphWiggumDetector(cfg RalphWiggumConfig) *RalphWiggumDetector {
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 10
	}
	if cfg.MinImprovement <= 0 {
		cfg.MinImprovement = 0.001
	}
	if cfg.MaxStagnationCount <= 0 {
		cfg.MaxStagnationCount = 5
	}
	return &RalphWiggumDetector{
		cfg:    cfg,
		scores: make([]float64, 0, cfg.WindowSize),
	}
}

// RecordScore adds a new experiment score to the sliding window.
func (r *RalphWiggumDetector) RecordScore(score float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scores = append(r.scores, score)
	if len(r.scores) > r.cfg.WindowSize {
		r.scores = r.scores[len(r.scores)-r.cfg.WindowSize:]
	}
}

// DetectStagnation checks whether the recent window shows stagnation.
// It analyzes the entire sliding window in a single call, counting
// how many consecutive recent scores show no meaningful improvement.
func (r *RalphWiggumDetector) DetectStagnation() StagnationReport {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.scores) < 2 {
		return StagnationReport{
			Stagnant:       false,
			Reason:         "insufficient data",
			Recommendation: "continue collecting results",
			WindowSize:     len(r.scores),
		}
	}

	stagnantCount := 0
	for i := 1; i < len(r.scores); i++ {
		if r.scores[i]-r.scores[i-1] < r.cfg.MinImprovement {
			stagnantCount++
		}
	}

	totalImprovement := r.scores[len(r.scores)-1] - r.scores[0]

	if stagnantCount >= r.cfg.MaxStagnationCount {
		r.stagnationCount = stagnantCount
		return StagnationReport{
			Stagnant:       true,
			Reason:         fmt.Sprintf("no improvement in %d consecutive transitions (delta=%.6f)", stagnantCount, totalImprovement),
			Recommendation: "consider changing experiment strategy or stopping the loop",
			WindowSize:     len(r.scores),
			Improvement:    totalImprovement,
		}
	}

	r.stagnationCount = stagnantCount
	return StagnationReport{
		Stagnant:    false,
		Reason:      "improvement within threshold",
		WindowSize:  len(r.scores),
		Improvement: totalImprovement,
	}
}

// StagnationCount returns the current consecutive stagnation count.
func (r *RalphWiggumDetector) StagnationCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stagnationCount
}

// --- Circuit Breaker ---

// CircuitState represents the state of a circuit breaker.
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

// CircuitBreakerConfig configures circuit breaker parameters.
type CircuitBreakerConfig struct {
	MaxConsecutiveFailures int           // Failures before tripping (default: 3)
	CooldownPeriod         time.Duration // Time before attempting recovery (default: 5m)
}

// DefaultCircuitBreakerConfig returns production defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		CooldownPeriod:         5 * time.Minute,
	}
}

// CircuitBreaker prevents infinite crash loops by tripping after
// consecutive failures and entering a cooldown period.
type CircuitBreaker struct {
	cfg                 CircuitBreakerConfig
	mu                  sync.Mutex
	state               CircuitState
	consecutiveFailures int
	lastFailureTime     time.Time
	halfOpenSuccesses   int
}

// NewCircuitBreaker creates a circuit breaker with the given config.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.MaxConsecutiveFailures <= 0 {
		cfg.MaxConsecutiveFailures = 3
	}
	if cfg.CooldownPeriod <= 0 {
		cfg.CooldownPeriod = 5 * time.Minute
	}
	return &CircuitBreaker{
		cfg:   cfg,
		state: CircuitClosed,
	}
}

// RecordResult records the outcome of an experiment and transitions states.
func (cb *CircuitBreaker) RecordResult(success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if success {
		cb.consecutiveFailures = 0
		if cb.state == CircuitHalfOpen {
			cb.halfOpenSuccesses++
			if cb.halfOpenSuccesses >= 1 {
				cb.state = CircuitClosed
				cb.halfOpenSuccesses = 0
			}
		}
		return
	}

	cb.consecutiveFailures++
	cb.lastFailureTime = time.Now()

	if cb.consecutiveFailures >= cb.cfg.MaxConsecutiveFailures {
		cb.state = CircuitOpen
	}
}

// IsOpen returns true if the circuit breaker is tripped and blocking execution.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.isOpenLocked(time.Now())
}

func (cb *CircuitBreaker) isOpenLocked(now time.Time) bool {
	if cb.state != CircuitOpen {
		return false
	}
	if now.Sub(cb.lastFailureTime) >= cb.cfg.CooldownPeriod {
		cb.state = CircuitHalfOpen
		cb.halfOpenSuccesses = 0
		return false
	}
	return true
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.isOpenLocked(time.Now())
	return cb.state
}

// ConsecutiveFailures returns the current consecutive failure count.
func (cb *CircuitBreaker) ConsecutiveFailures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.consecutiveFailures
}

// --- Budget Guard ---

// BudgetGuardConfig configures budget enforcement parameters.
type BudgetGuardConfig struct {
	MaxCredits       float64       // Maximum credits allowed (default: 10000)
	MaxDuration      time.Duration // Maximum wall-clock time (default: 24h)
	WarningThreshold float64       // Percentage at which to warn (default: 0.8)
}

// DefaultBudgetGuardConfig returns production defaults.
func DefaultBudgetGuardConfig() BudgetGuardConfig {
	return BudgetGuardConfig{
		MaxCredits:       10000,
		MaxDuration:      24 * time.Hour,
		WarningThreshold: 0.8,
	}
}

// BudgetReport describes the current budget state.
type BudgetReport struct {
	CreditsUsed      float64
	CreditsRemaining float64
	TimeElapsed      time.Duration
	TimeRemaining    time.Duration
	Exhausted        bool
	Warning          bool
	Reason           string
}

// BudgetGuard enforces credit and time limits on the experiment loop.
type BudgetGuard struct {
	cfg       BudgetGuardConfig
	mu        sync.Mutex
	used      float64
	startTime time.Time
}

// NewBudgetGuard creates a budget guard with the given config.
func NewBudgetGuard(cfg BudgetGuardConfig) *BudgetGuard {
	if cfg.MaxCredits <= 0 {
		cfg.MaxCredits = 10000
	}
	if cfg.MaxDuration <= 0 {
		cfg.MaxDuration = 24 * time.Hour
	}
	if cfg.WarningThreshold <= 0 || cfg.WarningThreshold >= 1 {
		cfg.WarningThreshold = 0.8
	}
	return &BudgetGuard{
		cfg:       cfg,
		startTime: time.Now(),
	}
}

// RecordSpend adds credit usage to the running total.
func (bg *BudgetGuard) RecordSpend(credits float64) {
	bg.mu.Lock()
	defer bg.mu.Unlock()
	bg.used += credits
}

// IsExhausted returns true if either credit or time budget is exceeded.
func (bg *BudgetGuard) IsExhausted() bool {
	bg.mu.Lock()
	defer bg.mu.Unlock()
	return bg.isExhaustedLocked()
}

func (bg *BudgetGuard) isExhaustedLocked() bool {
	if bg.used >= bg.cfg.MaxCredits {
		return true
	}
	if time.Since(bg.startTime) >= bg.cfg.MaxDuration {
		return true
	}
	return false
}

// RemainingBudget returns remaining credits and time.
func (bg *BudgetGuard) RemainingBudget() (credits float64, duration time.Duration) {
	bg.mu.Lock()
	defer bg.mu.Unlock()
	credits = bg.cfg.MaxCredits - bg.used
	if credits < 0 {
		credits = 0
	}
	duration = bg.cfg.MaxDuration - time.Since(bg.startTime)
	if duration < 0 {
		duration = 0
	}
	return credits, duration
}

// Report returns a comprehensive budget status report.
func (bg *BudgetGuard) Report() BudgetReport {
	bg.mu.Lock()
	defer bg.mu.Unlock()

	elapsed := time.Since(bg.startTime)
	creditsRemaining := bg.cfg.MaxCredits - bg.used
	if creditsRemaining < 0 {
		creditsRemaining = 0
	}
	timeRemaining := bg.cfg.MaxDuration - elapsed
	if timeRemaining < 0 {
		timeRemaining = 0
	}

	creditUsage := bg.used / bg.cfg.MaxCredits
	timeUsage := float64(elapsed) / float64(bg.cfg.MaxDuration)

	exhausted := bg.isExhaustedLocked()
	warning := !exhausted && (creditUsage >= bg.cfg.WarningThreshold || timeUsage >= bg.cfg.WarningThreshold)

	var reason string
	if exhausted {
		if bg.used >= bg.cfg.MaxCredits {
			reason = "credit budget exhausted"
		} else {
			reason = "time budget exhausted"
		}
	} else if warning {
		if creditUsage >= bg.cfg.WarningThreshold {
			reason = fmt.Sprintf("credit usage at %.1f%%", creditUsage*100)
		} else {
			reason = fmt.Sprintf("time usage at %.1f%%", timeUsage*100)
		}
	}

	return BudgetReport{
		CreditsUsed:      bg.used,
		CreditsRemaining: creditsRemaining,
		TimeElapsed:      elapsed,
		TimeRemaining:    timeRemaining,
		Exhausted:        exhausted,
		Warning:          warning,
		Reason:           reason,
	}
}

// --- LoopController (Orchestrator) ---

// GuardrailStatus aggregates all guardrail states.
type GuardrailStatus struct {
	ShouldContinue bool
	Stagnation     StagnationReport
	CircuitState   CircuitState
	Budget         BudgetReport
	ActiveReasons  []string
	Actions        []string
}

// LoopController orchestrates all guardrails to decide loop continuation.
type LoopController struct {
	rw     *RalphWiggumDetector
	cb     *CircuitBreaker
	bg     *BudgetGuard
	logger *slog.Logger
	mu     sync.Mutex
}

// NewLoopController creates a controller combining all guardrails.
func NewLoopController(
	rwCfg RalphWiggumConfig,
	cbCfg CircuitBreakerConfig,
	bgCfg BudgetGuardConfig,
	logger *slog.Logger,
) *LoopController {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoopController{
		rw:     NewRalphWiggumDetector(rwCfg),
		cb:     NewCircuitBreaker(cbCfg),
		bg:     NewBudgetGuard(bgCfg),
		logger: logger,
	}
}

// NewDefaultLoopController creates a controller with default configs.
func NewDefaultLoopController(logger *slog.Logger) *LoopController {
	return NewLoopController(
		DefaultRalphWiggumConfig(),
		DefaultCircuitBreakerConfig(),
		DefaultBudgetGuardConfig(),
		logger,
	)
}

// RecordExperiment records the outcome of a completed experiment.
func (lc *LoopController) RecordExperiment(score float64, credits float64, success bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.rw.RecordScore(score)
	lc.bg.RecordSpend(credits)
	lc.cb.RecordResult(success)
}

// ShouldContinue checks all guardrails and returns whether the loop should proceed.
func (lc *LoopController) ShouldContinue() GuardrailStatus {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	stagnation := lc.rw.DetectStagnation()
	circuitOpen := lc.cb.IsOpen()
	budget := lc.bg.Report()

	var reasons, actions []string
	shouldContinue := true

	if stagnation.Stagnant {
		shouldContinue = false
		reasons = append(reasons, fmt.Sprintf("stagnation: %s", stagnation.Reason))
		actions = append(actions, stagnation.Recommendation)
		lc.logger.Warn("guardrail: stagnation detected", "reason", stagnation.Reason)
	}

	if circuitOpen {
		shouldContinue = false
		reasons = append(reasons, fmt.Sprintf("circuit breaker open: %d consecutive failures", lc.cb.ConsecutiveFailures()))
		actions = append(actions, "wait for cooldown period or investigate failure cause")
		lc.logger.Warn("guardrail: circuit breaker tripped", "failures", lc.cb.ConsecutiveFailures())
	}

	if budget.Exhausted {
		shouldContinue = false
		reasons = append(reasons, fmt.Sprintf("budget: %s", budget.Reason))
		actions = append(actions, "increase budget or stop the loop")
		lc.logger.Warn("guardrail: budget exhausted", "reason", budget.Reason)
	} else if budget.Warning {
		lc.logger.Warn("guardrail: budget warning", "reason", budget.Reason)
	}

	return GuardrailStatus{
		ShouldContinue: shouldContinue,
		Stagnation:     stagnation,
		CircuitState:   lc.cb.State(),
		Budget:         budget,
		ActiveReasons:  reasons,
		Actions:        actions,
	}
}

// GetStatus returns the current guardrail status without modifying state.
func (lc *LoopController) GetStatus() GuardrailStatus {
	return lc.ShouldContinue()
}

// RalphWiggum returns the underlying stagnation detector.
func (lc *LoopController) RalphWiggum() *RalphWiggumDetector {
	return lc.rw
}

// CircuitBreaker returns the underlying circuit breaker.
func (lc *LoopController) CircuitBreaker() *CircuitBreaker {
	return lc.cb
}

// BudgetGuard returns the underlying budget guard.
func (lc *LoopController) BudgetGuard() *BudgetGuard {
	return lc.bg
}
