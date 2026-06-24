package autoresearch

import (
	"fmt"
	"log/slog"
	"time"
)

// DifficultyLevel represents progressive difficulty in WildClawBench evaluation.
type DifficultyLevel string

const (
	DifficultyEasy   DifficultyLevel = "easy"
	DifficultyMedium DifficultyLevel = "medium"
	DifficultyHard   DifficultyLevel = "hard"
	DifficultyExpert DifficultyLevel = "expert"
)

// BenchCategory represents an evaluation category in WildClawBench.
type BenchCategory string

const (
	CategoryCodeGeneration BenchCategory = "code_generation"
	CategoryCodeReview     BenchCategory = "code_review"
	CategoryDebugging      BenchCategory = "debugging"
	CategoryArchitecture   BenchCategory = "architecture"
	CategoryDevOps         BenchCategory = "devops"
)

// WildClawBenchConfig defines the configuration for a WildClawBench evaluation suite.
type WildClawBenchConfig struct {
	Categories       []BenchCategory
	DifficultyLevels []DifficultyLevel
	ScoringWeights   map[BenchCategory]float64
	AgentType        EvalAgentType
	MinPassThreshold float64
}

// DefaultWildClawBenchConfig returns a sensible default configuration.
func DefaultWildClawBenchConfig(agentType EvalAgentType) WildClawBenchConfig {
	return WildClawBenchConfig{
		Categories: []BenchCategory{
			CategoryCodeGeneration,
			CategoryCodeReview,
			CategoryDebugging,
			CategoryArchitecture,
			CategoryDevOps,
		},
		DifficultyLevels: []DifficultyLevel{
			DifficultyEasy,
			DifficultyMedium,
			DifficultyHard,
			DifficultyExpert,
		},
		ScoringWeights: map[BenchCategory]float64{
			CategoryCodeGeneration: 1.0,
			CategoryCodeReview:     1.0,
			CategoryDebugging:      1.0,
			CategoryArchitecture:   1.0,
			CategoryDevOps:         1.0,
		},
		AgentType:        agentType,
		MinPassThreshold: 70.0,
	}
}

// CategoryResult holds the evaluation results for a single category.
type CategoryResult struct {
	Category      BenchCategory
	Difficulty    DifficultyLevel
	TaskResults   []TaskResult
	Aggregate     AggregateStats
	WeightedScore float64
	Passed        bool
}

// WildClawBenchSuite represents a complete benchmark evaluation suite.
type WildClawBenchSuite struct {
	Config          WildClawBenchConfig
	Adapter         *EvalHarnessAdapter
	CategoryResults map[BenchCategory]map[DifficultyLevel]*CategoryResult
	CompositeScore  float64
	OverallVerdict  string
	StartTime       time.Time
	EndTime         time.Time
	logger          *slog.Logger
}

// NewWildClawBenchSuite creates a new benchmark suite for the given configuration.
func NewWildClawBenchSuite(config WildClawBenchConfig, logger *slog.Logger) (*WildClawBenchSuite, error) {
	if logger == nil {
		logger = slog.Default()
	}

	adapter, err := NewEvalHarnessAdapter(config.AgentType, logger)
	if err != nil {
		return nil, fmt.Errorf("create adapter for suite: %w", err)
	}

	return &WildClawBenchSuite{
		Config:          config,
		Adapter:         adapter,
		CategoryResults: make(map[BenchCategory]map[DifficultyLevel]*CategoryResult),
		logger:          logger,
	}, nil
}

// RunBenchSuite evaluates an experiment across all categories and difficulty levels.
func (s *WildClawBenchSuite) RunBenchSuite(experiment ExperimentResult) error {
	s.StartTime = time.Now()
	s.logger.Info("starting WildClawBench suite",
		"experiment_id", experiment.ID,
		"agent_type", string(s.Config.AgentType),
		"categories", len(s.Config.Categories),
		"difficulty_levels", len(s.Config.DifficultyLevels),
	)

	for _, category := range s.Config.Categories {
		s.CategoryResults[category] = make(map[DifficultyLevel]*CategoryResult)

		for _, difficulty := range s.Config.DifficultyLevels {
			s.logger.Info("evaluating category",
				"category", string(category),
				"difficulty", string(difficulty),
			)

			catResult, err := s.evaluateCategory(experiment, category, difficulty)
			if err != nil {
				s.logger.Error("category evaluation failed",
					"category", string(category),
					"difficulty", string(difficulty),
					"err", err,
				)
				return fmt.Errorf("evaluate %s/%s: %w", category, difficulty, err)
			}

			s.CategoryResults[category][difficulty] = catResult
		}
	}

	s.calculateCompositeScore()
	s.EndTime = time.Now()

	s.logger.Info("WildClawBench suite completed",
		"experiment_id", experiment.ID,
		"composite_score", fmt.Sprintf("%.2f", s.CompositeScore),
		"verdict", s.OverallVerdict,
		"duration", s.EndTime.Sub(s.StartTime).String(),
	)

	return nil
}

// evaluateCategory runs evaluation for a single category at a specific difficulty.
func (s *WildClawBenchSuite) evaluateCategory(experiment ExperimentResult, category BenchCategory, difficulty DifficultyLevel) (*CategoryResult, error) {
	categoryExperiment := s.adaptExperimentForCategory(experiment, category, difficulty)

	taskResult, err := s.Adapter.EvaluateExperiment(categoryExperiment)
	if err != nil {
		return nil, fmt.Errorf("evaluate experiment: %w", err)
	}

	results := []TaskResult{*taskResult}
	aggregate := CalculateAggregate(results)

	weight := s.Config.ScoringWeights[category]
	weightedScore := aggregate.Percentage * weight

	passed := aggregate.Percentage >= s.Config.MinPassThreshold

	return &CategoryResult{
		Category:      category,
		Difficulty:    difficulty,
		TaskResults:   results,
		Aggregate:     aggregate,
		WeightedScore: weightedScore,
		Passed:        passed,
	}, nil
}

// adaptExperimentForCategory modifies the experiment to focus on a specific category and difficulty.
func (s *WildClawBenchSuite) adaptExperimentForCategory(experiment ExperimentResult, category BenchCategory, difficulty DifficultyLevel) ExperimentResult {
	adapted := experiment
	adapted.Name = fmt.Sprintf("%s_%s_%s", experiment.Name, category, difficulty)

	categoryPrefix := fmt.Sprintf("// Category: %s, Difficulty: %s\n", category, difficulty)
	var adaptedChanges []string
	for _, change := range experiment.CodeChanges {
		adaptedChanges = append(adaptedChanges, categoryPrefix+change)
	}
	adapted.CodeChanges = adaptedChanges

	return adapted
}

// calculateCompositeScore computes the overall composite score across all categories and difficulties.
func (s *WildClawBenchSuite) calculateCompositeScore() {
	var totalWeightedScore float64
	var totalWeight float64
	var passCount int
	var totalCount int

	for category, diffResults := range s.CategoryResults {
		weight := s.Config.ScoringWeights[category]

		for _, catResult := range diffResults {
			totalCount++
			if catResult.Passed {
				passCount++
			}

			difficultyMultiplier := s.getDifficultyMultiplier(catResult.Difficulty)
			weightedScore := catResult.Aggregate.Percentage * weight * difficultyMultiplier

			totalWeightedScore += weightedScore
			totalWeight += weight * difficultyMultiplier
		}
	}

	if totalWeight > 0 {
		s.CompositeScore = totalWeightedScore / totalWeight
	}

	s.OverallVerdict = calculateVerdict(s.CompositeScore)

	s.logger.Info("composite score calculated",
		"composite_score", fmt.Sprintf("%.2f", s.CompositeScore),
		"pass_rate", fmt.Sprintf("%.1f%%", float64(passCount)/float64(totalCount)*100),
		"verdict", s.OverallVerdict,
	)
}

// getDifficultyMultiplier returns the weight multiplier for a difficulty level.
func (s *WildClawBenchSuite) getDifficultyMultiplier(difficulty DifficultyLevel) float64 {
	switch difficulty {
	case DifficultyEasy:
		return 1.0
	case DifficultyMedium:
		return 1.2
	case DifficultyHard:
		return 1.5
	case DifficultyExpert:
		return 2.0
	default:
		return 1.0
	}
}

// GetCategorySummary returns a summary of results for a specific category.
func (s *WildClawBenchSuite) GetCategorySummary(category BenchCategory) (float64, string, error) {
	diffResults, exists := s.CategoryResults[category]
	if !exists {
		return 0, "", fmt.Errorf("category %s not found", category)
	}

	var totalScore float64
	var count int

	for _, catResult := range diffResults {
		totalScore += catResult.Aggregate.Percentage
		count++
	}

	if count == 0 {
		return 0, "RED", nil
	}

	avgScore := totalScore / float64(count)
	verdict := calculateVerdict(avgScore)

	return avgScore, verdict, nil
}

// GetDifficultySummary returns a summary of results for a specific difficulty level across all categories.
func (s *WildClawBenchSuite) GetDifficultySummary(difficulty DifficultyLevel) (float64, string, error) {
	var totalScore float64
	var count int

	for _, diffResults := range s.CategoryResults {
		if catResult, exists := diffResults[difficulty]; exists {
			totalScore += catResult.Aggregate.Percentage
			count++
		}
	}

	if count == 0 {
		return 0, "RED", nil
	}

	avgScore := totalScore / float64(count)
	verdict := calculateVerdict(avgScore)

	return avgScore, verdict, nil
}

// GenerateReport produces a structured report of the benchmark results.
func (s *WildClawBenchSuite) GenerateReport() map[string]interface{} {
	report := make(map[string]interface{})

	report["experiment_id"] = s.Adapter.Rubric().AgentType
	report["agent_type"] = string(s.Adapter.Rubric().AgentType)
	report["start_time"] = s.StartTime.Format(time.RFC3339)
	report["end_time"] = s.EndTime.Format(time.RFC3339)
	report["duration"] = s.EndTime.Sub(s.StartTime).String()
	report["composite_score"] = s.CompositeScore
	report["overall_verdict"] = s.OverallVerdict

	categoryBreakdown := make(map[string]interface{})
	for category, diffResults := range s.CategoryResults {
		catData := make(map[string]interface{})
		for difficulty, catResult := range diffResults {
			catData[string(difficulty)] = map[string]interface{}{
				"percentage":     catResult.Aggregate.Percentage,
				"verdict":        catResult.Aggregate.Verdict,
				"passed":         catResult.Passed,
				"weighted_score": catResult.WeightedScore,
			}
		}
		categoryBreakdown[string(category)] = catData
	}
	report["category_breakdown"] = categoryBreakdown

	return report
}
