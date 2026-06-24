# Helixon Agent-Centric Eval Harness

## Purpose

Evaluate **Helixon platform/fleet agents** using different LLM backends to
understand how the same agent performs across backends and task types.

> **Critical distinction:** This is AGENT-CENTRIC evaluation, NOT a
> model comparison. We deploy a Helixon agent with each LLM backend and
> measure the agent's end-to-end behavior. The backend is a treatment,
> not the subject of study.

## Backends (Sprint B)

| Name | Model | Endpoint |
|------|-------|----------|
| aliyun-qwen3.7-plus | `qwen3.7-plus` | `https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1` |
| aliyun-qwen3.7-max | `qwen3.7-max` | `https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1` |
| minimax-m3 | `MiniMax-M3` | `https://api.minimaxi.com/v1` |

Endpoints are the Aliyun **cn-beijing** (not international) and
**minimaxi.com** (not minimax.io) variants as confirmed in Sprint A.

A local `llm-cluster-router` on `wsl2:8787` can route all three via a
single OpenAI-compatible endpoint (verified in Sprint A). Pass
`--router http://<wsl2-ip>:8787` to use it.

## Metrics (7 rubric-based, G-Eval scored)

Each criterion is scored 1-5 by an LLM judge using **anchored rubrics**
(score=1 / score=3 / score=5 anchors) per the G-Eval LLM-as-judge pattern.

1. **Task Completion** — did the agent satisfy the full brief?
2. **Code Quality** — SOLID/DRY/KISS adherence
3. **Token Efficiency** — tokens used vs task complexity
4. **Context Utilization** — effective use of provided context
5. **Self-Improvement** — identifying and fixing own mistakes
6. **Long-Session Stability** — coherence across long sessions
7. **Error Recovery** — diagnosing and recovering from errors

Weights are rebalanced per task type (e.g. code-quality weighs heaviest
on code generation; self-improvement weighs heaviest on the
self-improvement task). Weights sum to 1.0 per rubric.

## Task Suite (7 task types)

| ID | Type | What it exercises |
|----|------|-------------------|
| cg-001 | code_generation | Write a rate-limited HTTP client wrapper |
| cr-001 | code_review | Review a PR with a concurrency bug |
| db-001 | debugging | Find and fix a nil-pointer dereference |
| dc-001 | documentation | Document an undocumented function |
| lc-001 | long_context | Summarize and answer across multiple files |
| si-001 | self_improvement | Identify and fix a mistake in prior output |
| mp-001 | multi_step_planning | Plan a canary deployment rollout |

## Scoring

- **G-Eval LLM-as-judge** pattern: a judge backend scores each agent
  output against the anchored rubric and returns strict JSON.
- **Versioned rubrics** (`RubricVersion = "2.0.0"`) so reports stay
  comparable across harness revisions.
- **1-5 integer scale** per criterion, normalized to a 0-100 weighted
  score per (task, backend), then aggregated into a backend x task
  comparative matrix.
- To avoid **self-preference bias**, the judge SHOULD be a backend
  distinct from the one being judged. The CLI defaults to the router's
  local vLLM model as a neutral judge when `--router` is set.

## Verdict bands

| Weighted score | Verdict |
|----------------|---------|
| >= 80 | GREEN |
| 50-79 | YELLOW |
| < 50 | RED |

## Usage

```bash
# Resolve API keys from 1Password (never hardcoded)
export ALIYUN_API_KEY=$(op read "op://Cursor_IronClaw/Aliyun Team Qwen Token Plan Key/password")
export MINIMAX_API_KEY=$(op read "op://Cursor_IronClaw/minimax-api-1/api-key")

# Run all backends on all tasks, routing through the llm-cluster-router
go run ./cmd/helixon-eval \
  --backends all \
  --tasks all \
  --router http://100.103.124.50:8787 \
  --out /tmp/eval-report.json

# Run a single backend on a single task type for a quick smoke test
go run ./cmd/helixon-eval \
  --backends aliyun-qwen3.7-plus \
  --tasks code_generation \
  --router http://100.103.124.50:8787
```

The CLI reads keys via `op read` directly; you do not need to export
them as environment variables unless another tool consumes them.

## Output

- **JSON report** (to `--out` path or stdout) with full per-result
  scores, the comparative matrix, and the headline summary.
- **Text matrix** to stderr showing backend x task weighted scores with
  per-backend averages and verdicts.

## Files

| File | Purpose |
|------|---------|
| `rubrics.go` | 7 anchored criteria + per-task-type rubrics + versioning |
| `tasks.go` | 7-task suite with prompts, context files, expected outputs |
| `harness.go` | `EvalHarness.Run()`, LLM backend, G-Eval judge, prompt/parsers |
| `report.go` | `EvalReport`, comparative `ScoreMatrix`, text renderer |
| `cmd/helixon-eval/main.go` | CLI: 1Password key resolution, flags, run |

## Integration

The eval harness integrates with `helixon-autoresearch` via the
`integration/` package, which wires the harness to the autoresearch
experiment runner, the 10-stage academic research pipeline, and the
Engram persistence layer on `wsl1:8280`. See
`integration/README.md`.
