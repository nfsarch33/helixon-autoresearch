// Package main is the helixon-eval CLI entry point. It runs the
// agent-centric eval harness across the three Sprint B LLM backends,
// resolving all API keys from 1Password (never hardcoded).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-autoresearch/eval"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// DEPRECATED (v18699-2): this CLI is deprecated. The canonical
	// helixon-eval binary is github.com/nfsarch33/helixon-platform/cmd/helixon-eval
	// per ADR-075. This binary remains functional for backward compatibility.
	// New code SHOULD install and use the platform binary via `runx eval run --all`.
	logger.Warn("DEPRECATED: cmd/helixon-eval is deprecated; use helixon-platform/cmd/helixon-eval (ADR-075)",
		"sprint", "v18699-2",
		"see", "cursor-global-kb/adrs/ADR-075-helixon-eval-binary-canonicity.md",
	)

	backendsFlag := flag.String("backends", "all", "comma-separated backend names or 'all'")
	tasksFlag := flag.String("tasks", "all", "comma-separated task type ids or 'all'")
	judgeFlag := flag.String("judge", "", "backend name to use as G-Eval judge (defaults to none of the candidates; use a distinct backend to avoid self-preference)")
	routerFlag := flag.String("router", "", "optional llm-cluster-router base URL; if set, all backends route through it")
	outFlag := flag.String("out", "", "path to write the JSON report (default: stdout)")
	timeoutFlag := flag.Duration("timeout", 20*time.Minute, "total run timeout")
	// Engram persistence flags (v14502-04b). If --engram-url is empty
	// and HELIXON_EVAL_ENGRAM_URL is unset, persistence is skipped.
	engramURLFlag := flag.String("engram-url", "", "Engram MCP /memories base URL (default: HELIXON_EVAL_ENGRAM_URL; if both empty, persistence is skipped)")
	engramAppIDFlag := flag.String("engram-app-id", "", "Engram app_id (default HELIXON_EVAL_ENGRAM_APP_ID or helixon-eval)")
	engramUserIDFlag := flag.String("engram-user-id", "", "Engram user_id (default HELIXON_EVAL_ENGRAM_USER_ID or nfsarch33)")
	engramTimeoutFlag := flag.Duration("engram-timeout", 0, "per-request Engram POST timeout (default HELIXON_EVAL_ENGRAM_TIMEOUT or 30s)")
	experimentIDFlag := flag.String("experiment-id", "", "experiment id used as Engram metadata key (default: helixon-eval-YYYYMMDD-HHMMSS)")
	flag.Parse()

	// Resolve API keys from 1Password. These MUST never be hardcoded.
	keys, err := resolveKeys(liveOpRead{})
	if err != nil {
		logger.Error("failed to resolve API keys from 1Password", "err", err)
		os.Exit(1)
	}
	picked, label := pickMinimaxKey(keys, nil)
	logger.Info("resolved API keys from 1Password",
		"aliyun", masked(keys.aliyun),
		"minimax_active", label,
		"minimax_value", masked(picked),
	)

	backends := buildBackends(*backendsFlag, *routerFlag, keys)
	if len(backends) == 0 {
		logger.Error("no backends selected", "flag", *backendsFlag)
		os.Exit(1)
	}

	tasks := buildTasks(*tasksFlag)
	harness := eval.NewEvalHarness(backends).
		WithTasks(tasks).
		WithLogger(logger)

	// Judge selection: prefer an explicit flag, else pick a backend
	// distinct from all agent backends if possible. For Sprint B we
	// default to the llm-cluster-router's local vLLM model as a
	// neutral judge when a router is configured.
	if *judgeFlag != "" {
		if j, ok := findBackend(backends, *judgeFlag); ok {
			harness = harness.WithJudge(j)
		}
	} else if *routerFlag != "" {
		harness = harness.WithJudge(eval.LLMBackend{
			Name:   "router-judge",
			Model:  "Qwen/Qwen2.5-7B-Instruct",
			Router: *routerFlag,
		})
	} else {
		// Fallback: use the first backend as judge (note self-preference risk).
		harness = harness.WithJudge(backends[0])
		logger.Warn("no distinct judge configured; using first backend as judge (self-preference risk)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeoutFlag)
	defer cancel()

	report, err := harness.Run(ctx)
	if err != nil {
		logger.Error("eval run failed", "err", err)
		os.Exit(1)
	}

	// Emit the comparative matrix to stderr for visibility.
	fmt.Fprintln(os.Stderr, report.RenderText())

	// Write JSON report.
	var data []byte
	if *outFlag != "" {
		data, err = json.MarshalIndent(report, "", "  ")
		if err != nil {
			logger.Error("marshal report", "err", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*outFlag, data, 0o644); err != nil {
			logger.Error("write report", "path", *outFlag, "err", err)
			os.Exit(1)
		}
		logger.Info("report written", "path", *outFlag, "results", len(report.Results))
	} else {
		data, err = json.MarshalIndent(report, "", "  ")
		if err != nil {
			logger.Error("marshal report", "err", err)
			os.Exit(1)
		}
		fmt.Println(string(data))
	}

	// Best-effort Engram persistence (v14502-04b). Skipped entirely when
	// --engram-url is empty and HELIXON_EVAL_ENGRAM_URL is unset.
	engramCfg := LoadEngramConfig(EngramConfig{
		URL:     *engramURLFlag,
		AppID:   *engramAppIDFlag,
		UserID:  *engramUserIDFlag,
		Timeout: *engramTimeoutFlag,
	})
	expID := *experimentIDFlag
	if expID == "" {
		expID = "helixon-eval-" + time.Now().UTC().Format("20060102-150405")
	}
	exp := BuildEngramExperiment(expID, "helixon-eval", "agent-centric eval harness", *report)
	persistReportOptional(ctx, logger, &realHTTPSender{client: &http.Client{Timeout: engramCfg.Timeout}}, engramCfg, exp)
}

// opReader is the interface 1Password secrets are read through. Production
// wires this to the package-level opRead() (which shells out to `op`); tests
// substitute a fake map-backed reader so resolveKeys is fully unit-testable
// without touching the live vault or shelling out.
type opReader interface {
	Read(ref string) (string, error)
}

// liveOpRead is the production opReader; it shells out to the 1Password CLI.
// Keys are never written to disk or logged in plaintext.
type liveOpRead struct{}

func (liveOpRead) Read(ref string) (string, error) {
	return opRead(ref)
}

// keyBundle holds API keys resolved from 1Password.
type keyBundle struct {
	aliyun   string
	minimax1 string
	minimax2 string
}

// Aliyun key 1Password reference.
const opRefAliyun = "op://Cursor_IronClaw/Aliyun Team Qwen Token Plan Key/password"

// opRefMinimax1 is the primary MiniMax API key (active by default).
const opRefMinimax1 = "op://Cursor_IronClaw/minimax-api-1/api-key"

// opRefMinimax2 is the secondary MiniMax API key (failover).
const opRefMinimax2 = "op://Cursor_IronClaw/minimax-api-2/api-key"

// resolveKeys reads all credentials from the supplied opReader. Keys are
// never written to disk or logged. Both MiniMax keys are required so that
// pickMinimaxKey has a failover candidate at all times.
func resolveKeys(r opReader) (keyBundle, error) {
	aliyun, err := r.Read(opRefAliyun)
	if err != nil {
		return keyBundle{}, fmt.Errorf("aliyun key: %w", err)
	}
	minimax1, err := r.Read(opRefMinimax1)
	if err != nil {
		return keyBundle{}, fmt.Errorf("minimax key1: %w", err)
	}
	minimax2, err := r.Read(opRefMinimax2)
	if err != nil {
		return keyBundle{}, fmt.Errorf("minimax key2: %w", err)
	}
	return keyBundle{aliyun: aliyun, minimax1: minimax1, minimax2: minimax2}, nil
}

// opRead invokes the 1Password CLI to read a credential reference.
func opRead(ref string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "op", "read", ref)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("op read %s: %w", ref, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("op read %s returned empty value", ref)
	}
	return v, nil
}

// masked returns a masked preview of a secret for logging.
func masked(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", 4) + fmt.Sprintf("(%d chars)", len(s))
}

// pickMinimaxKey returns the active MiniMax API key plus its human label.
// `unhealthy` is an optional slice of MiniMax key indices (1 or 2) marked
// unhealthy by recent failures; nil means both are healthy. The picker
// prefers key1 when both are healthy; otherwise it picks the next healthy
// candidate; if both are unhealthy it returns key1 as the last-known-good
// fallback so the operator sees a clear 401 instead of a confusing 503.
func pickMinimaxKey(keys keyBundle, unhealthy []int) (string, string) {
	bad := make(map[int]bool, len(unhealthy))
	for _, idx := range unhealthy {
		bad[idx] = true
	}
	if !bad[1] {
		return keys.minimax1, "minimax-api-1"
	}
	if !bad[2] {
		return keys.minimax2, "minimax-api-2"
	}
	// Both unhealthy; surface key1 so a 401 is observed.
	return keys.minimax1, "minimax-api-1"
}

// buildBackendsWithHealth assembles the LLMBackend slice from the flag and
// attaches the chosen MiniMax key based on the supplied health signal.
// When router URL is provided, the router carries the key and per-backend
// APIURL is bypassed at completion time.
func buildBackendsWithHealth(flag, router string, keys keyBundle, unhealthy []int) []eval.LLMBackend {
	picked, _ := pickMinimaxKey(keys, unhealthy)
	all := eval.DefaultBackends()
	for i := range all {
		switch all[i].Model {
		case "qwen3.7-plus", "qwen3.7-max":
			all[i].APIKey = keys.aliyun
		case "MiniMax-M3":
			all[i].APIKey = picked
		}
		if router != "" {
			all[i].Router = router
		}
	}
	if flag == "" || flag == "all" {
		return all
	}
	names := strings.Split(flag, ",")
	var out []eval.LLMBackend
	for _, name := range names {
		name = strings.TrimSpace(name)
		for _, b := range all {
			if b.Name == name || b.Model == name {
				out = append(out, b)
			}
		}
	}
	return out
}

// buildBackends is the convenience wrapper used by main(); assumes both
// MiniMax keys are healthy and delegates to buildBackendsWithHealth.
func buildBackends(flag, router string, keys keyBundle) []eval.LLMBackend {
	return buildBackendsWithHealth(flag, router, keys, nil)
}

// buildTasks returns the task suite filtered by the tasks flag.
func buildTasks(flag string) []eval.Task {
	all := eval.DefaultTaskSuite()
	if flag == "" || flag == "all" {
		return all
	}
	ids := strings.Split(flag, ",")
	var out []eval.Task
	for _, id := range ids {
		id = strings.TrimSpace(id)
		for _, t := range all {
			if string(t.Type) == id || t.ID == id {
				out = append(out, t)
			}
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}

// findBackend looks up a backend by name in a slice.
func findBackend(backends []eval.LLMBackend, name string) (eval.LLMBackend, bool) {
	for _, b := range backends {
		if b.Name == name || b.Model == name {
			return b, true
		}
	}
	return eval.LLMBackend{}, false
}
