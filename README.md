# Helixon Autoresearch

Autonomous ML experiment runner inspired by [karpathy/autoresearch](https://github.com/karpathy/autoresearch). Features:

- **Experiment Loop** — Agent-driven code mutation with fixed-time training budgets
- **Retry & Dedup** — Exponential backoff, experiment deduplication
- **Metrics** — ExperimentMetrics collector with per-phase context timeouts
- **Engram Integration** — Persistent memory for experiment results and learnings
- **Comparison** — Cross-run result comparison and trend analysis

## Requirements

- Go 1.25.6+
- Engram memory engine (for persistence)

## Quick Start

```bash
go install github.com/nfsarch33/helixon-autoresearch/cmd/autoresearch@latest
autoresearch --help
```

## License

MIT — see [LICENSE](LICENSE)
