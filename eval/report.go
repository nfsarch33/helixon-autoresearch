package eval

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// TaskResult captures a single (task, backend) evaluation outcome.
type TaskResult struct {
	TaskID          string                `json:"task_id"`
	TaskType        TaskTypeID            `json:"task_type"`
	TaskName        string                `json:"task_name"`
	Backend         string                `json:"backend"`
	Model           string                `json:"model"`
	Rubric          string                `json:"rubric"`
	AgentOutput     string                `json:"agent_output"`
	TokenUsage      TokenUsage            `json:"token_usage"`
	CriterionScores map[CriterionID]Score `json:"criterion_scores"`
	WeightedScore   float64               `json:"weighted_score"` // 0-100
	Verdict         string                `json:"verdict"`        // GREEN/YELLOW/RED
	DurationMS      int64                 `json:"duration_ms"`
	Error           string                `json:"error,omitempty"`
}

// EvalReport is the top-level output of a harness run.
type EvalReport struct {
	Timestamp     time.Time    `json:"timestamp"`
	RubricVersion string       `json:"rubric_version"`
	Results       []TaskResult `json:"results"`
	Matrix        ScoreMatrix  `json:"matrix"`
	Summary       Summary      `json:"summary"`
}

// ScoreMatrix is the backend x task x rubric comparative view.
type ScoreMatrix struct {
	ByBackend map[string]BackendScore `json:"by_backend"` // keyed by backend name
	ByTask    map[string]TaskScore    `json:"by_task"`    // keyed by task id
}

// BackendScore aggregates all tasks for one backend.
type BackendScore struct {
	Backend       string                  `json:"backend"`
	Model         string                  `json:"model"`
	AvgWeighted   float64                 `json:"avg_weighted"`
	TaskScores    map[string]float64      `json:"task_scores"`    // task id -> weighted score
	CriterionAvgs map[CriterionID]float64 `json:"criterion_avgs"` // criterion -> avg 1-5
	PassRate      float64                 `json:"pass_rate"`      // fraction GREEN
	Verdict       string                  `json:"verdict"`
}

// TaskScore aggregates all backends for one task.
type TaskScore struct {
	TaskID        string             `json:"task_id"`
	TaskType      TaskTypeID         `json:"task_type"`
	TaskName      string             `json:"task_name"`
	BackendScores map[string]float64 `json:"backend_scores"` // backend -> weighted score
	BestBackend   string             `json:"best_backend"`
	Spread        float64            `json:"spread"` // max - min across backends
}

// Summary is the headline view of the whole run.
type Summary struct {
	OverallVerdict string   `json:"overall_verdict"`
	BestBackend    string   `json:"best_backend"`
	BackendRanking []string `json:"backend_ranking"` // best to worst
	TotalTasks     int      `json:"total_tasks"`
	TotalBackends  int      `json:"total_backends"`
	TotalResults   int      `json:"total_results"`
}

// buildMatrix assembles the backend x task x rubric matrix from results.
func buildMatrix(results []TaskResult, backends []LLMBackend, tasks []Task) ScoreMatrix {
	byBackend := make(map[string]BackendScore)
	byTask := make(map[string]TaskScore)

	// Initialize per-backend and per-task slots.
	for _, b := range backends {
		byBackend[b.Name] = BackendScore{
			Backend:       b.Name,
			Model:         b.Model,
			TaskScores:    make(map[string]float64),
			CriterionAvgs: make(map[CriterionID]float64),
		}
	}
	for _, t := range tasks {
		byTask[t.ID] = TaskScore{
			TaskID:        t.ID,
			TaskType:      t.Type,
			TaskName:      t.Name,
			BackendScores: make(map[string]float64),
		}
	}

	// Tally results.
	type acc struct {
		sum, max, min float64
		count, green  int
		critSum       map[CriterionID]float64
		critCount     map[CriterionID]int
	}
	backendAcc := make(map[string]*acc)
	for _, b := range backends {
		backendAcc[b.Name] = &acc{
			max:       -1,
			min:       1e9,
			critSum:   make(map[CriterionID]float64),
			critCount: make(map[CriterionID]int),
		}
	}

	for _, r := range results {
		// Per-backend accumulation.
		ba := backendAcc[r.Backend]
		ba.sum += r.WeightedScore
		ba.count++
		if r.WeightedScore > ba.max {
			ba.max = r.WeightedScore
		}
		if r.WeightedScore < ba.min {
			ba.min = r.WeightedScore
		}
		if r.Verdict == "GREEN" {
			ba.green++
		}
		for cid, s := range r.CriterionScores {
			ba.critSum[cid] += float64(s.Value)
			ba.critCount[cid]++
		}
		bs := byBackend[r.Backend]
		bs.TaskScores[r.TaskID] = r.WeightedScore

		// Per-task accumulation.
		ts := byTask[r.TaskID]
		ts.BackendScores[r.Backend] = r.WeightedScore
	}

	// Finalize per-backend.
	for name, ba := range backendAcc {
		bs := byBackend[name]
		if ba.count > 0 {
			bs.AvgWeighted = ba.sum / float64(ba.count)
			bs.PassRate = float64(ba.green) / float64(ba.count) * 100
		}
		for cid, sum := range ba.critSum {
			if c := ba.critCount[cid]; c > 0 {
				bs.CriterionAvgs[cid] = sum / float64(c)
			}
		}
		bs.Verdict = verdictFromScore(bs.AvgWeighted)
		byBackend[name] = bs
	}

	// Finalize per-task: best backend and spread.
	for id, ts := range byTask {
		var best float64 = -1
		var worst, bestB float64 = 1e9, -1
		var bestName string
		for b, s := range ts.BackendScores {
			if s > best {
				best = s
				bestName = b
				bestB = s
			}
			if s < worst {
				worst = s
			}
		}
		ts.BestBackend = bestName
		if bestB >= 0 && worst <= 1e8 {
			ts.Spread = bestB - worst
		}
		byTask[id] = ts
	}

	return ScoreMatrix{ByBackend: byBackend, ByTask: byTask}
}

// buildSummary produces the headline ranking from the matrix.
func buildSummary(results []TaskResult, backends []LLMBackend) Summary {
	matrix := buildMatrix(results, backends, nil)
	// Re-derive ranking by AvgWeighted descending.
	type pair struct {
		name string
		avg  float64
	}
	var pairs []pair
	for name, bs := range matrix.ByBackend {
		pairs = append(pairs, pair{name, bs.AvgWeighted})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].avg > pairs[j].avg })

	ranking := make([]string, 0, len(pairs))
	for _, p := range pairs {
		ranking = append(ranking, p.name)
	}

	s := Summary{
		TotalTasks:     countUniqueTasks(results),
		TotalBackends:  len(backends),
		TotalResults:   len(results),
		BackendRanking: ranking,
	}
	if len(ranking) > 0 {
		s.BestBackend = ranking[0]
	}
	// Overall verdict = average of backend avg scores.
	var total float64
	var n int
	for _, bs := range matrix.ByBackend {
		total += bs.AvgWeighted
		n++
	}
	if n > 0 {
		s.OverallVerdict = verdictFromScore(total / float64(n))
	}
	return s
}

// countUniqueTasks returns the number of distinct task IDs in results.
func countUniqueTasks(results []TaskResult) int {
	seen := make(map[string]struct{})
	for _, r := range results {
		seen[r.TaskID] = struct{}{}
	}
	return len(seen)
}

// RenderText produces a human-readable comparative matrix table from a
// report. Rows are backends, columns are task IDs, cells are scores.
func (r *EvalReport) RenderText() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Helixon Agent-Centric Eval Report\n"))
	sb.WriteString(fmt.Sprintf("Timestamp: %s  Rubric: v%s\n\n", r.Timestamp.Format(time.RFC3339), r.RubricVersion))

	// Header row: backend | task1 | task2 | ... | avg | verdict
	taskIDs := make([]string, 0, len(r.Matrix.ByTask))
	for id := range r.Matrix.ByTask {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)

	sb.WriteString(fmt.Sprintf("%-24s", "backend"))
	for _, id := range taskIDs {
		sb.WriteString(fmt.Sprintf(" %8s", id))
	}
	sb.WriteString(fmt.Sprintf(" %8s %8s\n", "avg", "verdict"))

	// Sort backends by ranking order.
	backendOrder := r.Summary.BackendRanking
	if len(backendOrder) == 0 {
		for b := range r.Matrix.ByBackend {
			backendOrder = append(backendOrder, b)
		}
		sort.Strings(backendOrder)
	}

	for _, b := range backendOrder {
		bs := r.Matrix.ByBackend[b]
		sb.WriteString(fmt.Sprintf("%-24s", b))
		for _, id := range taskIDs {
			if v, ok := bs.TaskScores[id]; ok {
				sb.WriteString(fmt.Sprintf(" %8.1f", v))
			} else {
				sb.WriteString(fmt.Sprintf(" %8s", "-"))
			}
		}
		sb.WriteString(fmt.Sprintf(" %8.1f %8s\n", bs.AvgWeighted, bs.Verdict))
	}

	sb.WriteString(fmt.Sprintf("\nBest backend: %s | Overall verdict: %s | Results: %d\n",
		r.Summary.BestBackend, r.Summary.OverallVerdict, r.Summary.TotalResults))
	return sb.String()
}
