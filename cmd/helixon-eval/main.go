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
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-autoresearch/eval"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	backendsFlag := flag.String("backends", "all", "comma-separated backend names or 'all'")
	tasksFlag := flag.String("tasks", "all", "comma-separated task type ids or 'all'")
	judgeFlag := flag.String("judge", "", "backend name to use as G-Eval judge (defaults to none of the candidates; use a distinct backend to avoid self-preference)")
	routerFlag := flag.String("router", "", "optional llm-cluster-router base URL; if set, all backends route through it")
	outFlag := flag.String("out", "", "path to write the JSON report (default: stdout)")
	timeoutFlag := flag.Duration("timeout", 20*time.Minute, "total run timeout")
	flag.Parse()

	// Resolve API keys from 1Password. These MUST never be hardcoded.
	keys, err := resolveKeys(logger)
	if err != nil {
		logger.Error("failed to resolve API keys from 1Password", "err", err)
		os.Exit(1)
	}

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
}

// keyBundle holds API keys resolved from 1Password.
type keyBundle struct {
	aliyun  string
	minimax string
}

// resolveKeys reads all credentials from 1Password via `op read`.
// Keys are never written to disk or logged.
func resolveKeys(logger *slog.Logger) (keyBundle, error) {
	aliyun, err := opRead("op://Cursor_IronClaw/Aliyun Team Qwen Token Plan Key/password")
	if err != nil {
		return keyBundle{}, fmt.Errorf("aliyun key: %w", err)
	}
	minimax, err := opRead("op://Cursor_IronClaw/minimax-api-1/api-key")
	if err != nil {
		return keyBundle{}, fmt.Errorf("minimax key: %w", err)
	}
	logger.Info("resolved API keys from 1Password", "aliyun", masked(aliyun), "minimax", masked(minimax))
	return keyBundle{aliyun: aliyun, minimax: minimax}, nil
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

// buildBackends assembles the LLMBackend slice from the flag. When a
// router URL is provided, the router carries the key and per-backend
// APIURL is bypassed at completion time.
func buildBackends(flag, router string, keys keyBundle) []eval.LLMBackend {
	all := eval.DefaultBackends()
	// Attach keys.
	for i := range all {
		switch all[i].Model {
		case "qwen3.7-plus", "qwen3.7-max":
			all[i].APIKey = keys.aliyun
		case "MiniMax-M3":
			all[i].APIKey = keys.minimax
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
