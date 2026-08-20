# runx-public-repo-gate: allow-file network_topology
# Helixon Autoresearch

Autonomous ML experiment runner inspired by [karpathy/autoresearch](https://github.com/karpathy/autoresearch).

## Features

- **Experiment Loop** — Six-phase lifecycle (ideation, implementation, training, evaluation, comparison, promotion) with per-phase timeouts
- **Engram Integration** — Persistent experiment memory via the Engram HTTP API
- **Deduplication** — Hypothesis-level dedup with case-insensitive normalisation
- **Retry** — Exponential backoff with jitter for transient Engram failures
- **Comparison** — Cross-run metric comparison with tolerance-based winner detection and ranking
- **Scheduler** — Fixed-interval or one-shot queue with configurable concurrency
- **Dashboard** — HTTP handler for experiment listing, history, metrics, and comparison
- **Sentrux Plugin** — Code quality measurement integration via runx

## Requirements

- Go 1.26+
- [Engram](https://github.com/nfsarch33/engram) memory engine (for persistence)

## Quick Start

```bash
go install github.com/nfsarch33/helixon-autoresearch/cmd/autoresearch@latest

# Run an experiment
export ENGRAM_URL=http://127.0.0.1:18888
autoresearch run "lr-sweep" "lower learning rate improves convergence"

# Search related experiments
autoresearch search "learning rate" 10

# View experiment history
autoresearch history <experiment-id>
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ENGRAM_URL` | `http://127.0.0.1:18888` | Engram API base URL |
| `ENGRAM_APP_ID` | `autoresearch` | Engram app ID |
| `ENGRAM_USER_ID` | `nfsarch33` | Engram user ID |

## Architecture

```
cmd/autoresearch/       CLI entry point (run, history, search)
internal/autoresearch/  Core packages:
  experiment.go         Types: ExperimentPhase, ExperimentResult, ExperimentConfig
  loop.go               ExperimentLoop with dedup, phase timeout, Engram persistence
  engram_client.go      EngramClient interface and HTTP implementation
  retry.go              RetryingEngramClient with exponential backoff
  compare.go            CompareExperiments, CompareMetrics, RankExperiments
  metrics.go            ExperimentMetrics collector with thread-safe snapshots
  scheduler.go          Scheduler with interval/one-shot modes and concurrency control
  dashboard.go          HTTP dashboard handler
  sentrux_plugin.go     Sentrux code quality measurement plugin
```

## Testing

```bash
go test -race -cover ./...
```

## License

MIT — see [LICENSE](LICENSE)
