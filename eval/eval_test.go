package eval

import (
	"context"
	"strings"
	"testing"
)

// fakeBackend is a test LLMBackend that returns canned responses without
// hitting any real API. It implements the Complete method via a closure.
type fakeBackend struct {
	name      string
	model     string
	responder func(prompt string) (string, TokenUsage, error)
}

func (f fakeBackend) Complete(ctx context.Context, prompt string, maxTokens int) (string, TokenUsage, error) {
	return f.responder(prompt)
}

func (f fakeBackend) asLLMBackend() LLMBackend {
	return LLMBackend{Name: f.name, Model: f.model, APIKey: "fake", APIURL: "http://fake.invalid"}
}

// TestDefaultRubricsValidate checks every default rubric sums to 1.0 and
// has all three anchors per criterion.
func TestDefaultRubricsValidate(t *testing.T) {
	rubrics := DefaultRubrics()
	if len(rubrics) != 7 {
		t.Fatalf("expected 7 default rubrics, got %d", len(rubrics))
	}
	for taskType, r := range rubrics {
		if err := r.Validate(); err != nil {
			t.Errorf("rubric for %s failed validation: %v", taskType, err)
		}
	}
}

// TestDefaultTaskSuiteSevenTypes confirms the suite covers all 7 types.
func TestDefaultTaskSuiteSevenTypes(t *testing.T) {
	tasks := DefaultTaskSuite()
	if len(tasks) != 7 {
		t.Fatalf("expected 7 tasks, got %d", len(tasks))
	}
	seen := make(map[TaskTypeID]bool, 7)
	for _, task := range tasks {
		seen[task.Type] = true
	}
	for _, tt := range AllTaskTypes() {
		if !seen[tt] {
			t.Errorf("task suite missing task type %q", tt)
		}
	}
}

// TestParseJudgeResponse validates the G-Eval JSON parser handles clean
// JSON, code-fenced JSON, and prose-wrapped JSON.
func TestParseJudgeResponse(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  map[CriterionID]int
	}{
		{
			name:  "clean_json",
			input: `{"task_completion":{"value":5,"rationale":"full"},"code_quality":{"value":4,"rationale":"minor"}}`,
			want:  map[CriterionID]int{CritTaskCompletion: 5, CritCodeQuality: 4},
		},
		{
			name:  "code_fenced",
			input: "```json\n" + `{"task_completion":{"value":3,"rationale":"mid"}}` + "\n```",
			want:  map[CriterionID]int{CritTaskCompletion: 3},
		},
		{
			name:  "prose_wrapped",
			input: `Here is my evaluation: {"task_completion":{"value":1,"rationale":"bad"}} done.`,
			want:  map[CriterionID]int{CritTaskCompletion: 1},
		},
		{
			name:  "out_of_range_clamped",
			input: `{"task_completion":{"value":99,"rationale":"x"},"code_quality":{"value":-5,"rationale":"y"}}`,
			want:  map[CriterionID]int{CritTaskCompletion: ScaleMax, CritCodeQuality: 1},
		},
		{
			// MiniMax-M3 emits a <think>... reasoning block before the JSON.
			// The parser must strip the think tag and recover the JSON that follows.
			name: "think_prefix_minimax_m3",
			input: `<think>
Let me analyze the agent's output carefully.

The agent was asked to write a Go package. Let me check criteria...
Some prose here with {braces} and other characters.
</think>

Here is my final evaluation:

{
  "task_completion": {"value": 4, "rationale": "good"},
  "code_quality": {"value": 3, "rationale": "minor issues"}
}`,
			want: map[CriterionID]int{CritTaskCompletion: 4, CritCodeQuality: 3},
		},
		{
			// Unclosed think tag (model ran out of tokens before closing).
			// Fallback must still try to find the JSON in what remains.
			name:  "think_unclosed",
			input: `<think>reasoning that never closes {"task_completion":{"value":2,"rationale":"ok"}}`,
			want:  map[CriterionID]int{CritTaskCompletion: 2},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scores, err := parseJudgeResponse(tc.input)
			if err != nil {
				t.Fatalf("parseJudgeResponse(%q) error: %v", tc.input, err)
			}
			for cid, wantVal := range tc.want {
				got, ok := scores[cid]
				if !ok {
					t.Errorf("missing criterion %q in %s", cid, tc.name)
					continue
				}
				if got.Value != wantVal {
					t.Errorf("criterion %q = %d, want %d", cid, got.Value, wantVal)
				}
			}
		})
	}
}

// TestWeightedScore checks the weighted aggregation produces expected
// 0-100 values from criterion scores.
func TestWeightedScore(t *testing.T) {
	rubric := rubricCodeGeneration()
	// All max scores => 100.
	maxScores := make(map[CriterionID]Score)
	for _, c := range rubric.Criteria {
		maxScores[c.ID] = Score{Value: ScaleMax}
	}
	if got := weightedScore(rubric, maxScores); got != 100 {
		t.Errorf("all-max weighted score = %.2f, want 100", got)
	}
	// All min scores => 20 (1/5 = 0.2, *100 = 20).
	minScores := make(map[CriterionID]Score)
	for _, c := range rubric.Criteria {
		minScores[c.ID] = Score{Value: 1}
	}
	if got := weightedScore(rubric, minScores); got < 19.9 || got > 20.1 {
		t.Errorf("all-min weighted score = %.2f, want ~20", got)
	}
}

// TestBuildAgentPrompt confirms context files are embedded in the prompt.
func TestBuildAgentPrompt(t *testing.T) {
	task := Task{
		ID:     "t1",
		Type:   TaskCodeGeneration,
		Prompt: "Do the thing.",
		ContextFiles: []ContextFile{
			{Path: "a.go", Language: "go", Content: "package a"},
		},
	}
	prompt := buildAgentPrompt(task)
	if !strings.Contains(prompt, "Do the thing.") {
		t.Error("prompt missing task brief")
	}
	if !strings.Contains(prompt, "a.go") {
		t.Error("prompt missing context file path")
	}
	if !strings.Contains(prompt, "package a") {
		t.Error("prompt missing context file content")
	}
}

// TestBuildMatrix_SingleBackendPerTask guards against a regression where
// mutations to the per-task BackendScores map were dropped because the
// task value was being copied by value rather than written back. This
// reproduces the panic that occurred during the first live eval against
// MiniMax-M3 with only one backend selected.
func TestBuildMatrix_SingleBackendPerTask(t *testing.T) {
	backends := []LLMBackend{{Name: "minimax-m3", Model: "MiniMax-M3"}}
	tasks := []Task{
		{ID: "t1", Type: TaskCodeGeneration},
		{ID: "t2", Type: TaskCodeReview},
	}
	results := []TaskResult{
		{TaskID: "t1", Backend: "minimax-m3", WeightedScore: 80, Verdict: "GREEN"},
		{TaskID: "t2", Backend: "minimax-m3", WeightedScore: 40, Verdict: "RED"},
	}
	matrix := buildMatrix(results, backends, tasks)

	// Without the fix this triggers `panic: assignment to entry in nil map`.
	if got := matrix.ByTask["t1"].BackendScores["minimax-m3"]; got != 80 {
		t.Errorf("t1.minimax-m3 score = %.0f, want 80", got)
	}
	if got := matrix.ByTask["t2"].BackendScores["minimax-m3"]; got != 40 {
		t.Errorf("t2.minimax-m3 score = %.0f, want 40", got)
	}
	if matrix.ByTask["t1"].BestBackend != "minimax-m3" {
		t.Errorf("t1 best backend = %s, want minimax-m3", matrix.ByTask["t1"].BestBackend)
	}
}

// TestBuildMatrix verifies the comparative matrix aggregates correctly.
func TestBuildMatrix(t *testing.T) {
	backends := []LLMBackend{
		{Name: "b1", Model: "m1"},
		{Name: "b2", Model: "m2"},
	}
	tasks := []Task{{ID: "t1", Type: TaskCodeGeneration}, {ID: "t2", Type: TaskCodeReview}}
	results := []TaskResult{
		{TaskID: "t1", Backend: "b1", WeightedScore: 90, Verdict: "GREEN", CriterionScores: map[CriterionID]Score{CritTaskCompletion: {Value: 5}}},
		{TaskID: "t1", Backend: "b2", WeightedScore: 50, Verdict: "YELLOW", CriterionScores: map[CriterionID]Score{CritTaskCompletion: {Value: 3}}},
		{TaskID: "t2", Backend: "b1", WeightedScore: 30, Verdict: "RED", CriterionScores: map[CriterionID]Score{CritTaskCompletion: {Value: 1}}},
		{TaskID: "t2", Backend: "b2", WeightedScore: 70, Verdict: "YELLOW", CriterionScores: map[CriterionID]Score{CritTaskCompletion: {Value: 4}}},
	}
	matrix := buildMatrix(results, backends, tasks)

	b1 := matrix.ByBackend["b1"]
	if got := b1.AvgWeighted; got < 59.9 || got > 60.1 {
		t.Errorf("b1 avg = %.2f, want 60", got)
	}
	if b1.PassRate != 50 { // 1 of 2 GREEN
		t.Errorf("b1 pass rate = %.0f, want 50", b1.PassRate)
	}
	ts := matrix.ByTask["t1"]
	if ts.BestBackend != "b1" {
		t.Errorf("t1 best backend = %s, want b1", ts.BestBackend)
	}
	if ts.Spread < 39.9 || ts.Spread > 40.1 {
		t.Errorf("t1 spread = %.2f, want 40", ts.Spread)
	}
}

// TestVerdictBands checks the verdict thresholds.
func TestVerdictBands(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{80, "GREEN"}, {95, "GREEN"},
		{79.9, "YELLOW"}, {50, "YELLOW"},
		{49.9, "RED"}, {0, "RED"},
	}
	for _, tc := range cases {
		if got := verdictFromScore(tc.score); got != tc.want {
			t.Errorf("verdictFromScore(%.1f) = %s, want %s", tc.score, got, tc.want)
		}
	}
}

// TestCriterionByID exercises the lookup helper across the seven
// canonical criteria and confirms the missing-key path is correct.
func TestCriterionByID(t *testing.T) {
	rubric := rubricCodeGeneration()
	for _, want := range rubric.Criteria {
		got, ok := rubric.CriterionByID(want.ID)
		if !ok {
			t.Errorf("expected to find criterion %q", want.ID)
			continue
		}
		if got.ID != want.ID {
			t.Errorf("CriterionByID(%q) = %q, want %q", want.ID, got.ID, want.ID)
		}
	}
	// Missing key returns false.
	if _, ok := rubric.CriterionByID("nope"); ok {
		t.Error("expected CriterionByID(\"nope\") to return ok=false")
	}
}

// TestValidate_RejectsBadWeights covers the negative validation paths
// (empty criteria, missing anchors, weight sum out of tolerance).
func TestValidate_RejectsBadWeights(t *testing.T) {
	cases := []struct {
		name    string
		rubric  *Rubric
		wantErr string
	}{
		{
			name: "no_criteria",
			rubric: &Rubric{
				Name: "Empty", Version: "test", TaskTypeID: "t",
				Criteria: nil,
			},
			wantErr: "no criteria",
		},
		{
			name: "missing_anchor",
			rubric: &Rubric{
				Name: "MissingAnchor", Version: "test", TaskTypeID: "t",
				Criteria: []Criterion{
					{ID: "x", Name: "x", Weight: 1.0, AnchorLow: "", AnchorMid: "m", AnchorHigh: "h"},
				},
			},
			wantErr: "missing anchors",
		},
		{
			name: "weights_too_low",
			rubric: &Rubric{
				Name: "LowSum", Version: "test", TaskTypeID: "t",
				Criteria: []Criterion{
					{ID: "x", Name: "x", Weight: 0.5, AnchorLow: "l", AnchorMid: "m", AnchorHigh: "h"},
				},
			},
			wantErr: "weights sum",
		},
		{
			name: "weights_too_high",
			rubric: &Rubric{
				Name: "HighSum", Version: "test", TaskTypeID: "t",
				Criteria: []Criterion{
					{ID: "x", Name: "x", Weight: 1.5, AnchorLow: "l", AnchorMid: "m", AnchorHigh: "h"},
				},
			},
			wantErr: "weights sum",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rubric.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestAllDefaultRubrics_HaveSevenCriteria confirms every task-type
// rubric exposes exactly the seven canonical criteria (no task type
// drops or duplicates metrics).
func TestAllDefaultRubrics_HaveSevenCriteria(t *testing.T) {
	for taskType, r := range DefaultRubrics() {
		if len(r.Criteria) != 7 {
			t.Errorf("rubric for %s has %d criteria, want 7", taskType, len(r.Criteria))
		}
		seen := make(map[CriterionID]bool, 7)
		for _, c := range r.Criteria {
			if seen[c.ID] {
				t.Errorf("rubric for %s has duplicate criterion %q", taskType, c.ID)
			}
			seen[c.ID] = true
		}
		for _, want := range []CriterionID{
			CritTaskCompletion, CritCodeQuality, CritTokenEfficiency,
			CritContextUse, CritSelfImprovement, CritLongSessionStab,
			CritErrorRecovery,
		} {
			if !seen[want] {
				t.Errorf("rubric for %s missing criterion %q", taskType, want)
			}
		}
	}
}

// TestRubricFor_UnknownTaskTypeErrors exercises the error path of
// rubricFor (called by callers that do not consult DefaultRubrics
// directly).
func TestRubricFor_UnknownTaskTypeErrors(t *testing.T) {
	if _, err := rubricFor("nonexistent"); err == nil {
		t.Error("expected error for unknown task type")
	}
}

// TestRubricVersionStable is a guard against accidental version bumps
// during refactors: the G-Eval rubric version is the contract for
// comparability across reports.
func TestRubricVersionStable(t *testing.T) {
	if RubricVersion != "2.0.0" {
		t.Errorf("RubricVersion = %q, want %q (bump intentionally in a release commit)", RubricVersion, "2.0.0")
	}
}

// TestScaleMaxIsFive locks the G-Eval 1-5 scale constant. Changing this
// breaks every prior report; the constant must change in lockstep with
// a rubric version bump.
func TestScaleMaxIsFive(t *testing.T) {
	if ScaleMax != 5 {
		t.Errorf("ScaleMax = %d, want 5", ScaleMax)
	}
}

// TestWithWeight confirms the helper produces an independent copy and
// does not mutate the source criterion.
func TestWithWeight(t *testing.T) {
	src := Criterion{ID: "x", Name: "x", Weight: 0.5}
	out := withWeight(src, 0.9)
	if out.Weight != 0.9 {
		t.Errorf("withWeight result weight = %.2f, want 0.9", out.Weight)
	}
	if src.Weight != 0.5 {
		t.Errorf("withWeight mutated source: weight = %.2f, want 0.5", src.Weight)
	}
}
