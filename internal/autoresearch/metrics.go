package autoresearch

import (
	"sync"
	"time"
)

// ExperimentMetrics collects timing and outcome data for experiment runs.
type ExperimentMetrics struct {
	mu             sync.Mutex
	totalRuns      int
	successCount   int
	failureCount   int
	skipCount      int
	dupCount       int
	totalDuration  time.Duration
	phaseDurations map[ExperimentPhase][]time.Duration
}

// NewExperimentMetrics creates a zero-value metrics collector.
func NewExperimentMetrics() *ExperimentMetrics {
	return &ExperimentMetrics{
		phaseDurations: make(map[ExperimentPhase][]time.Duration),
	}
}

// RecordRun records the outcome and duration of an experiment run.
func (m *ExperimentMetrics) RecordRun(status ExperimentStatus, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalRuns++
	m.totalDuration += duration
	switch status {
	case StatusCompleted:
		m.successCount++
	case StatusFailed:
		m.failureCount++
	case StatusSkipped:
		m.skipCount++
	}
}

// RecordDuplicate increments the duplicate-skipped counter.
func (m *ExperimentMetrics) RecordDuplicate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dupCount++
}

// RecordPhase records the duration of a single experiment phase.
func (m *ExperimentMetrics) RecordPhase(phase ExperimentPhase, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phaseDurations[phase] = append(m.phaseDurations[phase], duration)
}

// MetricsSnapshot is a point-in-time copy of collected metrics.
type MetricsSnapshot struct {
	TotalRuns      int                               `json:"total_runs"`
	SuccessCount   int                               `json:"success_count"`
	FailureCount   int                               `json:"failure_count"`
	SkipCount      int                               `json:"skip_count"`
	DuplicateCount int                               `json:"duplicate_count"`
	TotalDuration  time.Duration                     `json:"total_duration_ns"`
	AvgDuration    time.Duration                     `json:"avg_duration_ns"`
	PhaseDurations map[ExperimentPhase]PhaseDurStats `json:"phase_durations"`
}

// PhaseDurStats summarises timing for a single phase across runs.
type PhaseDurStats struct {
	Count int           `json:"count"`
	Total time.Duration `json:"total_ns"`
	Avg   time.Duration `json:"avg_ns"`
	Min   time.Duration `json:"min_ns"`
	Max   time.Duration `json:"max_ns"`
}

// Snapshot returns a point-in-time copy of the metrics.
func (m *ExperimentMetrics) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := MetricsSnapshot{
		TotalRuns:      m.totalRuns,
		SuccessCount:   m.successCount,
		FailureCount:   m.failureCount,
		SkipCount:      m.skipCount,
		DuplicateCount: m.dupCount,
		TotalDuration:  m.totalDuration,
		PhaseDurations: make(map[ExperimentPhase]PhaseDurStats),
	}
	if m.totalRuns > 0 {
		snap.AvgDuration = m.totalDuration / time.Duration(m.totalRuns)
	}
	for phase, durations := range m.phaseDurations {
		stats := computePhaseStats(durations)
		snap.PhaseDurations[phase] = stats
	}
	return snap
}

func computePhaseStats(durations []time.Duration) PhaseDurStats {
	if len(durations) == 0 {
		return PhaseDurStats{}
	}
	stats := PhaseDurStats{
		Count: len(durations),
		Min:   durations[0],
		Max:   durations[0],
	}
	for _, d := range durations {
		stats.Total += d
		if d < stats.Min {
			stats.Min = d
		}
		if d > stats.Max {
			stats.Max = d
		}
	}
	stats.Avg = stats.Total / time.Duration(stats.Count)
	return stats
}
