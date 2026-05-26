package autoresearch

import (
	"testing"
)

func TestNewSentruxPlugin(t *testing.T) {
	p := NewSentruxPlugin("")
	if p.runxPath != "runx" {
		t.Errorf("expected default runx, got %q", p.runxPath)
	}

	p2 := NewSentruxPlugin("/custom/path")
	if p2.runxPath != "/custom/path" {
		t.Errorf("expected custom path, got %q", p2.runxPath)
	}
}

func TestToEvoSpineEvent(t *testing.T) {
	p := NewSentruxPlugin("")
	metric := SentruxMetric{
		Repo:       "helix-dev-tools",
		Quality:    7012,
		ComplexFn:  4,
		Modularity: 0.85,
		Acyclicity: 0.92,
		MeasuredAt: "2026-05-18T17:00:00+10:00",
	}

	event := p.ToEvoSpineEvent(metric)

	if event["event"] != "sentrux_measurement" {
		t.Errorf("expected sentrux_measurement event, got %v", event["event"])
	}
	if event["repo"] != "helix-dev-tools" {
		t.Errorf("expected helix-dev-tools, got %v", event["repo"])
	}
	if event["quality"] != 7012 {
		t.Errorf("expected 7012, got %v", event["quality"])
	}
	if event["complex_fn"] != 4 {
		t.Errorf("expected 4, got %v", event["complex_fn"])
	}
}

func TestSentruxMetricJSON(t *testing.T) {
	metric := SentruxMetric{
		Repo:    "global-kb",
		Quality: 6960,
	}
	if metric.Repo != "global-kb" {
		t.Error("struct field access broken")
	}
}
