package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nfsarch33/helixon-autoresearch/eval"
)

// ResearchStageID identifies one of the 10 Karpathy-methodology stages.
type ResearchStageID int

const (
	StageQuestionFormulation ResearchStageID = iota + 1
	StageLiteratureReview
	StageHypothesisGeneration
	StageExperimentalDesign
	StageImplementation
	StageExecution
	StageAnalysis
	StageValidation
	StageDocumentation
	StageDissemination
)

// AllStages returns the 10 stages in order.
func AllStages() []ResearchStageID {
	return []ResearchStageID{
		StageQuestionFormulation,
		StageLiteratureReview,
		StageHypothesisGeneration,
		StageExperimentalDesign,
		StageImplementation,
		StageExecution,
		StageAnalysis,
		StageValidation,
		StageDocumentation,
		StageDissemination,
	}
}

func (s ResearchStageID) String() string {
	switch s {
	case StageQuestionFormulation:
		return "question_formulation"
	case StageLiteratureReview:
		return "literature_review"
	case StageHypothesisGeneration:
		return "hypothesis_generation"
	case StageExperimentalDesign:
		return "experimental_design"
	case StageImplementation:
		return "implementation"
	case StageExecution:
		return "execution"
	case StageAnalysis:
		return "analysis"
	case StageValidation:
		return "validation"
	case StageDocumentation:
		return "documentation"
	case StageDissemination:
		return "dissemination"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// StageResult captures the outcome of a single pipeline stage.
type StageResult struct {
	Stage     ResearchStageID `json:"stage"`
	Name      string          `json:"name"`
	Status    string          `json:"status"` // running/completed/failed
	StartedAt time.Time       `json:"started_at"`
	EndedAt   time.Time       `json:"ended_at,omitempty"`
	Output    string          `json:"output,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// EvalExperiment is the top-level experiment record tying the 10-stage
// pipeline to an eval harness report.
type EvalExperiment struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Hypothesis   string          `json:"hypothesis"`
	Question     string          `json:"question"`
	Stages       []StageResult   `json:"stages"`
	CurrentStage string          `json:"current_stage"`
	Report       eval.EvalReport `json:"report"`
	CreatedAt    time.Time       `json:"created_at"`
}

// PipelineRunner orchestrates the 10-stage academic research pipeline,
// delegating the execution stage to the eval harness and persisting
// results to Engram at each stage boundary.
type PipelineRunner struct {
	Harness      *eval.EvalHarness
	Persist      *EngramPersistor
	Logger       *slog.Logger
	StageTimeout time.Duration
}

// NewPipelineRunner constructs a runner. The harness and persistor must
// be pre-configured (keys resolved, EngramURL set).
func NewPipelineRunner(harness *eval.EvalHarness, persistor *EngramPersistor, logger *slog.Logger) *PipelineRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &PipelineRunner{
		Harness:      harness,
		Persist:      persistor,
		Logger:       logger,
		StageTimeout: 30 * time.Minute,
	}
}

// RunExperiment executes the full 10-stage pipeline for one eval
// experiment. The execution stage (stage 6) runs the eval harness; all
// other stages are lightweight metadata stages that record the
// research narrative and persist progress to Engram.
func (r *PipelineRunner) RunExperiment(ctx context.Context, exp *EvalExperiment) error {
	if exp == nil {
		return fmt.Errorf("experiment is nil")
	}
	exp.CreatedAt = time.Now().UTC()
	exp.Stages = make([]StageResult, 0, 10)

	for _, stage := range AllStages() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		sr := StageResult{
			Stage:     stage,
			Name:      stage.String(),
			Status:    "running",
			StartedAt: time.Now().UTC(),
		}
		exp.CurrentStage = stage.String()
		r.Logger.Info("stage started", "experiment", exp.ID, "stage", sr.Name)

		stageCtx, cancel := context.WithTimeout(ctx, r.StageTimeout)
		err := r.runStage(stageCtx, exp, stage, &sr)
		cancel()

		sr.EndedAt = time.Now().UTC()
		if err != nil {
			sr.Status = "failed"
			sr.Error = err.Error()
			exp.Stages = append(exp.Stages, sr)
			r.persistCheckpoint(ctx, exp)
			return fmt.Errorf("stage %s failed: %w", sr.Name, err)
		}
		sr.Status = "completed"
		exp.Stages = append(exp.Stages, sr)
		r.persistCheckpoint(ctx, exp)
		r.Logger.Info("stage completed", "experiment", exp.ID, "stage", sr.Name)
	}

	exp.CurrentStage = "completed"
	r.Logger.Info("experiment pipeline complete",
		"experiment", exp.ID,
		"overall_verdict", exp.Report.Summary.OverallVerdict,
		"best_backend", exp.Report.Summary.BestBackend,
	)
	return nil
}

// runStage dispatches a single stage. StageExecution runs the harness;
// all others are metadata-only (the narrative is captured in the
// experiment struct by the caller).
func (r *PipelineRunner) runStage(ctx context.Context, exp *EvalExperiment, stage ResearchStageID, sr *StageResult) error {
	switch stage {
	case StageExecution:
		// The eval harness is the experiment's execution stage.
		report, err := r.Harness.Run(ctx)
		if err != nil {
			return fmt.Errorf("harness run: %w", err)
		}
		exp.Report = *report
		summary, _ := json.Marshal(report.Summary)
		sr.Output = string(summary)
		return nil
	case StageAnalysis:
		// Summarize the comparative matrix as the analysis output.
		sr.Output = exp.Report.RenderText()
		return nil
	case StageDocumentation:
		// The eval report JSON is the documentation artifact.
		doc, _ := json.MarshalIndent(exp.Report, "", "  ")
		sr.Output = string(doc)
		return nil
	case StageDissemination:
		// Final persistence + logging is the dissemination stage.
		sr.Output = fmt.Sprintf("persisted to engram; best_backend=%s verdict=%s",
			exp.Report.Summary.BestBackend, exp.Report.Summary.OverallVerdict)
		return nil
	default:
		// Metadata stages: no heavy work, just record the narrative.
		sr.Output = exp.Name + " :: " + stage.String()
		return nil
	}
}

// persistCheckpoint saves the experiment to Engram after each stage so
// partial progress survives crashes.
func (r *PipelineRunner) persistCheckpoint(ctx context.Context, exp *EvalExperiment) {
	if r.Persist == nil {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := r.Persist.SaveExperiment(pctx, *exp); err != nil {
		r.Logger.Error("failed to persist experiment checkpoint", "experiment", exp.ID, "err", err)
	}
}
