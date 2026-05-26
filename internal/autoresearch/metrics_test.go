package autoresearch

import (
	"sync"
	"testing"
	"time"
)

func TestExperimentMetrics_RecordRun(t *testing.T) {
	m := NewExperimentMetrics()

	m.RecordRun(StatusCompleted, 10*time.Second)
	m.RecordRun(StatusCompleted, 20*time.Second)
	m.RecordRun(StatusFailed, 5*time.Second)
	m.RecordRun(StatusSkipped, 1*time.Second)

	snap := m.Snapshot()
	if snap.TotalRuns != 4 {
		t.Errorf("TotalRuns = %d, want 4", snap.TotalRuns)
	}
	if snap.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", snap.SuccessCount)
	}
	if snap.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", snap.FailureCount)
	}
	if snap.SkipCount != 1 {
		t.Errorf("SkipCount = %d, want 1", snap.SkipCount)
	}
	if snap.TotalDuration != 36*time.Second {
		t.Errorf("TotalDuration = %v, want 36s", snap.TotalDuration)
	}
	if snap.AvgDuration != 9*time.Second {
		t.Errorf("AvgDuration = %v, want 9s", snap.AvgDuration)
	}
}

func TestExperimentMetrics_RecordDuplicate(t *testing.T) {
	m := NewExperimentMetrics()
	m.RecordDuplicate()
	m.RecordDuplicate()

	snap := m.Snapshot()
	if snap.DuplicateCount != 2 {
		t.Errorf("DuplicateCount = %d, want 2", snap.DuplicateCount)
	}
}

func TestExperimentMetrics_RecordPhase(t *testing.T) {
	m := NewExperimentMetrics()
	m.RecordPhase(PhaseTraining, 30*time.Second)
	m.RecordPhase(PhaseTraining, 40*time.Second)
	m.RecordPhase(PhaseTraining, 20*time.Second)
	m.RecordPhase(PhaseEvaluation, 10*time.Second)

	snap := m.Snapshot()
	training, ok := snap.PhaseDurations[PhaseTraining]
	if !ok {
		t.Fatal("missing PhaseTraining stats")
	}
	if training.Count != 3 {
		t.Errorf("training.Count = %d, want 3", training.Count)
	}
	if training.Min != 20*time.Second {
		t.Errorf("training.Min = %v, want 20s", training.Min)
	}
	if training.Max != 40*time.Second {
		t.Errorf("training.Max = %v, want 40s", training.Max)
	}
	if training.Avg != 30*time.Second {
		t.Errorf("training.Avg = %v, want 30s", training.Avg)
	}

	eval, ok := snap.PhaseDurations[PhaseEvaluation]
	if !ok {
		t.Fatal("missing PhaseEvaluation stats")
	}
	if eval.Count != 1 {
		t.Errorf("eval.Count = %d, want 1", eval.Count)
	}
}

func TestExperimentMetrics_EmptySnapshot(t *testing.T) {
	m := NewExperimentMetrics()
	snap := m.Snapshot()

	if snap.TotalRuns != 0 {
		t.Errorf("TotalRuns = %d, want 0", snap.TotalRuns)
	}
	if snap.AvgDuration != 0 {
		t.Errorf("AvgDuration = %v, want 0", snap.AvgDuration)
	}
	if len(snap.PhaseDurations) != 0 {
		t.Errorf("PhaseDurations should be empty, got %d", len(snap.PhaseDurations))
	}
}

func TestExperimentMetrics_ConcurrentSafety(t *testing.T) {
	m := NewExperimentMetrics()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			m.RecordRun(StatusCompleted, time.Second)
		}()
		go func() {
			defer wg.Done()
			m.RecordPhase(PhaseTraining, time.Second)
		}()
		go func() {
			defer wg.Done()
			_ = m.Snapshot()
		}()
	}
	wg.Wait()

	snap := m.Snapshot()
	if snap.TotalRuns != 100 {
		t.Errorf("TotalRuns = %d, want 100", snap.TotalRuns)
	}
}

func TestComputePhaseStats_Empty(t *testing.T) {
	stats := computePhaseStats(nil)
	if stats.Count != 0 {
		t.Errorf("Count = %d, want 0", stats.Count)
	}
}

func TestNewExperimentMetrics(t *testing.T) {
	m := NewExperimentMetrics()
	if m == nil {
		t.Fatal("NewExperimentMetrics returned nil")
	}
	if m.phaseDurations == nil {
		t.Error("phaseDurations map should be initialized")
	}
}
