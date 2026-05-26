# Helixon Autoresearch — Agent Instructions

This is the Helixon autonomous ML experiment runner. Key packages:
- `cmd/autoresearch/` — CLI entry point
- `internal/experiment/` — Experiment loop, retry, dedup
- `internal/metrics/` — ExperimentMetrics collector
- `internal/engram/` — Engram memory integration
- `internal/comparison/` — Cross-run result comparison

Quality: 70%+ coverage, go test -race, go vet, golangci-lint, govulncheck, gosec.
