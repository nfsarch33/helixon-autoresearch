package autoresearch

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type SentruxMetric struct {
	Repo        string  `json:"repo"`
	Quality     int     `json:"quality"`
	ComplexFn   int     `json:"complex_fn"`
	Modularity  float64 `json:"modularity"`
	Acyclicity  float64 `json:"acyclicity"`
	MeasuredAt  string  `json:"measured_at"`
}

type SentruxPlugin struct {
	runxPath string
}

func NewSentruxPlugin(runxPath string) *SentruxPlugin {
	if runxPath == "" {
		runxPath = "runx"
	}
	return &SentruxPlugin{runxPath: runxPath}
}

func (p *SentruxPlugin) Measure(repoAlias string) (SentruxMetric, error) {
	cmd := exec.Command(p.runxPath, "sentrux", "gate", "--repo", repoAlias, "--json")
	out, err := cmd.Output()
	if err != nil {
		return SentruxMetric{}, fmt.Errorf("sentrux gate for %s: %w", repoAlias, err)
	}

	var metric SentruxMetric
	if err := json.Unmarshal(out, &metric); err != nil {
		return SentruxMetric{}, fmt.Errorf("parse sentrux output: %w", err)
	}

	metric.Repo = repoAlias
	metric.MeasuredAt = time.Now().Format(time.RFC3339)
	return metric, nil
}

func (p *SentruxPlugin) ToEvoSpineEvent(m SentruxMetric) map[string]interface{} {
	return map[string]interface{}{
		"event":       "sentrux_measurement",
		"repo":        m.Repo,
		"quality":     m.Quality,
		"complex_fn":  m.ComplexFn,
		"modularity":  m.Modularity,
		"acyclicity":  m.Acyclicity,
		"measured_at": m.MeasuredAt,
	}
}
