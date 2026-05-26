package autoresearch

import (
	"math"
	"testing"
	"time"
)

func approxEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestCompareExperiments_CandidateWins(t *testing.T) {
	baseline := ExperimentResult{
		ID:   "base-1",
		Name: "baseline-exp",
		Metrics: Metrics{
			After: map[string]float64{"accuracy": 0.80, "f1": 0.70, "throughput": 50.0},
		},
	}
	candidate := ExperimentResult{
		ID:   "cand-1",
		Name: "candidate-exp",
		Metrics: Metrics{
			After: map[string]float64{"accuracy": 0.90, "f1": 0.85, "throughput": 60.0},
		},
	}

	result := CompareExperiments(baseline, candidate, 0.01)

	if result.Winner != "candidate" {
		t.Fatalf("expected candidate to win, got %s", result.Winner)
	}
	if result.BaselineID != "base-1" {
		t.Fatalf("expected baseline id base-1, got %s", result.BaselineID)
	}
	if result.CandidateID != "cand-1" {
		t.Fatalf("expected candidate id cand-1, got %s", result.CandidateID)
	}
	if len(result.Improved) == 0 {
		t.Fatal("expected at least one improved metric")
	}
}

func TestCompareExperiments_BaselineWins(t *testing.T) {
	baseline := ExperimentResult{
		ID:   "base-1",
		Name: "baseline",
		Metrics: Metrics{
			After: map[string]float64{"accuracy": 0.95, "f1": 0.92, "latency": 10.0},
		},
	}
	candidate := ExperimentResult{
		ID:   "cand-1",
		Name: "candidate",
		Metrics: Metrics{
			After: map[string]float64{"accuracy": 0.85, "f1": 0.80, "latency": 5.0},
		},
	}

	result := CompareExperiments(baseline, candidate, 0.01)

	if result.Winner != "baseline" {
		t.Fatalf("expected baseline to win, got %s", result.Winner)
	}
	if len(result.Regressed) < 2 {
		t.Fatalf("expected at least 2 regressed metrics, got %d", len(result.Regressed))
	}
}

func TestCompareExperiments_Tie(t *testing.T) {
	baseline := ExperimentResult{
		ID:   "base-1",
		Name: "baseline",
		Metrics: Metrics{
			After: map[string]float64{"accuracy": 0.90, "latency": 10.0},
		},
	}
	candidate := ExperimentResult{
		ID:   "cand-1",
		Name: "candidate",
		Metrics: Metrics{
			After: map[string]float64{"accuracy": 0.95, "latency": 5.0},
		},
	}

	result := CompareExperiments(baseline, candidate, 0.01)

	if result.Winner != "tie" {
		t.Fatalf("expected tie, got %s", result.Winner)
	}
}

func TestCompareExperiments_Tolerance(t *testing.T) {
	baseline := ExperimentResult{
		ID:   "b",
		Name: "baseline",
		Metrics: Metrics{
			After: map[string]float64{"accuracy": 0.90},
		},
	}
	candidate := ExperimentResult{
		ID:   "c",
		Name: "candidate",
		Metrics: Metrics{
			After: map[string]float64{"accuracy": 0.905},
		},
	}

	result := CompareExperiments(baseline, candidate, 0.01)

	if len(result.Unchanged) != 1 {
		t.Fatalf("expected 1 unchanged metric, got %d", len(result.Unchanged))
	}
	if result.Winner != "tie" {
		t.Fatalf("expected tie with tolerance, got %s", result.Winner)
	}
}

func TestCompareMetrics_MixedKeys(t *testing.T) {
	before := Metrics{
		After: map[string]float64{"accuracy": 0.8, "loss": 0.5},
	}
	after := Metrics{
		After: map[string]float64{"accuracy": 0.9, "throughput": 100.0},
	}

	deltas := CompareMetrics(before, after)

	if !approxEqual(deltas["accuracy"], 0.1, 1e-9) {
		t.Fatalf("expected accuracy delta ~0.1, got %f", deltas["accuracy"])
	}
	if !approxEqual(deltas["loss"], -0.5, 1e-9) {
		t.Fatalf("expected loss delta ~-0.5, got %f", deltas["loss"])
	}
	if !approxEqual(deltas["throughput"], 100.0, 1e-9) {
		t.Fatalf("expected throughput delta ~100.0, got %f", deltas["throughput"])
	}
}

func TestCompareMetrics_EmptyMaps(t *testing.T) {
	deltas := CompareMetrics(Metrics{}, Metrics{})
	if len(deltas) != 0 {
		t.Fatalf("expected empty deltas for empty metrics, got %d", len(deltas))
	}
}

func TestRankExperiments_Ordering(t *testing.T) {
	results := []ExperimentResult{
		{ID: "low", Name: "low", Metrics: Metrics{After: map[string]float64{"score": 0.5}}},
		{ID: "high", Name: "high", Metrics: Metrics{After: map[string]float64{"score": 0.9}}},
		{ID: "mid", Name: "mid", Metrics: Metrics{After: map[string]float64{"score": 0.7}}},
	}

	ranked := RankExperiments(results)

	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked results, got %d", len(ranked))
	}
	if ranked[0].Result.ID != "high" {
		t.Fatalf("expected 'high' at rank 1, got %s", ranked[0].Result.ID)
	}
	if ranked[1].Result.ID != "mid" {
		t.Fatalf("expected 'mid' at rank 2, got %s", ranked[1].Result.ID)
	}
	if ranked[2].Result.ID != "low" {
		t.Fatalf("expected 'low' at rank 3, got %s", ranked[2].Result.ID)
	}
	if ranked[0].Rank != 1 || ranked[1].Rank != 2 || ranked[2].Rank != 3 {
		t.Fatal("rank numbers incorrect")
	}
}

func TestRankExperiments_Empty(t *testing.T) {
	ranked := RankExperiments(nil)
	if ranked != nil {
		t.Fatalf("expected nil for empty input, got %v", ranked)
	}
}

func TestRankExperiments_NoMetrics(t *testing.T) {
	results := []ExperimentResult{
		{ID: "a", Name: "a", Metrics: Metrics{}},
		{ID: "b", Name: "b", Metrics: Metrics{After: map[string]float64{"x": 1.0}}},
	}

	ranked := RankExperiments(results)
	if ranked[0].Result.ID != "b" {
		t.Fatalf("expected 'b' to rank first (has metrics), got %s", ranked[0].Result.ID)
	}
}

func TestCompareExperiments_SummaryContent(t *testing.T) {
	baseline := ExperimentResult{
		ID:        "b",
		Name:      "base",
		Timestamp: time.Now(),
		Metrics:   Metrics{After: map[string]float64{"accuracy": 0.8}},
	}
	candidate := ExperimentResult{
		ID:        "c",
		Name:      "cand",
		Timestamp: time.Now(),
		Metrics:   Metrics{After: map[string]float64{"accuracy": 0.9}},
	}

	result := CompareExperiments(baseline, candidate, 0.01)

	if result.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
}
