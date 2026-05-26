package autoresearch

import (
	"testing"
	"time"
)

func TestExperimentPhaseString(t *testing.T) {
	tests := []struct {
		phase ExperimentPhase
		want  string
	}{
		{PhaseIdeation, "ideation"},
		{PhaseImplementation, "implementation"},
		{PhaseTraining, "training"},
		{PhaseEvaluation, "evaluation"},
		{PhaseComparison, "comparison"},
		{PhasePromotion, "promotion"},
		{ExperimentPhase(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.want {
			t.Errorf("ExperimentPhase(%d).String() = %q, want %q", int(tt.phase), got, tt.want)
		}
	}
}

func TestExperimentConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  ExperimentConfig
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  ExperimentConfig{Name: "test-exp", Hypothesis: "faster training"},
			wantErr: false,
		},
		{
			name:    "missing name",
			config:  ExperimentConfig{Hypothesis: "faster training"},
			wantErr: true,
		},
		{
			name:    "missing hypothesis",
			config:  ExperimentConfig{Name: "test-exp"},
			wantErr: true,
		},
		{
			name:    "both missing",
			config:  ExperimentConfig{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExperimentResultFields(t *testing.T) {
	now := time.Now()
	result := ExperimentResult{
		ID:         "exp-001",
		Name:       "lr-sweep",
		Hypothesis: "lower lr improves convergence",
		CodeChanges: []string{
			"config/train.yaml: lr 1e-4 -> 1e-5",
		},
		Metrics: Metrics{
			Before: map[string]float64{"loss": 2.5, "accuracy": 0.72},
			After:  map[string]float64{"loss": 1.8, "accuracy": 0.81},
		},
		Duration:  5 * time.Minute,
		Timestamp: now,
		Status:    StatusCompleted,
		Phase:     PhaseEvaluation,
	}

	if result.ID != "exp-001" {
		t.Errorf("ID = %q, want exp-001", result.ID)
	}
	if result.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
	if result.Phase != PhaseEvaluation {
		t.Errorf("Phase = %v, want evaluation", result.Phase)
	}
	if len(result.CodeChanges) != 1 {
		t.Errorf("CodeChanges len = %d, want 1", len(result.CodeChanges))
	}
	if result.Metrics.After["accuracy"] != 0.81 {
		t.Errorf("After accuracy = %f, want 0.81", result.Metrics.After["accuracy"])
	}
}

func TestExperimentStatusValues(t *testing.T) {
	statuses := []ExperimentStatus{
		StatusPending, StatusRunning, StatusCompleted, StatusFailed, StatusSkipped,
	}
	expected := []string{"pending", "running", "completed", "failed", "skipped"}

	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("status[%d] = %q, want %q", i, string(s), expected[i])
		}
	}
}
