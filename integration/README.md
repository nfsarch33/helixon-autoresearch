# runx-public-repo-gate: allow-file fleet_host_alias,network_topology
# helixon-autoresearch Integration

Connects the autoresearch experiment runner to the eval harness, the
10-stage academic research pipeline, and the Engram persistence layer.

## Architecture

```
                          ┌──────────────────────────────┐
                          │   PipelineRunner             │
                          │   (10-stage Karpathy method) │
                          └──────┬───────────────┬───────┘
                                 │               │
                    stage 6      ▼               ▼   every stage
                 (execution)  ┌──────┐    ┌──────────────┐
                              │ Eval │    │ EngramPersist │
              ┌───────────────│Harness│   │ (wsl1:8280)   │
              │               └───┬──┘   └──────────────┘
              ▼                   │
   ┌──────────────────┐           │
   │ LLM backends     │◀──────────┘
   │ (via router or   │  agent runs tasks; judge scores output
   │  direct API)     │
   └──────────────────┘
```

## Components

| Component | Location | Role |
|-----------|----------|------|
| Experiment Runner | `internal/autoresearch/` | Six-phase experiment loop (pre-existing) |
| Eval Harness | `eval/` | Agent-centric task suite + G-Eval judge (Sprint B) |
| Integration Layer | `integration/pipeline.go` | 10-stage pipeline wiring eval + autoresearch |
| Engram Persistence | `integration/engram_persist.go` | Save/search experiment metadata on wsl1:8280 |
| Tracking | SprintBoard MCP | Documented below; results attached to sprint tickets |

## Flow

1. `PipelineRunner.RunExperiment` starts the 10-stage pipeline.
2. Stages 1-5 (question, lit review, hypothesis, design, implementation)
   are metadata stages that capture the research narrative.
3. **Stage 6 (Execution)** runs `EvalHarness.Run`, which deploys a
   Helixon agent with each LLM backend, runs the task suite, and scores
   outputs via the G-Eval LLM-as-judge.
4. Stage 7 (Analysis) renders the comparative matrix.
5. Stage 8 (Validation) persists the checkpoint to Engram.
6. Stage 9 (Documentation) emits the eval report JSON.
7. Stage 10 (Dissemination) finalizes and logs the best backend.
8. Each stage boundary persists a checkpoint to Engram so partial
   progress survives crashes.

## 10-Stage Research Pipeline (Karpathy methodology)

| # | Stage | Eval harness role |
|---|-------|-------------------|
| 1 | Question formulation | Frame the agent-centric evaluation question |
| 2 | Literature review | Search Engram for prior eval experiments |
| 3 | Hypothesis generation | E.g. "backend X outperforms Y on long-context" |
| 4 | Experimental design | Select task suite + rubric version + backends |
| 5 | Implementation | Wire harness + persistence |
| 6 | Execution | `EvalHarness.Run` across all backends |
| 7 | Analysis | Comparative matrix + per-criterion breakdown |
| 8 | Validation | Cross-check scores against expected outputs |
| 9 | Documentation | Emit report JSON + text matrix |
| 10 | Dissemination | Persist to Engram; track in SprintBoard |

## Engram Persistence

The `EngramPersistor` posts experiment metadata to the Engram memory
engine on `wsl1:8280` (verified in Sprint A). The full eval report JSON
is stored as the memory content with metadata tags (`experiment_id`,
`stage`, `verdict`, `best_backend`, `rubric_version`) for later recall.

```go
persistor := integration.NewEngramPersistor(
    "http://100.119.90.30:8280", // wsl1 Engram
    "autoresearch-eval",
    "nfsarch33",
)
runner := integration.NewPipelineRunner(harness, persistor, logger)
err := runner.RunExperiment(ctx, &integration.EvalExperiment{
    ID:         "exp-2026-06-25-001",
    Name:       "sprint-b-backend-comparison",
    Question:   "Which LLM backend best supports Helixon agents across task types?",
    Hypothesis: "qwen3.7-max outperforms MiniMax-M3 on long-context stability",
})
```

## SprintBoard Tracking

Eval results are tracked in SprintBoard (the Helixon control-plane REST
client on `:9400`) by attaching the eval report to the sprint ticket as
evidence. The integration does not call SprintBoard directly; instead,
the CLI or an operator attaches the `--out` report JSON to the relevant
sprint ticket as a linked artifact. This keeps the integration layer
free of a hard SprintBoard dependency while still closing the tracking
loop.

To attach a report to a sprint ticket:

```bash
# After the eval run writes /tmp/eval-report.json:
runx sprintboard attach --sprint sprint-b --ticket s2-eval-harness \
  --artifact /tmp/eval-report.json --label "eval-report-$(date +%Y%m%d)"
```

## Credentials

All API keys are resolved from 1Password at runtime via `op read` —
never hardcoded, never written to disk, never logged in plaintext.

| Secret | 1Password reference |
|--------|---------------------|
| Aliyun API key | `op://Cursor_IronClaw/Aliyun Team Qwen Token Plan Key/password` |
| MiniMax API key | `op://Cursor_IronClaw/minimax-api-1/api-key` |

## Files

| File | Purpose |
|------|---------|
| `pipeline.go` | `PipelineRunner`, 10-stage stages, `EvalExperiment` |
| `engram_persist.go` | `EngramPersistor` (save/search/ping) |
