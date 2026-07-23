# Helixon Autoresearch — Agent Instructions

Autonomous ML experiment runner. Extracted from helixon-ec (ai-agent-business-stack).

## Package Structure

- `cmd/autoresearch/` — CLI entry point (run, history, search commands)
- `internal/autoresearch/` — Core library (single package, no sub-packages)
  - `experiment.go` — Types: ExperimentPhase, ExperimentResult, ExperimentConfig
  - `loop.go` — ExperimentLoop orchestrating six phases with dedup and Engram persistence
  - `engram_client.go` — EngramClient interface + HTTPEngramClient
  - `retry.go` — RetryingEngramClient with exponential backoff and jitter
  - `compare.go` — CompareExperiments, CompareMetrics, RankExperiments
  - `metrics.go` — Thread-safe ExperimentMetrics collector with snapshots
  - `scheduler.go` — Fixed-interval or one-shot experiment scheduler with concurrency
  - `dashboard.go` — HTTP handler for experiment CRUD and comparison
  - `sentrux_plugin.go` — Sentrux code quality measurement via runx

## Quality Gates

- `go test -race -cover ./...` — target 80%+ coverage
- `go vet ./...`
- `golangci-lint run`
- No credential leaks

## Conventions

- Go stdlib first. Single external dep: `github.com/google/uuid`.
- Conventional commits: `type(scope): message`.
- Identity: `nfsarch33` / SSH `~/.ssh/<key>`.
