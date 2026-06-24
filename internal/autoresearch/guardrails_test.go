package autoresearch

import (
	"testing"
	"time"
)

func TestRalphWiggum_StagnationDetection(t *testing.T) {
	cfg := RalphWiggumConfig{
		WindowSize:         5,
		MinImprovement:     0.01,
		MaxStagnationCount: 3,
	}
	detector := NewRalphWiggumDetector(cfg)

	for i := 0; i < 5; i++ {
		detector.RecordScore(0.5)
	}

	report := detector.DetectStagnation()

	if !report.Stagnant {
		t.Error("expected stagnation to be detected")
	}
	if report.Reason == "" {
		t.Error("expected stagnation reason to be populated")
	}
	if report.WindowSize != 5 {
		t.Errorf("expected window size 5, got %d", report.WindowSize)
	}
}

func TestRalphWiggum_NoStagnation(t *testing.T) {
	cfg := RalphWiggumConfig{
		WindowSize:         5,
		MinImprovement:     0.01,
		MaxStagnationCount: 3,
	}
	detector := NewRalphWiggumDetector(cfg)

	for i := 0; i < 5; i++ {
		detector.RecordScore(float64(i) * 0.2)
	}

	report := detector.DetectStagnation()

	if report.Stagnant {
		t.Error("expected no stagnation with improving scores")
	}
}

func TestRalphWiggum_EmptyWindow(t *testing.T) {
	cfg := RalphWiggumConfig{
		WindowSize:         5,
		MinImprovement:     0.01,
		MaxStagnationCount: 3,
	}
	detector := NewRalphWiggumDetector(cfg)

	report := detector.DetectStagnation()

	if report.Stagnant {
		t.Error("expected no stagnation for empty window")
	}
	if report.Reason != "insufficient data" {
		t.Errorf("expected reason 'insufficient data', got %q", report.Reason)
	}
	if report.WindowSize != 0 {
		t.Errorf("expected window size 0, got %d", report.WindowSize)
	}
}

func TestRalphWiggum_SingleScore(t *testing.T) {
	cfg := RalphWiggumConfig{
		WindowSize:         5,
		MinImprovement:     0.01,
		MaxStagnationCount: 3,
	}
	detector := NewRalphWiggumDetector(cfg)

	detector.RecordScore(0.5)

	report := detector.DetectStagnation()

	if report.Stagnant {
		t.Error("expected no stagnation with single score")
	}
	if report.Reason != "insufficient data" {
		t.Errorf("expected reason 'insufficient data', got %q", report.Reason)
	}
}

func TestCircuitBreaker_NormalOperation(t *testing.T) {
	cfg := CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		CooldownPeriod:         1 * time.Minute,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordResult(true)
	cb.RecordResult(true)

	if cb.State() != CircuitClosed {
		t.Errorf("expected state CLOSED, got %v", cb.State())
	}
	if cb.IsOpen() {
		t.Error("expected circuit breaker to be closed")
	}
	if cb.ConsecutiveFailures() != 0 {
		t.Errorf("expected 0 consecutive failures, got %d", cb.ConsecutiveFailures())
	}
}

func TestCircuitBreaker_TripsOnConsecutiveFailures(t *testing.T) {
	cfg := CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		CooldownPeriod:         1 * time.Minute,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordResult(false)
	cb.RecordResult(false)
	cb.RecordResult(false)

	if cb.State() != CircuitOpen {
		t.Errorf("expected state OPEN, got %v", cb.State())
	}
	if !cb.IsOpen() {
		t.Error("expected circuit breaker to be open")
	}
	if cb.ConsecutiveFailures() != 3 {
		t.Errorf("expected 3 consecutive failures, got %d", cb.ConsecutiveFailures())
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	cfg := CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		CooldownPeriod:         10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordResult(false)
	cb.RecordResult(false)
	cb.RecordResult(false)

	if cb.State() != CircuitOpen {
		t.Error("expected state OPEN after failures")
	}

	time.Sleep(20 * time.Millisecond)

	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected state HALF_OPEN after cooldown, got %v", cb.State())
	}

	cb.RecordResult(true)

	if cb.State() != CircuitClosed {
		t.Errorf("expected state CLOSED after recovery, got %v", cb.State())
	}
}

func TestCircuitBreaker_CooldownPeriod(t *testing.T) {
	cfg := CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		CooldownPeriod:         100 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordResult(false)
	cb.RecordResult(false)
	cb.RecordResult(false)

	if !cb.IsOpen() {
		t.Error("expected circuit breaker to be open immediately after tripping")
	}

	time.Sleep(50 * time.Millisecond)

	if !cb.IsOpen() {
		t.Error("expected circuit breaker to still be open before cooldown")
	}

	time.Sleep(60 * time.Millisecond)

	if cb.IsOpen() {
		t.Error("expected circuit breaker to transition after cooldown")
	}
}

func TestCircuitBreaker_FailureResetsOnSuccess(t *testing.T) {
	cfg := CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		CooldownPeriod:         1 * time.Minute,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordResult(false)
	cb.RecordResult(false)

	if cb.ConsecutiveFailures() != 2 {
		t.Errorf("expected 2 consecutive failures, got %d", cb.ConsecutiveFailures())
	}

	cb.RecordResult(true)

	if cb.ConsecutiveFailures() != 0 {
		t.Errorf("expected 0 consecutive failures after success, got %d", cb.ConsecutiveFailures())
	}
	if cb.State() != CircuitClosed {
		t.Errorf("expected state CLOSED, got %v", cb.State())
	}
}

func TestBudgetGuard_CreditLimit(t *testing.T) {
	cfg := BudgetGuardConfig{
		MaxCredits:       1000,
		MaxDuration:      24 * time.Hour,
		WarningThreshold: 0.8,
	}
	bg := NewBudgetGuard(cfg)

	bg.RecordSpend(500)

	if bg.IsExhausted() {
		t.Error("expected budget not exhausted at 50%")
	}

	credits, _ := bg.RemainingBudget()
	if credits != 500 {
		t.Errorf("expected 500 credits remaining, got %f", credits)
	}

	bg.RecordSpend(600)

	if !bg.IsExhausted() {
		t.Error("expected budget exhausted after overspend")
	}

	report := bg.Report()
	if !report.Exhausted {
		t.Error("expected report to show exhausted")
	}
	if report.CreditsRemaining != 0 {
		t.Errorf("expected 0 credits remaining, got %f", report.CreditsRemaining)
	}
}

func TestBudgetGuard_TimeLimit(t *testing.T) {
	cfg := BudgetGuardConfig{
		MaxCredits:       10000,
		MaxDuration:      10 * time.Millisecond,
		WarningThreshold: 0.8,
	}
	bg := NewBudgetGuard(cfg)

	time.Sleep(20 * time.Millisecond)

	if !bg.IsExhausted() {
		t.Error("expected budget exhausted due to time limit")
	}

	_, duration := bg.RemainingBudget()
	if duration != 0 {
		t.Errorf("expected 0 duration remaining, got %v", duration)
	}
}

func TestBudgetGuard_WarningThreshold(t *testing.T) {
	cfg := BudgetGuardConfig{
		MaxCredits:       1000,
		MaxDuration:      24 * time.Hour,
		WarningThreshold: 0.8,
	}
	bg := NewBudgetGuard(cfg)

	bg.RecordSpend(700)

	report := bg.Report()
	if report.Warning {
		t.Error("expected no warning at 70% usage")
	}

	bg.RecordSpend(150)

	report = bg.Report()
	if !report.Warning {
		t.Error("expected warning at 85% usage")
	}
	if report.Exhausted {
		t.Error("expected not exhausted at 85%")
	}
}

func TestLoopController_AllGuardrails(t *testing.T) {
	lc := NewLoopController(
		RalphWiggumConfig{
			WindowSize:         5,
			MinImprovement:     0.01,
			MaxStagnationCount: 3,
		},
		CircuitBreakerConfig{
			MaxConsecutiveFailures: 3,
			CooldownPeriod:         1 * time.Minute,
		},
		BudgetGuardConfig{
			MaxCredits:       1000,
			MaxDuration:      24 * time.Hour,
			WarningThreshold: 0.8,
		},
		nil,
	)

	for i := 0; i < 5; i++ {
		lc.RecordExperiment(0.5, 50, false)
	}

	status := lc.ShouldContinue()

	if status.ShouldContinue {
		t.Error("expected loop to stop when circuit breaker trips")
	}
	if status.CircuitState != CircuitOpen {
		t.Errorf("expected circuit OPEN, got %v", status.CircuitState)
	}
	if len(status.ActiveReasons) == 0 {
		t.Error("expected active reasons to be populated")
	}
}

func TestLoopController_ShouldContinue(t *testing.T) {
	lc := NewLoopController(
		RalphWiggumConfig{
			WindowSize:         5,
			MinImprovement:     0.01,
			MaxStagnationCount: 10,
		},
		CircuitBreakerConfig{
			MaxConsecutiveFailures: 10,
			CooldownPeriod:         1 * time.Minute,
		},
		BudgetGuardConfig{
			MaxCredits:       10000,
			MaxDuration:      24 * time.Hour,
			WarningThreshold: 0.8,
		},
		nil,
	)

	lc.RecordExperiment(0.5, 100, true)
	lc.RecordExperiment(0.6, 100, true)
	lc.RecordExperiment(0.7, 100, true)

	status := lc.ShouldContinue()

	if !status.ShouldContinue {
		t.Error("expected loop to continue with healthy metrics")
	}
	if len(status.ActiveReasons) != 0 {
		t.Errorf("expected no active reasons, got %v", status.ActiveReasons)
	}
	if status.CircuitState != CircuitClosed {
		t.Errorf("expected circuit CLOSED, got %v", status.CircuitState)
	}
}
