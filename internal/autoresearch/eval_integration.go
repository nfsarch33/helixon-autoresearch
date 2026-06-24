package autoresearch

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// EvalAgentType represents the type of agent being evaluated.
type EvalAgentType string

const (
	AgentFleetCoder      EvalAgentType = "fleet_coder"
	AgentFleetPRReviewer EvalAgentType = "fleet_pr_reviewer"
	AgentFleetDevOps     EvalAgentType = "fleet_devops"
)

// RubricCriterion defines a single evaluation criterion with scoring guidance.
type RubricCriterion struct {
	ID       string
	Name     string
	Weight   float64
	MaxScore int
}

// AgentRubric defines the complete rubric for an agent type.
type AgentRubric struct {
	AgentType  EvalAgentType
	Version    string
	Criteria   []RubricCriterion
	TotalTasks int
	MaxScore   int
}

// TaskResult captures the result of evaluating a single task against a rubric.
type TaskResult struct {
	TaskID          string
	AgentType       EvalAgentType
	Timestamp       time.Time
	Duration        time.Duration
	CriterionScores map[string]int
	TotalScore      int
	MaxScore        int
	Percentage      float64
	Verdict         string
	Output          string
	Error           string
}

// AggregateStats contains aggregated statistics across all task results.
type AggregateStats struct {
	TotalScore        int
	MaxScore          int
	AverageScore      float64
	Percentage        float64
	PassRate          float64
	CriterionAverages map[string]float64
	Verdict           string
}

// EvalHarnessAdapter bridges the autoresearch framework with eval-harness scoring.
type EvalHarnessAdapter struct {
	logger *slog.Logger
	rubric *AgentRubric
}

// NewEvalHarnessAdapter creates an adapter for the given agent type.
func NewEvalHarnessAdapter(agentType EvalAgentType, logger *slog.Logger) (*EvalHarnessAdapter, error) {
	if logger == nil {
		logger = slog.Default()
	}
	rubric, err := rubricForAgent(agentType)
	if err != nil {
		return nil, fmt.Errorf("create adapter: %w", err)
	}
	return &EvalHarnessAdapter{
		logger: logger,
		rubric: rubric,
	}, nil
}

// Rubric returns the underlying rubric for inspection.
func (a *EvalHarnessAdapter) Rubric() *AgentRubric {
	return a.rubric
}

// EvaluateExperiment scores an experiment result against the eval-harness rubric.
// It converts the experiment's code changes and output into criterion-level scores
// and returns a structured TaskResult suitable for cross-experiment comparison.
func (a *EvalHarnessAdapter) EvaluateExperiment(result ExperimentResult) (*TaskResult, error) {
	if result.ID == "" {
		return nil, fmt.Errorf("experiment result has empty ID")
	}

	a.logger.Info("evaluating experiment",
		"experiment_id", result.ID,
		"agent_type", string(a.rubric.AgentType),
	)

	output := buildOutputFromExperiment(result)
	duration := result.Duration

	tr := &TaskResult{
		TaskID:          result.ID,
		AgentType:       a.rubric.AgentType,
		Timestamp:       time.Now(),
		Duration:        duration,
		CriterionScores: make(map[string]int),
		Output:          output,
	}

	for _, criterion := range a.rubric.Criteria {
		score := scoreCriterion(criterion, output, result)
		tr.CriterionScores[criterion.ID] = score
		tr.TotalScore += score
		tr.MaxScore += criterion.MaxScore
	}

	if tr.MaxScore > 0 {
		tr.Percentage = float64(tr.TotalScore) / float64(tr.MaxScore) * 100
	}
	tr.Verdict = calculateVerdict(tr.Percentage)

	if result.Error != "" {
		tr.Error = result.Error
	}

	a.logger.Info("evaluation complete",
		"experiment_id", result.ID,
		"total_score", tr.TotalScore,
		"max_score", tr.MaxScore,
		"percentage", fmt.Sprintf("%.1f", tr.Percentage),
		"verdict", tr.Verdict,
	)

	return tr, nil
}

// CalculateAggregate computes aggregate statistics across multiple task results.
func CalculateAggregate(results []TaskResult) AggregateStats {
	if len(results) == 0 {
		return AggregateStats{Verdict: "RED", CriterionAverages: make(map[string]float64)}
	}

	agg := AggregateStats{
		CriterionAverages: make(map[string]float64),
	}

	totalScore := 0
	maxScore := 0
	passCount := 0
	criterionTotals := make(map[string]int)
	criterionCounts := make(map[string]int)

	for _, r := range results {
		totalScore += r.TotalScore
		maxScore += r.MaxScore
		if r.Percentage >= 70 {
			passCount++
		}
		for cid, score := range r.CriterionScores {
			criterionTotals[cid] += score
			criterionCounts[cid]++
		}
	}

	agg.TotalScore = totalScore
	agg.MaxScore = maxScore
	if maxScore > 0 {
		agg.Percentage = float64(totalScore) / float64(maxScore) * 100
		agg.AverageScore = float64(totalScore) / float64(len(results))
	}
	agg.PassRate = float64(passCount) / float64(len(results)) * 100
	agg.Verdict = calculateVerdict(agg.Percentage)

	for cid, total := range criterionTotals {
		count := criterionCounts[cid]
		if count > 0 {
			agg.CriterionAverages[cid] = float64(total) / float64(count)
		}
	}

	return agg
}

// ScoreExperimentMetrics converts a TaskResult into the autoresearch Metrics format
// so that experiment comparison logic can use eval-harness scores.
func ScoreExperimentMetrics(tr *TaskResult) Metrics {
	after := make(map[string]float64, len(tr.CriterionScores))
	for cid, score := range tr.CriterionScores {
		after[cid] = float64(score)
	}
	after["overall_percentage"] = tr.Percentage
	return Metrics{
		Before: make(map[string]float64),
		After:  after,
	}
}

func buildOutputFromExperiment(result ExperimentResult) string {
	var sb strings.Builder
	for _, change := range result.CodeChanges {
		sb.WriteString(change)
		sb.WriteString("\n")
	}
	if result.Hypothesis != "" {
		sb.WriteString(result.Hypothesis)
	}
	return sb.String()
}

func scoreCriterion(criterion RubricCriterion, output string, result ExperimentResult) int {
	switch criterion.ID {
	case "code_correctness":
		return scoreCodeCorrectness(output)
	case "test_coverage":
		return scoreTestCoverage(output)
	case "code_quality":
		return scoreCodeQuality(output)
	case "task_completion":
		return scoreTaskCompletion(output, result)
	case "error_handling":
		return scoreErrorHandling(output)
	case "review_thoroughness":
		return scoreReviewThoroughness(output)
	case "false_positive_rate":
		return scoreFalsePositiveRate(output)
	case "actionable_feedback":
		return scoreActionableFeedback(output)
	case "security_awareness":
		return scoreSecurityAwareness(output)
	case "performance_awareness":
		return scorePerformanceAwareness(output)
	case "deployment_success":
		return scoreDeploymentSuccess(output)
	case "monitoring_setup":
		return scoreMonitoringSetup(output)
	case "incident_response":
		return scoreIncidentResponse(output)
	case "documentation_quality":
		return scoreDocumentationQuality(output)
	case "automation_coverage":
		return scoreAutomationCoverage(output)
	default:
		return scoreGeneric(output)
	}
}

func scoreCodeCorrectness(output string) int {
	if containsAny(output, []string{"error", "failed", "undefined", "cannot", "invalid"}) {
		return 3
	}
	if containsAny(output, []string{"func ", "package ", "import "}) {
		if containsAny(output, []string{"return", "{"}) {
			return 8
		}
		return 6
	}
	return 4
}

func scoreTestCoverage(output string) int {
	hasTests := containsAny(output, []string{"func Test", "t.Run", "testing.T"})
	hasCases := strings.Count(output, "t.Run") >= 3 || strings.Count(output, "name:") >= 3
	if hasTests && hasCases {
		return 9
	}
	if hasTests {
		return 6
	}
	return 2
}

func scoreCodeQuality(output string) int {
	score := 5
	if strings.Contains(output, "error") && strings.Contains(output, "return") {
		score += 2
	}
	if strings.Contains(output, "//") || strings.Contains(output, "/*") {
		score++
	}
	if !containsAny(output, []string{"TODO", "FIXME", "XXX"}) {
		score++
	}
	return clamp(score, 0, 10)
}

func scoreTaskCompletion(output string, result ExperimentResult) int {
	if len(strings.TrimSpace(output)) < 20 {
		return 2
	}
	if result.Status == StatusCompleted {
		return 9
	}
	if len(output) > 100 {
		return 7
	}
	return 5
}

func scoreErrorHandling(output string) int {
	score := 4
	if strings.Contains(output, "if err != nil") {
		score += 3
	}
	if containsAny(output, []string{"return err", "fmt.Errorf", "errors.New"}) {
		score += 2
	}
	if containsAny(output, []string{"recover", "defer"}) {
		score++
	}
	return clamp(score, 0, 10)
}

func scoreReviewThoroughness(output string) int {
	score := 5
	issues := strings.Count(strings.ToLower(output), "issue") +
		strings.Count(strings.ToLower(output), "bug") +
		strings.Count(strings.ToLower(output), "problem")
	if issues >= 3 {
		score += 3
	} else if issues >= 1 {
		score++
	}
	if strings.Contains(output, "line ") || strings.Contains(output, "L") {
		score += 2
	}
	return clamp(score, 0, 10)
}

func scoreFalsePositiveRate(output string) int {
	score := 7
	lower := strings.ToLower(output)
	if strings.Contains(lower, "looks good") || strings.Contains(lower, "no issues") {
		score += 2
	}
	return clamp(score, 0, 10)
}

func scoreActionableFeedback(output string) int {
	score := 5
	if containsAny(output, []string{"consider", "suggest", "recommend", "should"}) {
		score += 2
	}
	if strings.Contains(output, "```") {
		score += 2
	}
	if strings.Contains(output, "example") || strings.Contains(output, "e.g.") {
		score++
	}
	return clamp(score, 0, 10)
}

func scoreSecurityAwareness(output string) int {
	score := 4
	terms := []string{"security", "vulnerability", "injection", "authentication",
		"authorization", "sanitize", "validate", "credential", "secret"}
	for _, term := range terms {
		if strings.Contains(strings.ToLower(output), term) {
			score += 2
			break
		}
	}
	return clamp(score, 0, 10)
}

func scorePerformanceAwareness(output string) int {
	score := 4
	terms := []string{"performance", "optimize", "efficient", "slow", "bottleneck",
		"cache", "memory", "cpu", "latency"}
	for _, term := range terms {
		if strings.Contains(strings.ToLower(output), term) {
			score += 2
			break
		}
	}
	return clamp(score, 0, 10)
}

func scoreDeploymentSuccess(output string) int {
	if containsAny(output, []string{"running", "deployed", "started", "success"}) {
		return 8
	}
	if containsAny(output, []string{"error", "failed", "crash"}) {
		return 3
	}
	return 5
}

func scoreMonitoringSetup(output string) int {
	score := 4
	terms := []string{"metric", "alert", "monitor", "log", "dashboard", "prometheus", "grafana"}
	for _, term := range terms {
		if strings.Contains(strings.ToLower(output), term) {
			score += 2
		}
	}
	return clamp(score, 0, 10)
}

func scoreIncidentResponse(output string) int {
	score := 5
	if containsAny(output, []string{"diagnose", "investigate", "root cause"}) {
		score += 2
	}
	if containsAny(output, []string{"fix", "resolve", "mitigate"}) {
		score += 2
	}
	if containsAny(output, []string{"prevent", "future", "improvement"}) {
		score++
	}
	return clamp(score, 0, 10)
}

func scoreDocumentationQuality(output string) int {
	score := 5
	if len(output) > 200 {
		score += 2
	}
	if strings.Contains(output, "##") || strings.Contains(output, "#") {
		score++
	}
	if containsAny(output, []string{"step", "procedure", "instruction"}) {
		score += 2
	}
	return clamp(score, 0, 10)
}

func scoreAutomationCoverage(output string) int {
	score := 5
	terms := []string{"automate", "script", "ci/cd", "pipeline", "workflow", "cron", "job"}
	for _, term := range terms {
		if strings.Contains(strings.ToLower(output), term) {
			score += 2
		}
	}
	return clamp(score, 0, 10)
}

func scoreGeneric(output string) int {
	if output == "" {
		return 0
	}
	if len(output) < 50 {
		return 4
	}
	if len(output) < 200 {
		return 6
	}
	return 7
}

func calculateVerdict(percentage float64) string {
	switch {
	case percentage >= 80:
		return "GREEN"
	case percentage >= 50:
		return "YELLOW"
	default:
		return "RED"
	}
}

func containsAny(s string, substrs []string) bool {
	lower := strings.ToLower(s)
	for _, sub := range substrs {
		if strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func rubricForAgent(agentType EvalAgentType) (*AgentRubric, error) {
	switch agentType {
	case AgentFleetCoder:
		return fleetCoderRubric(), nil
	case AgentFleetPRReviewer:
		return fleetPRReviewerRubric(), nil
	case AgentFleetDevOps:
		return fleetDevOpsRubric(), nil
	default:
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}
}

func fleetCoderRubric() *AgentRubric {
	return &AgentRubric{
		AgentType: AgentFleetCoder,
		Version:   "1.0",
		Criteria: []RubricCriterion{
			{ID: "code_correctness", Name: "Code Correctness", Weight: 0.25, MaxScore: 10},
			{ID: "test_coverage", Name: "Test Coverage", Weight: 0.20, MaxScore: 10},
			{ID: "code_quality", Name: "Code Quality", Weight: 0.20, MaxScore: 10},
			{ID: "task_completion", Name: "Task Completion", Weight: 0.20, MaxScore: 10},
			{ID: "error_handling", Name: "Error Handling", Weight: 0.15, MaxScore: 10},
		},
		TotalTasks: 10,
		MaxScore:   50,
	}
}

func fleetPRReviewerRubric() *AgentRubric {
	return &AgentRubric{
		AgentType: AgentFleetPRReviewer,
		Version:   "1.0",
		Criteria: []RubricCriterion{
			{ID: "review_thoroughness", Name: "Review Thoroughness", Weight: 0.25, MaxScore: 10},
			{ID: "false_positive_rate", Name: "False Positive Rate", Weight: 0.20, MaxScore: 10},
			{ID: "actionable_feedback", Name: "Actionable Feedback", Weight: 0.20, MaxScore: 10},
			{ID: "security_awareness", Name: "Security Awareness", Weight: 0.20, MaxScore: 10},
			{ID: "performance_awareness", Name: "Performance Awareness", Weight: 0.15, MaxScore: 10},
		},
		TotalTasks: 10,
		MaxScore:   50,
	}
}

func fleetDevOpsRubric() *AgentRubric {
	return &AgentRubric{
		AgentType: AgentFleetDevOps,
		Version:   "1.0",
		Criteria: []RubricCriterion{
			{ID: "deployment_success", Name: "Deployment Success", Weight: 0.25, MaxScore: 10},
			{ID: "monitoring_setup", Name: "Monitoring Setup", Weight: 0.20, MaxScore: 10},
			{ID: "incident_response", Name: "Incident Response", Weight: 0.20, MaxScore: 10},
			{ID: "documentation_quality", Name: "Documentation Quality", Weight: 0.15, MaxScore: 10},
			{ID: "automation_coverage", Name: "Automation Coverage", Weight: 0.20, MaxScore: 10},
		},
		TotalTasks: 10,
		MaxScore:   50,
	}
}
