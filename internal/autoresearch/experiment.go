package autoresearch

import (
	"fmt"
	"time"
)

type ExperimentPhase int

const (
	PhaseIdeation ExperimentPhase = iota
	PhaseImplementation
	PhaseTraining
	PhaseEvaluation
	PhaseComparison
	PhasePromotion
)

func (p ExperimentPhase) String() string {
	switch p {
	case PhaseIdeation:
		return "ideation"
	case PhaseImplementation:
		return "implementation"
	case PhaseTraining:
		return "training"
	case PhaseEvaluation:
		return "evaluation"
	case PhaseComparison:
		return "comparison"
	case PhasePromotion:
		return "promotion"
	default:
		return fmt.Sprintf("unknown(%d)", int(p))
	}
}

type ExperimentStatus string

const (
	StatusPending   ExperimentStatus = "pending"
	StatusRunning   ExperimentStatus = "running"
	StatusCompleted ExperimentStatus = "completed"
	StatusFailed    ExperimentStatus = "failed"
	StatusSkipped   ExperimentStatus = "skipped"
)

type Metrics struct {
	Before map[string]float64 `json:"before"`
	After  map[string]float64 `json:"after"`
}

type ExperimentResult struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Hypothesis  string           `json:"hypothesis"`
	CodeChanges []string         `json:"code_changes"`
	Metrics     Metrics          `json:"metrics"`
	Duration    time.Duration    `json:"duration_ns"`
	Timestamp   time.Time        `json:"timestamp"`
	Status      ExperimentStatus `json:"status"`
	Phase       ExperimentPhase  `json:"phase"`
	Error       string           `json:"error,omitempty"`
}

type ExperimentConfig struct {
	Name       string            `json:"name"`
	Hypothesis string            `json:"hypothesis"`
	BaseDir    string            `json:"base_dir"`
	Tags       map[string]string `json:"tags,omitempty"`
	Timeout    time.Duration     `json:"timeout_ns"`
}

func (c ExperimentConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("experiment name is required")
	}
	if c.Hypothesis == "" {
		return fmt.Errorf("hypothesis is required")
	}
	return nil
}
