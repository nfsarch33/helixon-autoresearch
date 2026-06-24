// Package eval implements the Helixon agent-centric evaluation harness.
//
// This package evaluates Helixon platform/fleet AGENTS (not models in
// isolation) using different LLM backends. An agent is deployed with a
// given backend, runs a task suite, and outputs are scored with
// rubric-based metrics using the G-Eval LLM-as-judge pattern.
//
// Critical distinction: we vary the LLM backend to understand how the
// same agent performs across backends; we are NOT comparing the models
// themselves in isolation.
package eval

import "fmt"

// RubricVersion is the version tag stamped onto every report so scores
// remain comparable across harness revisions. Bump when criteria change.
const RubricVersion = "2.0.0"

// ScaleMax is the per-criterion maximum score (1..ScaleMax). A 1-5 scale
// matches the G-Eval convention and keeps judge variance manageable.
const ScaleMax = 5

// CriterionID is a stable identifier for a single rubric criterion.
type CriterionID string

// The seven agent-centric metrics required by Sprint B. Each ID is stable
// across rubric versions so reports stay comparable.
const (
	CritTaskCompletion  CriterionID = "task_completion"
	CritCodeQuality     CriterionID = "code_quality"
	CritTokenEfficiency CriterionID = "token_efficiency"
	CritContextUse      CriterionID = "context_utilization"
	CritSelfImprovement CriterionID = "self_improvement"
	CritLongSessionStab CriterionID = "long_session_stability"
	CritErrorRecovery   CriterionID = "error_recovery"
)

// Criterion defines a single scoring dimension with anchored levels.
type Criterion struct {
	ID          CriterionID `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Weight      float64     `json:"weight"` // relative weight, sums to 1.0 across a rubric
	// Anchors describe what a score of 1, 3, and ScaleMax looks like so the
	// LLM judge has a grounded reference instead of an unanchored 1-5 scale.
	AnchorLow  string `json:"anchor_low"`  // score = 1
	AnchorMid  string `json:"anchor_mid"`  // score = 3
	AnchorHigh string `json:"anchor_high"` // score = ScaleMax
}

// Rubric is the versioned set of criteria applied to one task type.
type Rubric struct {
	Name       string      `json:"name"`
	Version    string      `json:"version"`
	TaskTypeID TaskTypeID  `json:"task_type_id"`
	Criteria   []Criterion `json:"criteria"`
}

// DefaultRubrics returns the seven-metric rubric mapped to each of the
// seven task types. Weights are tuned per task type: e.g. code-quality
// weighs heaviest on code generation, while self-improvement weighs
// heaviest on the self-improvement task.
func DefaultRubrics() map[TaskTypeID]*Rubric {
	return map[TaskTypeID]*Rubric{
		TaskCodeGeneration:  rubricCodeGeneration(),
		TaskCodeReview:      rubricCodeReview(),
		TaskDebugging:       rubricDebugging(),
		TaskDocumentation:   rubricDocumentation(),
		TaskLongContext:     rubricLongContext(),
		TaskSelfImprovement: rubricSelfImprovement(),
		TaskMultiStepPlan:   rubricMultiStepPlan(),
	}
}

// rubricFor returns the rubric for a task type, falling back to a
// generic balanced rubric if the type is unknown.
func rubricFor(id TaskTypeID) (*Rubric, error) {
	if r, ok := DefaultRubrics()[id]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("no default rubric for task type %q", id)
}

// --- Shared criterion definitions (anchored to the 1-5 G-Eval scale) ---

var critTaskCompletion = Criterion{
	ID:          CritTaskCompletion,
	Name:        "Task Completion",
	Description: "Did the agent fully satisfy the task brief, including implicit requirements and edge cases?",
	Weight:      0.20,
	AnchorLow:   "1 = Output is missing, off-topic, or fails the primary objective.",
	AnchorMid:   "3 = Primary objective met but edge cases or implicit requirements ignored.",
	AnchorHigh:  "5 = All explicit and implicit requirements satisfied; edge cases handled.",
}

var critCodeQuality = Criterion{
	ID:          CritCodeQuality,
	Name:        "Code Quality (SOLID/DRY/KISS)",
	Description: "Adherence to SOLID, DRY, KISS principles; readability, naming, and structure.",
	Weight:      0.15,
	AnchorLow:   "1 = Tangled, duplicated, or unreadable code with poor names.",
	AnchorMid:   "3 = Mostly readable; minor DRY/SOLID violations.",
	AnchorHigh:  "5 = Clean, idiomatic, well-factored code with clear abstractions.",
}

var critTokenEfficiency = Criterion{
	ID:          CritTokenEfficiency,
	Name:        "Token Efficiency",
	Description: "Tokens used relative to task complexity. Penalize both verbose rambling and under-specified terseness.",
	Weight:      0.10,
	AnchorLow:   "1 = Grossly wasteful (>3x expected tokens) or unusably terse.",
	AnchorMid:   "3 = Reasonable token use with some redundancy.",
	AnchorHigh:  "5 = Minimal tokens for maximum information; no redundancy.",
}

var critContextUse = Criterion{
	ID:          CritContextUse,
	Name:        "Context Utilization",
	Description: "Did the agent effectively use the provided context (codebase, prior turns, retrieved memory)?",
	Weight:      0.15,
	AnchorLow:   "1 = Ignored provided context; hallucinated or contradicted it.",
	AnchorMid:   "3 = Used some context but missed relevant signals.",
	AnchorHigh:  "5 = Synthesized provided context into a well-grounded answer.",
}

var critSelfImprovement = Criterion{
	ID:          CritSelfImprovement,
	Name:        "Self-Improvement",
	Description: "Did the agent identify its own mistakes and correct them within the session?",
	Weight:      0.10,
	AnchorLow:   "1 = Repeated the same mistake; no self-correction.",
	AnchorMid:   "3 = Noticed some mistakes but did not fully fix them.",
	AnchorHigh:  "5 = Identified and corrected mistakes with clear reasoning.",
}

var critLongSessionStab = Criterion{
	ID:          CritLongSessionStab,
	Name:        "Long-Session Stability",
	Description: "Coherence, consistency, and goal retention across a long multi-turn session.",
	Weight:      0.15,
	AnchorLow:   "1 = Lost the thread, contradicted earlier turns, or drifted.",
	AnchorMid:   "3 = Mostly coherent with minor drift late in the session.",
	AnchorHigh:  "5 = Fully coherent end-to-end; goals and constraints retained.",
}

var critErrorRecovery = Criterion{
	ID:          CritErrorRecovery,
	Name:        "Error Recovery",
	Description: "How well the agent detected, diagnosed, and recovered from errors (tool failures, bad state).",
	Weight:      0.15,
	AnchorLow:   "1 = Crashed, looped, or ignored errors entirely.",
	AnchorMid:   "3 = Surface-level error handling without root-cause diagnosis.",
	AnchorHigh:  "5 = Diagnosed root cause, recovered gracefully, and prevented recurrence.",
}

// --- Per-task-type rubrics (weights rebalanced per task) ---

func rubricCodeGeneration() *Rubric {
	return &Rubric{
		Name:       "Code Generation Rubric",
		Version:    RubricVersion,
		TaskTypeID: TaskCodeGeneration,
		Criteria: []Criterion{
			withWeight(critTaskCompletion, 0.25),
			withWeight(critCodeQuality, 0.30),
			withWeight(critTokenEfficiency, 0.10),
			withWeight(critContextUse, 0.10),
			withWeight(critSelfImprovement, 0.05),
			withWeight(critLongSessionStab, 0.05),
			withWeight(critErrorRecovery, 0.15),
		},
	}
}

func rubricCodeReview() *Rubric {
	return &Rubric{
		Name:       "Code Review Rubric",
		Version:    RubricVersion,
		TaskTypeID: TaskCodeReview,
		Criteria: []Criterion{
			withWeight(critTaskCompletion, 0.20),
			withWeight(critCodeQuality, 0.20),
			withWeight(critTokenEfficiency, 0.10),
			withWeight(critContextUse, 0.20),
			withWeight(critSelfImprovement, 0.05),
			withWeight(critLongSessionStab, 0.05),
			withWeight(critErrorRecovery, 0.20),
		},
	}
}

func rubricDebugging() *Rubric {
	return &Rubric{
		Name:       "Debugging Rubric",
		Version:    RubricVersion,
		TaskTypeID: TaskDebugging,
		Criteria: []Criterion{
			withWeight(critTaskCompletion, 0.20),
			withWeight(critCodeQuality, 0.15),
			withWeight(critTokenEfficiency, 0.10),
			withWeight(critContextUse, 0.15),
			withWeight(critSelfImprovement, 0.10),
			withWeight(critLongSessionStab, 0.05),
			withWeight(critErrorRecovery, 0.25),
		},
	}
}

func rubricDocumentation() *Rubric {
	return &Rubric{
		Name:       "Documentation Rubric",
		Version:    RubricVersion,
		TaskTypeID: TaskDocumentation,
		Criteria: []Criterion{
			withWeight(critTaskCompletion, 0.25),
			withWeight(critCodeQuality, 0.15),
			withWeight(critTokenEfficiency, 0.15),
			withWeight(critContextUse, 0.20),
			withWeight(critSelfImprovement, 0.05),
			withWeight(critLongSessionStab, 0.05),
			withWeight(critErrorRecovery, 0.15),
		},
	}
}

func rubricLongContext() *Rubric {
	return &Rubric{
		Name:       "Long-Context Rubric",
		Version:    RubricVersion,
		TaskTypeID: TaskLongContext,
		Criteria: []Criterion{
			withWeight(critTaskCompletion, 0.15),
			withWeight(critCodeQuality, 0.10),
			withWeight(critTokenEfficiency, 0.15),
			withWeight(critContextUse, 0.25),
			withWeight(critSelfImprovement, 0.05),
			withWeight(critLongSessionStab, 0.25),
			withWeight(critErrorRecovery, 0.05),
		},
	}
}

func rubricSelfImprovement() *Rubric {
	return &Rubric{
		Name:       "Self-Improvement Rubric",
		Version:    RubricVersion,
		TaskTypeID: TaskSelfImprovement,
		Criteria: []Criterion{
			withWeight(critTaskCompletion, 0.15),
			withWeight(critCodeQuality, 0.10),
			withWeight(critTokenEfficiency, 0.10),
			withWeight(critContextUse, 0.10),
			withWeight(critSelfImprovement, 0.35),
			withWeight(critLongSessionStab, 0.10),
			withWeight(critErrorRecovery, 0.10),
		},
	}
}

func rubricMultiStepPlan() *Rubric {
	return &Rubric{
		Name:       "Multi-Step Planning Rubric",
		Version:    RubricVersion,
		TaskTypeID: TaskMultiStepPlan,
		Criteria: []Criterion{
			withWeight(critTaskCompletion, 0.20),
			withWeight(critCodeQuality, 0.10),
			withWeight(critTokenEfficiency, 0.10),
			withWeight(critContextUse, 0.15),
			withWeight(critSelfImprovement, 0.10),
			withWeight(critLongSessionStab, 0.25),
			withWeight(critErrorRecovery, 0.10),
		},
	}
}

// withWeight returns a copy of c with the given weight. Used so the
// anchored criterion definitions stay shared and only weights vary.
func withWeight(c Criterion, w float64) Criterion {
	out := c
	out.Weight = w
	return out
}

// CriterionByID looks up a criterion in a rubric by its stable ID.
func (r *Rubric) CriterionByID(id CriterionID) (Criterion, bool) {
	for _, c := range r.Criteria {
		if c.ID == id {
			return c, true
		}
	}
	return Criterion{}, false
}

// Validate checks that weights sum to ~1.0 and all criteria have anchors.
func (r *Rubric) Validate() error {
	if len(r.Criteria) == 0 {
		return fmt.Errorf("rubric %q has no criteria", r.Name)
	}
	var sum float64
	for _, c := range r.Criteria {
		sum += c.Weight
		if c.AnchorLow == "" || c.AnchorMid == "" || c.AnchorHigh == "" {
			return fmt.Errorf("criterion %q in rubric %q is missing anchors", c.ID, r.Name)
		}
	}
	// Allow a small floating-point tolerance.
	if sum < 0.99 || sum > 1.01 {
		return fmt.Errorf("rubric %q weights sum to %.3f, expected 1.0", r.Name, sum)
	}
	return nil
}
