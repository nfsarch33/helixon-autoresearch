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
