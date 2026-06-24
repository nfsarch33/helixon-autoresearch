package autoresearch

import (
	"testing"
	"time"
)

func TestDefaultWildClawBenchConfig(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	if len(cfg.Categories) != 5 {
		t.Errorf("Categories count = %d, want 5", len(cfg.Categories))
	}
	if len(cfg.DifficultyLevels) != 4 {
		t.Errorf("DifficultyLevels count = %d, want 4", len(cfg.DifficultyLevels))
	}
	if cfg.AgentType != AgentFleetCoder {
		t.Errorf("AgentType = %s, want %s", cfg.AgentType, AgentFleetCoder)
	}
	if cfg.MinPassThreshold != 70.0 {
		t.Errorf("MinPassThreshold = %f, want 70", cfg.MinPassThreshold)
	}
	if len(cfg.ScoringWeights) != 5 {
		t.Errorf("ScoringWeights count = %d, want 5", len(cfg.ScoringWeights))
	}
}

func TestNewWildClawBenchSuite(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}
	if suite.Adapter == nil {
		t.Error("Adapter should not be nil")
	}
	if len(suite.CategoryResults) != 0 {
		t.Error("CategoryResults should be empty before run")
	}
}

func TestNewWildClawBenchSuite_UnknownAgent(t *testing.T) {
	cfg := DefaultWildClawBenchConfig("bogus_agent")
	_, err := NewWildClawBenchSuite(cfg, testLogger())
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}

func TestRunBenchSuite_Success(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}

	experiment := ExperimentResult{
		ID:         "bench-001",
		Name:       "wildclawbench-test",
		Hypothesis: "comprehensive evaluation across categories",
		CodeChanges: []string{
			"package main\nimport \"fmt\"\n\nfunc process() error {\n\tif err != nil { return err }\n\treturn nil\n}",
			"func TestProcess(t *testing.T) {\n\tt.Run(\"basic\", func(t *testing.T) {})\n\tt.Run(\"edge\", func(t *testing.T) {})\n\tt.Run(\"error\", func(t *testing.T) {})\n}",
			"func handleRequest() {\n\tdefer recover()\n\t// validate input\n\tfmt.Errorf(\"bad request\")\n}",
		},
		Metrics: Metrics{
			Before: map[string]float64{},
			After:  map[string]float64{},
		},
		Duration:  10 * time.Second,
		Timestamp: time.Now(),
		Status:    StatusCompleted,
		Phase:     PhaseEvaluation,
	}

	if err := suite.RunBenchSuite(experiment); err != nil {
		t.Fatalf("RunBenchSuite: %v", err)
	}

	if len(suite.CategoryResults) != 5 {
		t.Errorf("CategoryResults count = %d, want 5", len(suite.CategoryResults))
	}

	for _, category := range cfg.Categories {
		diffResults, exists := suite.CategoryResults[category]
		if !exists {
			t.Errorf("category %s not found in results", category)
			continue
		}
		if len(diffResults) != 4 {
			t.Errorf("category %s has %d difficulty levels, want 4", category, len(diffResults))
		}
		for _, difficulty := range cfg.DifficultyLevels {
			catResult, exists := diffResults[difficulty]
			if !exists {
				t.Errorf("category %s / difficulty %s not found", category, difficulty)
				continue
			}
			if catResult.Aggregate.Percentage < 0 || catResult.Aggregate.Percentage > 100 {
				t.Errorf("category %s / difficulty %s: percentage = %f, want [0, 100]",
					category, difficulty, catResult.Aggregate.Percentage)
			}
		}
	}

	if suite.CompositeScore < 0 || suite.CompositeScore > 100 {
		t.Errorf("CompositeScore = %f, want [0, 100]", suite.CompositeScore)
	}
	if suite.OverallVerdict == "" {
		t.Error("OverallVerdict should be non-empty")
	}
	if suite.StartTime.IsZero() {
		t.Error("StartTime should be set")
	}
	if suite.EndTime.IsZero() {
		t.Error("EndTime should be set")
	}
}

func TestRunBenchSuite_AllAgentTypes(t *testing.T) {
	agents := []EvalAgentType{AgentFleetCoder, AgentFleetPRReviewer, AgentFleetDevOps}
	for _, agent := range agents {
		t.Run(string(agent), func(t *testing.T) {
			cfg := DefaultWildClawBenchConfig(agent)
			suite, err := NewWildClawBenchSuite(cfg, testLogger())
			if err != nil {
				t.Fatalf("NewWildClawBenchSuite: %v", err)
			}

			experiment := ExperimentResult{
				ID:          "bench-" + string(agent),
				Name:        string(agent) + "-test",
				Hypothesis:  "test " + string(agent),
				CodeChanges: []string{"package main\nfunc run() { return }"},
				Status:      StatusCompleted,
				Duration:    5 * time.Second,
			}

			if err := suite.RunBenchSuite(experiment); err != nil {
				t.Fatalf("RunBenchSuite: %v", err)
			}
			if suite.CompositeScore < 0 {
				t.Errorf("CompositeScore = %f, want >= 0", suite.CompositeScore)
			}
		})
	}
}

func TestRunBenchSuite_EmptyCodeChanges(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}

	experiment := ExperimentResult{
		ID:          "bench-empty",
		Name:        "empty-changes",
		Hypothesis:  "empty code changes",
		CodeChanges: nil,
		Status:      StatusFailed,
		Duration:    1 * time.Second,
	}

	if err := suite.RunBenchSuite(experiment); err != nil {
		t.Fatalf("RunBenchSuite should not error on empty changes: %v", err)
	}

	if suite.CompositeScore < 0 {
		t.Errorf("CompositeScore = %f, want >= 0", suite.CompositeScore)
	}
}

func TestGetCategorySummary(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}

	experiment := ExperimentResult{
		ID:          "bench-cat-summary",
		Name:        "category-summary-test",
		Hypothesis:  "test category summaries",
		CodeChanges: []string{"package main\nimport \"fmt\"\nfunc test() { fmt.Println(\"hello\") }"},
		Status:      StatusCompleted,
		Duration:    5 * time.Second,
	}

	if err := suite.RunBenchSuite(experiment); err != nil {
		t.Fatalf("RunBenchSuite: %v", err)
	}

	for _, category := range cfg.Categories {
		score, verdict, err := suite.GetCategorySummary(category)
		if err != nil {
			t.Fatalf("GetCategorySummary(%s): %v", category, err)
		}
		if score < 0 || score > 100 {
			t.Errorf("category %s: score = %f, want [0, 100]", category, score)
		}
		if verdict == "" {
			t.Errorf("category %s: verdict should be non-empty", category)
		}
	}
}

func TestGetCategorySummary_NotFound(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}

	_, _, err = suite.GetCategorySummary("nonexistent_category")
	if err == nil {
		t.Fatal("expected error for nonexistent category")
	}
}

func TestGetDifficultySummary(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}

	experiment := ExperimentResult{
		ID:          "bench-diff-summary",
		Name:        "difficulty-summary-test",
		Hypothesis:  "test difficulty summaries",
		CodeChanges: []string{"package main\nfunc run() { return }"},
		Status:      StatusCompleted,
		Duration:    5 * time.Second,
	}

	if err := suite.RunBenchSuite(experiment); err != nil {
		t.Fatalf("RunBenchSuite: %v", err)
	}

	for _, difficulty := range cfg.DifficultyLevels {
		score, verdict, err := suite.GetDifficultySummary(difficulty)
		if err != nil {
			t.Fatalf("GetDifficultySummary(%s): %v", difficulty, err)
		}
		if score < 0 || score > 100 {
			t.Errorf("difficulty %s: score = %f, want [0, 100]", difficulty, score)
		}
		if verdict == "" {
			t.Errorf("difficulty %s: verdict should be non-empty", difficulty)
		}
	}
}

func TestGetDifficultySummary_NotFound(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}

	score, verdict, err := suite.GetDifficultySummary(DifficultyExpert)
	if err != nil {
		t.Fatalf("GetDifficultySummary on empty suite: %v", err)
	}
	if score != 0 {
		t.Errorf("score = %f, want 0 for empty suite", score)
	}
	if verdict != "RED" {
		t.Errorf("verdict = %s, want RED for empty suite", verdict)
	}
}

func TestGenerateReport(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}

	experiment := ExperimentResult{
		ID:          "bench-report",
		Name:        "report-test",
		Hypothesis:  "test report generation",
		CodeChanges: []string{"package main\nfunc run() { return }"},
		Status:      StatusCompleted,
		Duration:    5 * time.Second,
	}

	if err := suite.RunBenchSuite(experiment); err != nil {
		t.Fatalf("RunBenchSuite: %v", err)
	}

	report := suite.GenerateReport()

	if _, ok := report["composite_score"]; !ok {
		t.Error("report missing composite_score")
	}
	if _, ok := report["overall_verdict"]; !ok {
		t.Error("report missing overall_verdict")
	}
	if _, ok := report["category_breakdown"]; !ok {
		t.Error("report missing category_breakdown")
	}
	if _, ok := report["start_time"]; !ok {
		t.Error("report missing start_time")
	}
	if _, ok := report["end_time"]; !ok {
		t.Error("report missing end_time")
	}
	if _, ok := report["duration"]; !ok {
		t.Error("report missing duration")
	}

	breakdown, ok := report["category_breakdown"].(map[string]interface{})
	if !ok {
		t.Fatal("category_breakdown should be map[string]interface{}")
	}
	if len(breakdown) != 5 {
		t.Errorf("category_breakdown count = %d, want 5", len(breakdown))
	}
}

func TestGetDifficultyMultiplier(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}

	tests := []struct {
		difficulty DifficultyLevel
		want       float64
	}{
		{DifficultyEasy, 1.0},
		{DifficultyMedium, 1.2},
		{DifficultyHard, 1.5},
		{DifficultyExpert, 2.0},
	}

	for _, tt := range tests {
		got := suite.getDifficultyMultiplier(tt.difficulty)
		if got != tt.want {
			t.Errorf("getDifficultyMultiplier(%s) = %f, want %f", tt.difficulty, got, tt.want)
		}
	}
}

func TestWildClawBenchConfig_AllCategoriesPresent(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)

	expected := []BenchCategory{
		CategoryCodeGeneration,
		CategoryCodeReview,
		CategoryDebugging,
		CategoryArchitecture,
		CategoryDevOps,
	}

	for _, cat := range expected {
		found := false
		for _, c := range cfg.Categories {
			if c == cat {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("category %s not found in default config", cat)
		}
	}
}

func TestWildClawBenchConfig_ProgressiveDifficulty(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)

	expected := []DifficultyLevel{
		DifficultyEasy,
		DifficultyMedium,
		DifficultyHard,
		DifficultyExpert,
	}

	if len(cfg.DifficultyLevels) != len(expected) {
		t.Fatalf("DifficultyLevels count = %d, want %d", len(cfg.DifficultyLevels), len(expected))
	}

	for i, d := range expected {
		if cfg.DifficultyLevels[i] != d {
			t.Errorf("DifficultyLevels[%d] = %s, want %s", i, cfg.DifficultyLevels[i], d)
		}
	}
}

func TestAdaptExperimentForCategory(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}

	original := ExperimentResult{
		ID:          "orig-001",
		Name:        "original",
		Hypothesis:  "test hypothesis",
		CodeChanges: []string{"code line 1", "code line 2"},
		Status:      StatusCompleted,
	}

	adapted := suite.adaptExperimentForCategory(original, CategoryCodeGeneration, DifficultyHard)

	if adapted.ID != original.ID {
		t.Errorf("ID should be preserved: got %s, want %s", adapted.ID, original.ID)
	}
	if adapted.Name != "original_code_generation_hard" {
		t.Errorf("Name = %q, want %q", adapted.Name, "original_code_generation_hard")
	}
	if len(adapted.CodeChanges) != len(original.CodeChanges) {
		t.Errorf("CodeChanges count = %d, want %d", len(adapted.CodeChanges), len(original.CodeChanges))
	}
}

func TestCalculateCompositeScore_ProgressiveWeighting(t *testing.T) {
	cfg := DefaultWildClawBenchConfig(AgentFleetCoder)
	suite, err := NewWildClawBenchSuite(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewWildClawBenchSuite: %v", err)
	}

	experiment := ExperimentResult{
		ID:          "bench-weighting",
		Name:        "weighting-test",
		Hypothesis:  "verify progressive difficulty weighting",
		CodeChanges: []string{"package main\nimport \"fmt\"\nfunc good() { fmt.Println(\"ok\") }"},
		Status:      StatusCompleted,
		Duration:    5 * time.Second,
	}

	if err := suite.RunBenchSuite(experiment); err != nil {
		t.Fatalf("RunBenchSuite: %v", err)
	}

	if suite.CompositeScore < 0 || suite.CompositeScore > 100 {
		t.Errorf("CompositeScore = %f, want [0, 100]", suite.CompositeScore)
	}
}
