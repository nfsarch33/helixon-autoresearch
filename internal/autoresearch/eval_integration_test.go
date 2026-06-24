package autoresearch

import (
	"testing"
	"time"
)

func TestNewEvalHarnessAdapter_AllAgentTypes(t *testing.T) {
	agents := []EvalAgentType{AgentFleetCoder, AgentFleetPRReviewer, AgentFleetDevOps}
	for _, agent := range agents {
		adapter, err := NewEvalHarnessAdapter(agent, nil)
		if err != nil {
			t.Fatalf("NewEvalHarnessAdapter(%s): %v", agent, err)
		}
		if adapter.Rubric() == nil {
			t.Errorf("Rubric() returned nil for %s", agent)
		}
		if adapter.Rubric().AgentType != agent {
			t.Errorf("Rubric().AgentType = %s, want %s", adapter.Rubric().AgentType, agent)
		}
	}
}

func TestNewEvalHarnessAdapter_UnknownAgent(t *testing.T) {
	_, err := NewEvalHarnessAdapter("unknown_agent", nil)
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}

func TestEvaluateExperiment_FleetCoder(t *testing.T) {
	adapter, err := NewEvalHarnessAdapter(AgentFleetCoder, testLogger())
	if err != nil {
		t.Fatalf("NewEvalHarnessAdapter: %v", err)
	}

	result := ExperimentResult{
		ID:         "exp-001",
		Name:       "code-gen-test",
		Hypothesis: "better prompting improves code quality",
		CodeChanges: []string{
			"package main\n\nimport \"fmt\"\n\nfunc add(a, b int) int {\n\treturn a + b\n}",
			"func TestAdd(t *testing.T) {\n\tt.Run(\"positive\", func(t *testing.T) {\n\t\tif got := add(1, 2); got != 3 {\n\t\t\tt.Errorf(\"got %d\", got)\n\t\t}\n\t})\n}",
		},
		Metrics: Metrics{
			Before: map[string]float64{},
			After:  map[string]float64{},
		},
		Duration:  5 * time.Second,
		Timestamp: time.Now(),
		Status:    StatusCompleted,
		Phase:     PhaseEvaluation,
	}

	tr, err := adapter.EvaluateExperiment(result)
	if err != nil {
		t.Fatalf("EvaluateExperiment: %v", err)
	}

	if tr.TaskID != "exp-001" {
		t.Errorf("TaskID = %q, want %q", tr.TaskID, "exp-001")
	}
	if tr.AgentType != AgentFleetCoder {
		t.Errorf("AgentType = %s, want %s", tr.AgentType, AgentFleetCoder)
	}
	if tr.TotalScore <= 0 {
		t.Error("TotalScore should be > 0")
	}
	if tr.MaxScore != 50 {
		t.Errorf("MaxScore = %d, want 50", tr.MaxScore)
	}
	if tr.Percentage <= 0 || tr.Percentage > 100 {
		t.Errorf("Percentage = %f, want (0, 100]", tr.Percentage)
	}
	if tr.Verdict == "" {
		t.Error("Verdict should be non-empty")
	}
	if len(tr.CriterionScores) != 5 {
		t.Errorf("CriterionScores count = %d, want 5", len(tr.CriterionScores))
	}
}

func TestEvaluateExperiment_FleetPRReviewer(t *testing.T) {
	adapter, err := NewEvalHarnessAdapter(AgentFleetPRReviewer, testLogger())
	if err != nil {
		t.Fatalf("NewEvalHarnessAdapter: %v", err)
	}

	result := ExperimentResult{
		ID:         "exp-002",
		Name:       "review-test",
		Hypothesis: "structured reviews catch more bugs",
		CodeChanges: []string{
			"## Code Review\n\nFound issue on line 42: potential null pointer.\n" +
				"Suggest adding a nil check. Consider using early return pattern.\n" +
				"Also found a security vulnerability with authentication bypass.\n" +
				"Performance: there is a bottleneck in the loop.\n```go\nif err != nil { return err }\n```",
		},
		Status:   StatusCompleted,
		Duration: 3 * time.Second,
	}

	tr, err := adapter.EvaluateExperiment(result)
	if err != nil {
		t.Fatalf("EvaluateExperiment: %v", err)
	}

	if tr.AgentType != AgentFleetPRReviewer {
		t.Errorf("AgentType = %s, want %s", tr.AgentType, AgentFleetPRReviewer)
	}
	if tr.TotalScore <= 0 {
		t.Error("TotalScore should be > 0")
	}
	if len(tr.CriterionScores) != 5 {
		t.Errorf("CriterionScores count = %d, want 5", len(tr.CriterionScores))
	}
}

func TestEvaluateExperiment_FleetDevOps(t *testing.T) {
	adapter, err := NewEvalHarnessAdapter(AgentFleetDevOps, testLogger())
	if err != nil {
		t.Fatalf("NewEvalHarnessAdapter: %v", err)
	}

	result := ExperimentResult{
		ID:         "exp-003",
		Name:       "deploy-test",
		Hypothesis: "automated deployments reduce incident time",
		CodeChanges: []string{
			"## Deployment Runbook\n\n### Step 1: Run the script\n" +
				"The pipeline deploys the service successfully. All services are running.\n" +
				"Prometheus metrics and Grafana dashboard configured.\n" +
				"Incident response: diagnose root cause and resolve the issue.\n" +
				"Automation: ci/cd pipeline workflow automated.\n" +
				"Prevent future incidents with improvement to monitoring alerts.",
		},
		Status:   StatusCompleted,
		Duration: 10 * time.Second,
	}

	tr, err := adapter.EvaluateExperiment(result)
	if err != nil {
		t.Fatalf("EvaluateExperiment: %v", err)
	}

	if tr.AgentType != AgentFleetDevOps {
		t.Errorf("AgentType = %s, want %s", tr.AgentType, AgentFleetDevOps)
	}
	if tr.TotalScore <= 0 {
		t.Error("TotalScore should be > 0")
	}
}

func TestEvaluateExperiment_EmptyID(t *testing.T) {
	adapter, err := NewEvalHarnessAdapter(AgentFleetCoder, testLogger())
	if err != nil {
		t.Fatalf("NewEvalHarnessAdapter: %v", err)
	}

	_, err = adapter.EvaluateExperiment(ExperimentResult{})
	if err == nil {
		t.Fatal("expected error for empty experiment ID")
	}
}

func TestEvaluateExperiment_WithError(t *testing.T) {
	adapter, err := NewEvalHarnessAdapter(AgentFleetCoder, testLogger())
	if err != nil {
		t.Fatalf("NewEvalHarnessAdapter: %v", err)
	}

	result := ExperimentResult{
		ID:         "exp-err",
		Name:       "error-experiment",
		Hypothesis: "this will fail",
		CodeChanges: []string{
			"package main\nimport \"fmt\"\nfunc broken() { error here }",
		},
		Status: StatusFailed,
		Error:  "compilation failed",
	}

	tr, err := adapter.EvaluateExperiment(result)
	if err != nil {
		t.Fatalf("EvaluateExperiment: %v", err)
	}

	if tr.Error != "compilation failed" {
		t.Errorf("Error = %q, want %q", tr.Error, "compilation failed")
	}
}

func TestEvaluateExperiment_EmptyOutput(t *testing.T) {
	adapter, err := NewEvalHarnessAdapter(AgentFleetCoder, testLogger())
	if err != nil {
		t.Fatalf("NewEvalHarnessAdapter: %v", err)
	}

	result := ExperimentResult{
		ID:          "exp-empty",
		Name:        "empty-output",
		Hypothesis:  "nothing here",
		CodeChanges: nil,
		Status:      StatusFailed,
	}

	tr, err := adapter.EvaluateExperiment(result)
	if err != nil {
		t.Fatalf("EvaluateExperiment: %v", err)
	}

	if tr.TotalScore < 0 {
		t.Errorf("TotalScore = %d, want >= 0", tr.TotalScore)
	}
}

func TestCalculateAggregate_Empty(t *testing.T) {
	agg := CalculateAggregate(nil)
	if agg.Verdict != "RED" {
		t.Errorf("Verdict = %q, want RED", agg.Verdict)
	}
	if agg.TotalScore != 0 {
		t.Errorf("TotalScore = %d, want 0", agg.TotalScore)
	}
}

func TestCalculateAggregate_MultipleResults(t *testing.T) {
	results := []TaskResult{
		{
			TotalScore:      40,
			MaxScore:        50,
			Percentage:      80,
			Verdict:         "GREEN",
			CriterionScores: map[string]int{"code_correctness": 8, "test_coverage": 8, "code_quality": 8, "task_completion": 8, "error_handling": 8},
		},
		{
			TotalScore:      30,
			MaxScore:        50,
			Percentage:      60,
			Verdict:         "YELLOW",
			CriterionScores: map[string]int{"code_correctness": 6, "test_coverage": 6, "code_quality": 6, "task_completion": 6, "error_handling": 6},
		},
	}

	agg := CalculateAggregate(results)
	if agg.TotalScore != 70 {
		t.Errorf("TotalScore = %d, want 70", agg.TotalScore)
	}
	if agg.MaxScore != 100 {
		t.Errorf("MaxScore = %d, want 100", agg.MaxScore)
	}
	if agg.Percentage != 70 {
		t.Errorf("Percentage = %f, want 70", agg.Percentage)
	}
	if agg.PassRate != 50 {
		t.Errorf("PassRate = %f, want 50", agg.PassRate)
	}
	if agg.Verdict != "YELLOW" {
		t.Errorf("Verdict = %q, want YELLOW", agg.Verdict)
	}
	if len(agg.CriterionAverages) != 5 {
		t.Errorf("CriterionAverages count = %d, want 5", len(agg.CriterionAverages))
	}
	if agg.CriterionAverages["code_correctness"] != 7 {
		t.Errorf("CriterionAverages[code_correctness] = %f, want 7", agg.CriterionAverages["code_correctness"])
	}
}

func TestCalculateAggregate_AllPassing(t *testing.T) {
	results := []TaskResult{
		{TotalScore: 45, MaxScore: 50, Percentage: 90, CriterionScores: map[string]int{"a": 9}},
		{TotalScore: 40, MaxScore: 50, Percentage: 80, CriterionScores: map[string]int{"a": 8}},
	}

	agg := CalculateAggregate(results)
	if agg.PassRate != 100 {
		t.Errorf("PassRate = %f, want 100", agg.PassRate)
	}
}

func TestScoreExperimentMetrics(t *testing.T) {
	tr := &TaskResult{
		CriterionScores: map[string]int{
			"code_correctness": 8,
			"test_coverage":    7,
		},
		Percentage: 75,
	}

	metrics := ScoreExperimentMetrics(tr)
	if metrics.After["code_correctness"] != 8 {
		t.Errorf("After[code_correctness] = %f, want 8", metrics.After["code_correctness"])
	}
	if metrics.After["test_coverage"] != 7 {
		t.Errorf("After[test_coverage] = %f, want 7", metrics.After["test_coverage"])
	}
	if metrics.After["overall_percentage"] != 75 {
		t.Errorf("After[overall_percentage] = %f, want 75", metrics.After["overall_percentage"])
	}
}

func TestCalculateVerdict(t *testing.T) {
	tests := []struct {
		pct  float64
		want string
	}{
		{90, "GREEN"},
		{80, "GREEN"},
		{79.9, "YELLOW"},
		{50, "YELLOW"},
		{49.9, "RED"},
		{0, "RED"},
	}
	for _, tt := range tests {
		got := calculateVerdict(tt.pct)
		if got != tt.want {
			t.Errorf("calculateVerdict(%f) = %q, want %q", tt.pct, got, tt.want)
		}
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("hello world", []string{"world", "foo"}) {
		t.Error("should find 'world'")
	}
	if containsAny("hello world", []string{"foo", "bar"}) {
		t.Error("should not find any")
	}
	if !containsAny("Hello World", []string{"hello"}) {
		t.Error("should be case insensitive")
	}
}

func TestClamp(t *testing.T) {
	if clamp(5, 0, 10) != 5 {
		t.Error("clamp(5,0,10) should be 5")
	}
	if clamp(-1, 0, 10) != 0 {
		t.Error("clamp(-1,0,10) should be 0")
	}
	if clamp(15, 0, 10) != 10 {
		t.Error("clamp(15,0,10) should be 10")
	}
}

func TestScoreCriterion_SpecificCriteria(t *testing.T) {
	result := ExperimentResult{Status: StatusCompleted}

	t.Run("code_correctness_good", func(t *testing.T) {
		score := scoreCriterion(RubricCriterion{ID: "code_correctness"}, "package main\nimport \"fmt\"\nfunc add() { return 1 }", result)
		if score < 5 {
			t.Errorf("score = %d, expected >= 5 for valid code", score)
		}
	})

	t.Run("code_correctness_bad", func(t *testing.T) {
		score := scoreCriterion(RubricCriterion{ID: "code_correctness"}, "this code has failed with error undefined", result)
		if score > 5 {
			t.Errorf("score = %d, expected <= 5 for broken code", score)
		}
	})

	t.Run("error_handling_good", func(t *testing.T) {
		score := scoreCriterion(RubricCriterion{ID: "error_handling"}, "if err != nil { return err }\nfmt.Errorf(\"bad\")\ndefer recover()", result)
		if score < 7 {
			t.Errorf("score = %d, expected >= 7 for good error handling", score)
		}
	})

	t.Run("test_coverage_good", func(t *testing.T) {
		output := "func TestFoo(t *testing.T) {\n t.Run(\"a\", ...)\n t.Run(\"b\", ...)\n t.Run(\"c\", ...)\n}"
		score := scoreCriterion(RubricCriterion{ID: "test_coverage"}, output, result)
		if score < 8 {
			t.Errorf("score = %d, expected >= 8 for comprehensive tests", score)
		}
	})
}

func TestRubricForAgent_AllTypes(t *testing.T) {
	tests := []struct {
		agent       EvalAgentType
		criteriaLen int
	}{
		{AgentFleetCoder, 5},
		{AgentFleetPRReviewer, 5},
		{AgentFleetDevOps, 5},
	}
	for _, tt := range tests {
		rubric, err := rubricForAgent(tt.agent)
		if err != nil {
			t.Fatalf("rubricForAgent(%s): %v", tt.agent, err)
		}
		if len(rubric.Criteria) != tt.criteriaLen {
			t.Errorf("rubric for %s has %d criteria, want %d", tt.agent, len(rubric.Criteria), tt.criteriaLen)
		}
	}
}

func TestRubricForAgent_Unknown(t *testing.T) {
	_, err := rubricForAgent("bogus")
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}
