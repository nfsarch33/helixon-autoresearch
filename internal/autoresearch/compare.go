package autoresearch

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// CompareResult holds a comparison between two experiment runs.
type CompareResult struct {
	BaselineID   string             `json:"baseline_id"`
	CandidateID  string             `json:"candidate_id"`
	MetricDeltas map[string]float64 `json:"metric_deltas"`
	Improved     []string           `json:"improved"`
	Regressed    []string           `json:"regressed"`
	Unchanged    []string           `json:"unchanged"`
	Winner       string             `json:"winner"`
	Summary      string             `json:"summary"`
}

// RankedExperiment pairs an experiment result with a computed score.
type RankedExperiment struct {
	Result ExperimentResult `json:"result"`
	Score  float64          `json:"score"`
	Rank   int              `json:"rank"`
}

// CompareExperiments produces a structured comparison between two experiment results.
// Tolerance defines the absolute threshold below which a metric delta is considered unchanged.
func CompareExperiments(baseline, candidate ExperimentResult, tolerance float64) CompareResult {
	deltas := CompareMetrics(baseline.Metrics, candidate.Metrics)

	var improved, regressed, unchanged []string
	for metric, delta := range deltas {
		if math.Abs(delta) <= tolerance {
			unchanged = append(unchanged, metric)
		} else if delta > 0 {
			improved = append(improved, metric)
		} else {
			regressed = append(regressed, metric)
		}
	}

	sort.Strings(improved)
	sort.Strings(regressed)
	sort.Strings(unchanged)

	winner := determineWinner(improved, regressed)
	summary := buildSummary(baseline, candidate, improved, regressed, unchanged, winner)

	return CompareResult{
		BaselineID:   baseline.ID,
		CandidateID:  candidate.ID,
		MetricDeltas: deltas,
		Improved:     improved,
		Regressed:    regressed,
		Unchanged:    unchanged,
		Winner:       winner,
		Summary:      summary,
	}
}

// CompareMetrics computes the delta (after - before) for each metric present
// in either the baseline or candidate Metrics.After map.
func CompareMetrics(before, after Metrics) map[string]float64 {
	deltas := make(map[string]float64)

	allKeys := make(map[string]struct{})
	for k := range before.After {
		allKeys[k] = struct{}{}
	}
	for k := range after.After {
		allKeys[k] = struct{}{}
	}

	for k := range allKeys {
		baseVal := before.After[k]
		candVal := after.After[k]
		deltas[k] = candVal - baseVal
	}

	return deltas
}

// RankExperiments ranks a slice of experiment results by average metric improvement.
// Higher average metric values rank higher.
func RankExperiments(results []ExperimentResult) []RankedExperiment {
	if len(results) == 0 {
		return nil
	}

	ranked := make([]RankedExperiment, len(results))
	for i, r := range results {
		ranked[i] = RankedExperiment{
			Result: r,
			Score:  computeScore(r),
		}
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})

	for i := range ranked {
		ranked[i].Rank = i + 1
	}

	return ranked
}

func computeScore(r ExperimentResult) float64 {
	if len(r.Metrics.After) == 0 {
		return 0
	}
	var sum float64
	for _, v := range r.Metrics.After {
		sum += v
	}
	return sum / float64(len(r.Metrics.After))
}

func determineWinner(improved, regressed []string) string {
	if len(improved) > len(regressed) {
		return "candidate"
	}
	if len(regressed) > len(improved) {
		return "baseline"
	}
	return "tie"
}

func buildSummary(baseline, candidate ExperimentResult, improved, regressed, unchanged []string, winner string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Comparing %q (baseline) vs %q (candidate): ", baseline.Name, candidate.Name))

	switch winner {
	case "candidate":
		sb.WriteString("candidate wins. ")
	case "baseline":
		sb.WriteString("baseline wins. ")
	default:
		sb.WriteString("tie. ")
	}

	if len(improved) > 0 {
		sb.WriteString(fmt.Sprintf("Improved: %s. ", strings.Join(improved, ", ")))
	}
	if len(regressed) > 0 {
		sb.WriteString(fmt.Sprintf("Regressed: %s. ", strings.Join(regressed, ", ")))
	}
	if len(unchanged) > 0 {
		sb.WriteString(fmt.Sprintf("Unchanged: %s.", strings.Join(unchanged, ", ")))
	}

	return strings.TrimSpace(sb.String())
}
