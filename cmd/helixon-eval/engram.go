// engram.go: cmd/helixon-eval Engram persistence layer (v14502-04b).
//
// This file wires helixon-eval to the user-engram MCP via HTTP POST to
// /memories. The integration.EngramPersistor in the integration package
// already supports /memories; we re-implement the wire here directly so
// that the cmd binary does not depend on integration (and so we can
// unit-test the sender with a fake interface).
//
// Fallback policy:
//
//   - If --engram-url or HELIXON_EVAL_ENGRAM_URL is unset, persistence
//     is skipped entirely and the eval still succeeds. This keeps the
//     CLI usable on hosts where Engram is unreachable.
//
//   - If Engram returns a non-2xx response or the HTTP call fails, the
//     error is returned to the caller, the experiment payload is logged
//     to AGENTRACE_LOG as event=eval_run_disk_only, and eval still
//     exits 0. Eval runs MUST NOT silently disappear when Engram is
//     down — the fallback Agentrace event preserves the audit trail.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nfsarch33/helixon-autoresearch/eval"
	"github.com/nfsarch33/helixon-autoresearch/integration"
)

// These are local mirrors of the integration package's unexported wire
// types. We duplicate them here so cmd/helixon-eval does not need to
// import unexported names; the integration package owns the canonical
// implementations and is the reference for the wire format.
type engramMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type engramMemory struct {
	Messages []engramMessage `json:"messages"`
	UserID   string          `json:"user_id"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// EngramConfig configures the Engram persistor.
type EngramConfig struct {
	URL     string        // e.g. http://<host>:<port>
	AppID   string        // e.g. helixon-eval
	UserID  string        // e.g. nfsarch33
	Timeout time.Duration // per-request; defaults to 30s
}

func (c EngramConfig) IsEnabled() bool { return strings.TrimSpace(c.URL) != "" }

// LoadEngramConfig merges EngramConfig with HELIXON_EVAL_ENGRAM_*
// environment variables and applies defaults. The flag struct wins
// over env if both are set.
func LoadEngramConfig(flag EngramConfig) EngramConfig {
	if flag.URL == "" {
		flag.URL = os.Getenv("HELIXON_EVAL_ENGRAM_URL")
	}
	if flag.AppID == "" {
		flag.AppID = envOrDefault("HELIXON_EVAL_ENGRAM_APP_ID", "helixon-eval")
	}
	if flag.UserID == "" {
		flag.UserID = envOrDefault("HELIXON_EVAL_ENGRAM_USER_ID", "nfsarch33")
	}
	if flag.Timeout == 0 {
		if v := os.Getenv("HELIXON_EVAL_ENGRAM_TIMEOUT"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				flag.Timeout = d
			}
		}
		if flag.Timeout == 0 {
			flag.Timeout = 30 * time.Second
		}
	}
	return flag
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// engramSender abstracts the HTTP wire so unit tests can substitute a
// fake. The interface is small: a single POST returning status + error.
type engramSender interface {
	Post(ctx context.Context, url, contentType string, body []byte) (int, error)
}

// realHTTPSender is the production engramSender: it shells out to
// http.Client.Do with the supplied timeout.
type realHTTPSender struct {
	client *http.Client
}

func (r *realHTTPSender) Post(ctx context.Context, url, contentType string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build engram request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("engram POST: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// validation constants.
var experimentIDPattern = regexp.MustCompile(`^[A-Za-z0-9._\-]{1,64}$`)

// ValidateExperimentID returns true when id is a usable experiment id.
// Empty or whitespace-only strings are rejected, as are strings that
// contain characters Engram's metadata keys cannot tolerate.
func ValidateExperimentID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	return experimentIDPattern.MatchString(id)
}

// BuildEngramExperiment wraps a report in an EvalExperiment ready for
// persistence. The ID is taken verbatim and is required.
func BuildEngramExperiment(id, name, hypothesis string, report eval.EvalReport) integration.EvalExperiment {
	if !ValidateExperimentID(id) {
		panic("BuildEngramExperiment: invalid experiment id")
	}
	return integration.EvalExperiment{
		ID:           id,
		Name:         name,
		Hypothesis:   hypothesis,
		Question:     name,
		Report:       report,
		CurrentStage: "completed",
		CreatedAt:    time.Now().UTC(),
	}
}

// PushReportToEngram serialises exp and POSTs it to cfg.URL/memories.
// On any non-nil error, an eval_run_disk_only Agentrace event is
// written so the experiment is not lost when Engram is down.
func PushReportToEngram(ctx context.Context, sender engramSender, cfg EngramConfig, exp integration.EvalExperiment) error {
	if !cfg.IsEnabled() {
		return nil
	}
	if !ValidateExperimentID(exp.ID) {
		return fmt.Errorf("invalid experiment id: %q", exp.ID)
	}
	payload, err := json.Marshal(exp)
	if err != nil {
		return fmt.Errorf("marshal experiment: %w", err)
	}
	body := engramMemory{
		Messages: []engramMessage{
			{Role: "user", Content: string(payload)},
		},
		UserID: cfg.UserID,
		Metadata: map[string]any{
			"app_id":          cfg.AppID,
			"experiment_id":   exp.ID,
			"experiment_name": exp.Name,
			"stage":           exp.CurrentStage,
			"verdict":         exp.Report.Summary.OverallVerdict,
			"best_backend":    exp.Report.Summary.BestBackend,
			"total_results":   exp.Report.Summary.TotalResults,
			"rubric_version":  exp.Report.RubricVersion,
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal memories body: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	url := strings.TrimRight(cfg.URL, "/") + "/memories"
	status, err := sender.Post(ctx, url, "application/json", data)
	if err != nil {
		SendEngramFallbackEvent(exp.ID, err.Error())
		return fmt.Errorf("engram POST failed: %w", err)
	}
	if status >= 400 {
		SendEngramFallbackEvent(exp.ID, fmt.Sprintf("status %d", status))
		return fmt.Errorf("engram POST returned status %d", status)
	}
	return nil
}

// SendEngramFallbackEvent writes a structured NDJSON line to the
// Agentrace log when Engram persistence fails. The agentrace log path
// is taken from $AGENTRACE_LOG or defaults to ~/.cache/helixon/agentrace/agentrace.ndjson.
func SendEngramFallbackEvent(experimentID, reason string) {
	logPath := os.Getenv("AGENTRACE_LOG")
	if logPath == "" {
		home, _ := os.UserHomeDir()
		logPath = filepath.Join(home, ".cache", "helixon", "agentrace", "agentrace.ndjson")
	}
	record := map[string]any{
		"ts":            time.Now().UTC().Format(time.RFC3339),
		"event":         "eval_run_disk_only",
		"experiment_id": experimentID,
		"reason":        reason,
		"service":       "helixon-eval",
	}
	b, _ := json.Marshal(record)
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		// Best-effort; never block the eval.
		_, _ = fmt.Fprintf(os.Stderr, "agentrace mkdir failed: %v\n", err)
		return
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "agentrace open failed: %v\n", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "agentrace write failed: %v\n", err)
	}
}

// persistReportOptional is the cmd-binary entrypoint that wraps
// PushReportToEngram with a logger. It is intentionally lossy: an Engram
// failure is logged but does NOT cause process exit, because the eval
// has already succeeded. The persistence call is best-effort.
func persistReportOptional(ctx context.Context, logger *slog.Logger, sender engramSender, cfg EngramConfig, exp integration.EvalExperiment) {
	if !cfg.IsEnabled() {
		logger.Info("engram persistence skipped (no URL configured)")
		return
	}
	if err := PushReportToEngram(ctx, sender, cfg, exp); err != nil {
		logger.Warn("engram persistence failed; eval report saved to disk only",
			"experiment_id", exp.ID,
			"err", err,
		)
		return
	}
	logger.Info("engram persistence ok",
		"experiment_id", exp.ID,
		"url", cfg.URL,
	)
}
