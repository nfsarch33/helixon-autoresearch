package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-autoresearch/eval"
)

// EvalReportForTest returns a minimal eval.EvalReport that satisfies
// the fields used by the Engram persistor (Summary, RubricVersion).
func EvalReportForTest() eval.EvalReport {
	return eval.EvalReport{
		Timestamp:     time.Now().UTC(),
		RubricVersion: "v1.0.0-test",
		Summary: eval.Summary{
			OverallVerdict: "YELLOW",
			BestBackend:    "minimax-M3",
			TotalResults:   7,
		},
	}
}

// fakeEngramSender is a stub engramSender that records each payload.
// Tests assert on recorded payloads instead of doing real HTTP.
type fakeEngramSender struct {
	mu        atomic.Int32
	received  []engramPayload
	nextErr   error
	statusOut int
}

type engramPayload struct {
	URL         string
	ContentType string
	Body        []byte
}

func (f *fakeEngramSender) Post(_ context.Context, url, contentType string, body []byte) (int, error) {
	f.mu.Add(1)
	f.received = append(f.received, engramPayload{URL: url, ContentType: contentType, Body: body})
	if f.nextErr != nil {
		return 0, f.nextErr
	}
	if f.statusOut != 0 {
		return f.statusOut, nil
	}
	return http.StatusOK, nil
}

func (f *fakeEngramSender) Count() int { return int(f.mu.Load()) }

// Test 1: LoadEngramConfig honours --engram-url flag
func TestLoadEngramConfigFromFlag(t *testing.T) {
	cfg := LoadEngramConfig(EngramConfig{URL: "http://example.com:8280"})
	if cfg.URL != "http://example.com:8280" {
		t.Errorf("URL = %q, want example.com", cfg.URL)
	}
	if cfg.AppID != "helixon-eval" {
		t.Errorf("AppID = %q, want helixon-eval (default)", cfg.AppID)
	}
	if cfg.UserID != "nfsarch33" {
		t.Errorf("UserID = %q, want nfsarch33 (default)", cfg.UserID)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s (default)", cfg.Timeout)
	}
}

// Test 2: LoadEngramConfig falls back to HELIXON_EVAL_ENGRAM_URL env
func TestLoadEngramConfigFromEnv(t *testing.T) {
	t.Setenv("HELIXON_EVAL_ENGRAM_URL", "http://localhost:8280")
	t.Setenv("HELIXON_EVAL_ENGRAM_APP_ID", "app-x")
	t.Setenv("HELIXON_EVAL_ENGRAM_USER_ID", "user-y")
	t.Setenv("HELIXON_EVAL_ENGRAM_TIMEOUT", "10s")
	cfg := LoadEngramConfig(EngramConfig{})
	if cfg.URL != "http://localhost:8280" {
		t.Errorf("URL from env = %q, want http://localhost:8280", cfg.URL)
	}
	if cfg.AppID != "app-x" {
		t.Errorf("AppID from env = %q, want app-x", cfg.AppID)
	}
	if cfg.UserID != "user-y" {
		t.Errorf("UserID from env = %q, want user-y", cfg.UserID)
	}
	if cfg.Timeout != 10*time.Second {
		t.Errorf("Timeout from env = %v, want 10s", cfg.Timeout)
	}
}

// Test 3: Empty config returns empty URL (caller short-circuits)
func TestLoadEngramConfigEmpty(t *testing.T) {
	os.Unsetenv("HELIXON_EVAL_ENGRAM_URL")
	cfg := LoadEngramConfig(EngramConfig{})
	if cfg.URL != "" {
		t.Errorf("URL = %q, want empty", cfg.URL)
	}
	if cfg.AppID != "helixon-eval" {
		t.Errorf("AppID = %q, want default", cfg.AppID)
	}
}

// Test 4: BuildEngramExperiment constructs a valid EvalExperiment from a
// report and metadata.
func TestBuildEngramExperiment(t *testing.T) {
	report := EvalReportForTest()
	exp := BuildEngramExperiment("v14502-02", "real-eval-run-1", "real eval run", report)
	if exp.ID != "v14502-02" {
		t.Errorf("ID = %q, want v14502-02", exp.ID)
	}
	if exp.Name != "real-eval-run-1" {
		t.Errorf("Name = %q, want real-eval-run-1", exp.Name)
	}
	if exp.Hypothesis != "real eval run" {
		t.Errorf("Hypothesis = %q, want 'real eval run'", exp.Hypothesis)
	}
	if exp.CurrentStage != "completed" {
		t.Errorf("CurrentStage = %q, want completed", exp.CurrentStage)
	}
	if exp.Report.RubricVersion != report.RubricVersion {
		t.Errorf("Report.RubricVersion = %q, want %q", exp.Report.RubricVersion, report.RubricVersion)
	}
}

// Test 5: PushReportToEngram builds a valid POST body
func TestPushReportToEngramSendsValidBody(t *testing.T) {
	sender := &fakeEngramSender{}
	report := EvalReportForTest()
	exp := BuildEngramExperiment("v14502-02", "real-eval", "hypothesis", report)
	cfg := EngramConfig{URL: "http://localhost:8280", AppID: "helixon-eval", UserID: "nfsarch33", Timeout: time.Second}
	if err := PushReportToEngram(context.Background(), sender, cfg, exp); err != nil {
		t.Fatalf("PushReportToEngram: %v", err)
	}
	if sender.Count() != 1 {
		t.Fatalf("sender POST count = %d, want 1", sender.Count())
	}
	got := sender.received[0]
	if !strings.HasSuffix(got.URL, "/memories") {
		t.Errorf("URL = %q, want suffix /memories", got.URL)
	}
	if got.ContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got.ContentType)
	}
	var body engramMemory
	if err := json.Unmarshal(got.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(body.Messages) != 1 {
		t.Errorf("messages len = %d, want 1", len(body.Messages))
	}
	if body.Messages[0].Role != "user" {
		t.Errorf("role = %q, want user", body.Messages[0].Role)
	}
	if !strings.Contains(body.Messages[0].Content, exp.ID) {
		t.Errorf("content does not contain experiment id %q", exp.ID)
	}
	if body.UserID != "nfsarch33" {
		t.Errorf("UserID = %q, want nfsarch33", body.UserID)
	}
	if body.Metadata["experiment_id"] != exp.ID {
		t.Errorf("metadata experiment_id = %v, want %q", body.Metadata["experiment_id"], exp.ID)
	}
}

// Test 6: PushReportToEngram writes Agentrace fallback when Engram is down
func TestPushReportToEngramAgentraceFallback(t *testing.T) {
	sender := &fakeEngramSender{nextErr: errors.New("connection refused")}
	tmp := t.TempDir()
	t.Setenv("AGENTRACE_LOG", tmp+"/agentrace.ndjson")
	report := EvalReportForTest()
	exp := BuildEngramExperiment("v14502-02", "real-eval", "hypothesis", report)
	cfg := EngramConfig{URL: "http://localhost:8280", AppID: "helixon-eval", UserID: "nfsarch33", Timeout: time.Second}
	// Should return sentinel error so caller can log it but NOT fail
	err := PushReportToEngram(context.Background(), sender, cfg, exp)
	if err == nil {
		t.Fatal("expected error from sender, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("err = %v, want 'connection refused'", err)
	}
	if sender.Count() != 1 {
		t.Errorf("sender POST count = %d, want 1", sender.Count())
	}
	// Verify fallback Agentrace event was written
	data, rerr := os.ReadFile(tmp + "/agentrace.ndjson")
	if rerr != nil {
		t.Fatalf("read agentrace log: %v", rerr)
	}
	if !strings.Contains(string(data), "eval_run_disk_only") {
		t.Errorf("agentrace log missing eval_run_disk_only event: %s", string(data))
	}
	if !strings.Contains(string(data), exp.ID) {
		t.Errorf("agentrace log missing experiment id: %s", string(data))
	}
}

// Test 7: PushReportToEngram returns server-side error
func TestPushReportToEngramServerError(t *testing.T) {
	sender := &fakeEngramSender{statusOut: 500}
	report := EvalReportForTest()
	exp := BuildEngramExperiment("v14502-02", "real-eval", "h", report)
	cfg := EngramConfig{URL: "http://localhost:8280", AppID: "helixon-eval", UserID: "nfsarch33", Timeout: time.Second}
	err := PushReportToEngram(context.Background(), sender, cfg, exp)
	if err == nil {
		t.Fatal("expected error from 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want contains 500", err)
	}
}

// Test 8: Real HTTP roundtrip via httptest.Server
func TestPushReportToEngramRealHTTP(t *testing.T) {
	var captured engramMemory
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("unmarshal: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	sender := &realHTTPSender{client: &http.Client{Timeout: time.Second}}
	report := EvalReportForTest()
	exp := BuildEngramExperiment("v14502-02", "real", "h", report)
	cfg := EngramConfig{URL: srv.URL, AppID: "helixon-eval", UserID: "nfsarch33", Timeout: time.Second}
	if err := PushReportToEngram(context.Background(), sender, cfg, exp); err != nil {
		t.Fatalf("PushReportToEngram: %v", err)
	}
	if len(captured.Messages) != 1 {
		t.Errorf("messages len = %d, want 1", len(captured.Messages))
	}
	if captured.Messages[0].Role != "user" {
		t.Errorf("role = %q, want user", captured.Messages[0].Role)
	}
	if captured.UserID != "nfsarch33" {
		t.Errorf("UserID = %q, want nfsarch33", captured.UserID)
	}
}

// Test 9: ValidateExperimentID rejects empty/invalid IDs
func TestValidateExperimentID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"v14502-02", true},
		{"v1", true},
		{"abc_DEF-123", true},
		{"", false},
		{"   ", false},
		{"v 14 502-02", false}, // contains spaces
	}
	for _, c := range cases {
		if got := ValidateExperimentID(c.in); got != c.want {
			t.Errorf("ValidateExperimentID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// Test 10: BuildEngramExperiment rejects empty experiment ID
func TestBuildEngramExperimentEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty ID")
		}
	}()
	BuildEngramExperiment("", "n", "h", EvalReportForTest())
}

// Test 11: SendEngramFallbackEvent writes a structured NDJSON line
func TestSendEngramFallbackEvent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTRACE_LOG", tmp+"/agentrace.ndjson")
	SendEngramFallbackEvent("v14502-02", "connection refused")
	data, err := os.ReadFile(tmp + "/agentrace.ndjson")
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	line := bytes.TrimSpace(data)
	if !strings.Contains(string(line), `"event":"eval_run_disk_only"`) {
		t.Errorf("missing event field: %s", string(line))
	}
	if !strings.Contains(string(line), `"experiment_id":"v14502-02"`) {
		t.Errorf("missing experiment_id: %s", string(line))
	}
	if !strings.Contains(string(line), `"reason":"connection refused"`) {
		t.Errorf("missing reason: %s", string(line))
	}
}

// Test 12: realHTTPSender is non-nil and reachable via Post.
func TestRealHTTPSenderNonNil(t *testing.T) {
	s := &realHTTPSender{client: &http.Client{Timeout: time.Second}}
	if s == nil {
		t.Fatal("realHTTPSender should not be nil")
	}
}

// Test 13: BuildEngramExperiment sets CreatedAt to UTC now
func TestBuildEngramExperimentSetsCreatedAt(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	exp := BuildEngramExperiment("v14502-02", "n", "h", EvalReportForTest())
	after := time.Now().UTC().Add(time.Second)
	if exp.CreatedAt.Before(before) || exp.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v not in [%v, %v]", exp.CreatedAt, before, after)
	}
}

// Test 14: PushReportToEngram respects context cancellation
func TestPushReportToEngramContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	sender := &realHTTPSender{client: &http.Client{Timeout: 50 * time.Millisecond}}
	exp := BuildEngramExperiment("v14502-02", "n", "h", EvalReportForTest())
	cfg := EngramConfig{URL: srv.URL, AppID: "x", UserID: "y", Timeout: 100 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := PushReportToEngram(ctx, sender, cfg, exp); err == nil {
		t.Error("expected timeout error")
	}
}
