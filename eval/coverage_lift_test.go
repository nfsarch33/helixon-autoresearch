package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewEvalHarness_Defaults ensures the constructor wires defaults.
func TestNewEvalHarness_Defaults(t *testing.T) {
	backends := []LLMBackend{{Name: "test", Model: "m", APIKey: "k"}}
	h := NewEvalHarness(backends)
	if h == nil {
		t.Fatalf("expected non-nil harness")
	}
	if len(h.Backends) != 1 {
		t.Errorf("backends len: got %d", len(h.Backends))
	}
	if len(h.Tasks) == 0 {
		t.Errorf("expected default tasks")
	}
	if h.Rubrics == nil {
		t.Errorf("expected rubrics")
	}
	if h.Logger == nil {
		t.Errorf("expected default logger")
	}
}

// TestEvalHarness_WithMethods covers the builder methods.
func TestEvalHarness_WithMethods(t *testing.T) {
	h := NewEvalHarness(nil)
	j := LLMBackend{Name: "judge", Model: "jm"}
	tl := slog.New(slog.NewTextHandler(io.Discard, nil))
	tasks := []Task{{ID: "t1", Type: TaskCodeGeneration, Difficulty: DiffMedium, Name: "T1"}}
	got := h.WithJudge(j).WithLogger(tl).WithTasks(tasks)
	if got.Judge.Name != "judge" {
		t.Errorf("WithJudge: got %q", got.Judge.Name)
	}
	if got.Logger != tl {
		t.Errorf("WithLogger: did not set logger")
	}
	if len(got.Tasks) != 1 {
		t.Errorf("WithTasks: got %d", len(got.Tasks))
	}
}

// TestEvalHarness_WithLogger_NilIgnored covers the nil-guard.
func TestEvalHarness_WithLogger_NilIgnored(t *testing.T) {
	h := NewEvalHarness(nil)
	original := h.Logger
	h.WithLogger(nil)
	if h.Logger != original {
		t.Errorf("nil logger should not replace existing")
	}
}

// TestTaskByID_Lift covers found and not-found (gap coverage).
func TestTaskByID_Lift(t *testing.T) {
	suite := DefaultTaskSuite()
	task, ok := TaskByID(suite, "cg-001")
	if !ok {
		t.Errorf("expected to find cg-001")
	}
	if task.Type != TaskCodeGeneration {
		t.Errorf("got %s, want code_generation", task.Type)
	}
	_, ok = TaskByID(suite, "nonexistent")
	if ok {
		t.Errorf("expected not-found")
	}
}

// TestTasksByType_Lift covers the filter path.
func TestTasksByType_Lift(t *testing.T) {
	suite := DefaultTaskSuite()
	filtered := TasksByType(suite, TaskCodeGeneration)
	if len(filtered) != 1 {
		t.Errorf("expected 1 code_generation task, got %d", len(filtered))
	}
	empty := TasksByType(suite, TaskTypeID("unknown"))
	if len(empty) != 0 {
		t.Errorf("expected 0 for unknown type, got %d", len(empty))
	}
}

// TestAllTaskTypes_StableOrder_Lift ensures the canonical order is preserved.
func TestAllTaskTypes_StableOrder_Lift(t *testing.T) {
	all := AllTaskTypes()
	if len(all) != 7 {
		t.Errorf("expected 7 task types, got %d", len(all))
	}
	if all[0] != TaskCodeGeneration || all[6] != TaskMultiStepPlan {
		t.Errorf("unexpected ordering: first=%s last=%s", all[0], all[6])
	}
}

// TestCountUniqueTasks_Lift covers the helper.
func TestCountUniqueTasks_Lift(t *testing.T) {
	results := []TaskResult{
		{TaskID: "a"},
		{TaskID: "b"},
		{TaskID: "a"}, // duplicate
	}
	got := countUniqueTasks(results)
	if got != 2 {
		t.Errorf("countUniqueTasks: got %d, want 2", got)
	}
}

// TestParseJudgeResponse_BadJSON_Lift covers the failure path.
func TestParseJudgeResponse_BadJSON_Lift(t *testing.T) {
	_, err := parseJudgeResponse("not json")
	if err == nil {
		t.Errorf("expected JSON error")
	}
}

// chatResponseFixture returns a canned OpenAI-compatible chat response.
func chatResponseFixture(text string) string {
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":%q}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, text)
}

// mustJSON returns the JSON-encoded form of v as a string.
func mustJSON(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestComplete_Success covers the happy-path HTTP call.
func TestComplete_Success(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatResponseFixture("hello world")))
	}))
	defer srv.Close()

	backend := LLMBackend{Name: "test", Model: "m", APIURL: srv.URL, APIKey: "k"}
	text, usage, err := backend.Complete(context.Background(), "test prompt", 100)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "hello world" {
		t.Errorf("text: got %q", text)
	}
	if usage.Total != 15 {
		t.Errorf("usage.Total: got %d", usage.Total)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected 1 HTTP call")
	}
}

// TestComplete_ViaRouter covers the Router-URL path.
func TestComplete_ViaRouter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			t.Errorf("expected /v1/chat/completions path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatResponseFixture("via router")))
	}))
	defer srv.Close()

	backend := LLMBackend{Name: "test", Model: "m", Router: srv.URL, APIKey: "k"}
	text, _, err := backend.Complete(context.Background(), "test prompt", 100)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "via router" {
		t.Errorf("text: got %q", text)
	}
}

// TestComplete_ServerError covers the 4xx response path.
func TestComplete_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "backend exploded", http.StatusInternalServerError)
	}))
	defer srv.Close()

	backend := LLMBackend{Name: "test", Model: "m", APIURL: srv.URL, APIKey: "k"}
	_, _, err := backend.Complete(context.Background(), "test prompt", 100)
	if err == nil {
		t.Fatalf("expected error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got %v", err)
	}
}

// TestComplete_NoChoices covers the empty-choices path.
func TestComplete_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	backend := LLMBackend{Name: "test", Model: "m", APIURL: srv.URL, APIKey: "k"}
	_, _, err := backend.Complete(context.Background(), "test prompt", 100)
	if err == nil {
		t.Fatalf("expected error on empty choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("expected 'no choices', got %v", err)
	}
}

// TestComplete_ContextCancel covers the cancelled-context path.
func TestComplete_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(chatResponseFixture("late")))
	}))
	defer srv.Close()

	backend := LLMBackend{Name: "test", Model: "m", APIURL: srv.URL, APIKey: "k"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, err := backend.Complete(ctx, "test prompt", 100)
	if err == nil {
		t.Errorf("expected context-cancel error")
	}
}

// TestEvalHarness_Run_HappyPath exercises the full Run via httptest backends.
func TestEvalHarness_Run_HappyPath(t *testing.T) {
	// Agent backend returns a canned response.
	agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatResponseFixture("agent did the thing")))
	}))
	defer agentSrv.Close()

	// Judge backend returns a valid scoring JSON.
	judgeContent := `{"task_completion":{"value":4,"rationale":"ok"},"code_quality":{"value":4,"rationale":"ok"},"token_efficiency":{"value":3,"rationale":"ok"},"context_utilization":{"value":4,"rationale":"ok"},"self_improvement":{"value":3,"rationale":"ok"},"long_session_stability":{"value":4,"rationale":"ok"},"error_recovery":{"value":4,"rationale":"ok"}}`
	judgeOuter := `{"choices":[{"message":{"role":"assistant","content":` + mustJSON(judgeContent) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	judgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(judgeOuter))
	}))
	defer judgeSrv.Close()

	backends := []LLMBackend{
		{Name: "agent-a", Model: "ma", APIURL: agentSrv.URL, APIKey: "k"},
	}
	tasks := []Task{
		{ID: "t1", Type: TaskCodeGeneration, Difficulty: DiffMedium, Name: "T1", Prompt: "do it"},
	}
	judge := LLMBackend{Name: "judge", Model: "jm", APIURL: judgeSrv.URL, APIKey: "k"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewEvalHarness(backends).WithTasks(tasks).WithJudge(judge).WithLogger(logger)

	report, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report == nil {
		t.Fatalf("expected report")
	}
	if len(report.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(report.Results))
	}
	if report.Results[0].Verdict == "" {
		t.Errorf("expected non-empty verdict")
	}
	if report.Results[0].Error != "" {
		t.Logf("judge error path was taken: %s", report.Results[0].Error)
	}
	if report.Results[0].WeightedScore <= 0 {
		t.Errorf("expected positive weighted score, got %v (judge_err=%q)", report.Results[0].WeightedScore, report.Results[0].Error)
	}
}

// TestEvalHarness_Run_AgentError exercises the agent-failure path.
func TestEvalHarness_Run_AgentError(t *testing.T) {
	agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer agentSrv.Close()
	judgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(chatResponseFixture("unused")))
	}))
	defer judgeSrv.Close()

	backends := []LLMBackend{{Name: "agent-a", Model: "ma", APIURL: agentSrv.URL, APIKey: "k"}}
	tasks := []Task{{ID: "t1", Type: TaskCodeGeneration, Difficulty: DiffMedium, Name: "T1"}}
	judge := LLMBackend{Name: "judge", Model: "jm", APIURL: judgeSrv.URL, APIKey: "k"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewEvalHarness(backends).WithTasks(tasks).WithJudge(judge).WithLogger(logger)
	report, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Results[0].Verdict != "RED" {
		t.Errorf("expected RED verdict on agent error, got %q", report.Results[0].Verdict)
	}
	if report.Results[0].Error == "" {
		t.Errorf("expected error message on agent failure")
	}
}

// TestDefaultBackends covers the helper.
func TestDefaultBackends_Lift(t *testing.T) {
	bs := DefaultBackends()
	if len(bs) != 3 {
		t.Errorf("expected 3 default backends, got %d", len(bs))
	}
	for _, b := range bs {
		if b.Name == "" {
			t.Errorf("default backend has no name")
		}
	}
}

// TestBuildSummary_Basics covers the summary builder with mixed scores.
func TestBuildSummary_Basics(t *testing.T) {
	results := []TaskResult{
		{TaskID: "t1", Backend: "b1", CriterionScores: map[CriterionID]Score{CritTaskCompletion: {Value: 5}}},
		{TaskID: "t1", Backend: "b2", CriterionScores: map[CriterionID]Score{CritTaskCompletion: {Value: 2}}},
	}
	backends := []LLMBackend{{Name: "b1"}, {Name: "b2"}}
	s := buildSummary(results, backends)
	if s.BestBackend != "b1" {
		t.Errorf("best backend: got %q", s.BestBackend)
	}
	if len(s.BackendRanking) != 2 {
		t.Errorf("expected 2 in ranking, got %d", len(s.BackendRanking))
	}
}

// TestBuildSummary_TieBreak covers the equal-score tie-break branch.
func TestBuildSummary_TieBreak(t *testing.T) {
	results := []TaskResult{
		{TaskID: "t1", Backend: "b1", CriterionScores: map[CriterionID]Score{CritTaskCompletion: {Value: 3}}},
		{TaskID: "t1", Backend: "b2", CriterionScores: map[CriterionID]Score{CritTaskCompletion: {Value: 3}}},
	}
	backends := []LLMBackend{{Name: "b1"}, {Name: "b2"}}
	s := buildSummary(results, backends)
	// With equal scores either can be best; just verify we got a non-empty result.
	if s.BestBackend == "" {
		t.Errorf("expected non-empty best backend")
	}
}

// TestRenderText_FullReport exercises RenderText with a populated report.
func TestRenderText_FullReport(t *testing.T) {
	rep := &EvalReport{
		RubricVersion: RubricVersion,
		Summary:       Summary{OverallVerdict: "YELLOW", BestBackend: "b1", BackendRanking: []string{"b1", "b2"}, TotalResults: 2},
		Matrix: ScoreMatrix{
			ByBackend: map[string]*BackendScore{
				"b1": {Backend: "b1", Model: "m1", AvgWeighted: 60, Verdict: "YELLOW"},
				"b2": {Backend: "b2", Model: "m2", AvgWeighted: 30, Verdict: "RED"},
			},
			ByTask: map[string]*TaskScore{
				"t1": {TaskID: "t1", BestBackend: "b1", BackendScores: map[string]float64{"b1": 60, "b2": 30}},
			},
		},
		Results: []TaskResult{{TaskID: "t1"}, {TaskID: "t1"}},
	}
	out := rep.RenderText()
	for _, want := range []string{"YELLOW", "b1", "b2", "60", "30"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}

// TestZeroScores_Lift covers the all-zero fallback with a rubric.
func TestZeroScores_Lift(t *testing.T) {
	rubric := DefaultRubrics()[TaskCodeGeneration]
	zs := zeroScores(rubric)
	if len(zs) != len(rubric.Criteria) {
		t.Errorf("expected one zero score per criterion, got %d vs %d", len(zs), len(rubric.Criteria))
	}
	for _, v := range zs {
		if v.Value != 1 {
			t.Errorf("expected floor value 1, got %d", v.Value)
		}
		if !strings.Contains(v.Rationale, "floor") {
			t.Errorf("expected 'floor' in rationale, got %q", v.Rationale)
		}
	}
}

// TestTruncate_Lift covers the helper.
func TestTruncate_Lift(t *testing.T) {
	if got := truncate("abc", 10); got != "abc" {
		t.Errorf("short: got %q", got)
	}
	if got := truncate("abcdefghij", 5); got != "abcde..." {
		t.Errorf("long: got %q", got)
	}
	if got := truncate("", 5); got != "" {
		t.Errorf("empty: got %q", got)
	}
}

// TestBuildJudgePrompt_Lift covers the judge-prompt builder.
func TestBuildJudgePrompt_Lift(t *testing.T) {
	rubric := DefaultRubrics()[TaskCodeGeneration]
	task := Task{Prompt: "do thing X", ExpectedOutput: "expected"}
	prompt := buildJudgePrompt(task, rubric, "the output")
	if !strings.Contains(prompt, "do thing X") {
		t.Errorf("expected task in prompt")
	}
	if !strings.Contains(prompt, "the output") {
		t.Errorf("expected output in prompt")
	}
	if !strings.Contains(prompt, "task_completion") {
		t.Errorf("expected rubric criterion id in prompt")
	}
}

// TestJudgeScore_HappyPath exercises judgeScore via a Router httptest.
func TestJudgeScore_HappyPath(t *testing.T) {
	judgeContent := `{"task_completion":{"value":3,"rationale":"ok"},"code_quality":{"value":3,"rationale":"ok"},"token_efficiency":{"value":3,"rationale":"ok"},"context_utilization":{"value":3,"rationale":"ok"},"self_improvement":{"value":3,"rationale":"ok"},"long_session_stability":{"value":3,"rationale":"ok"},"error_recovery":{"value":3,"rationale":"ok"}}`
	judgeOuter := `{"choices":[{"message":{"role":"assistant","content":` + mustJSON(judgeContent) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(judgeOuter))
	}))
	defer srv.Close()

	judge := LLMBackend{Name: "judge", Model: "jm", APIURL: srv.URL, APIKey: "k"}
	h := &EvalHarness{Judge: judge}
	rubric := DefaultRubrics()[TaskCodeGeneration]
	task := Task{ID: "t1", Type: TaskCodeGeneration, Difficulty: DiffMedium, Prompt: "do it"}
	scores, err := h.judgeScore(context.Background(), task, rubric, "agent output")
	if err != nil {
		t.Fatalf("judgeScore: %v", err)
	}
	if scores[CritTaskCompletion].Value != 3 {
		t.Errorf("expected 3, got %d", scores[CritTaskCompletion].Value)
	}
}

// TestJudgeScore_BadJSON covers the parse-failure fallback path.
func TestJudgeScore_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(chatResponseFixture("garbage")))
	}))
	defer srv.Close()

	judge := LLMBackend{Name: "judge", Model: "jm", APIURL: srv.URL, APIKey: "k"}
	h := &EvalHarness{Judge: judge}
	rubric := DefaultRubrics()[TaskCodeGeneration]
	task := Task{ID: "t1", Type: TaskCodeGeneration, Difficulty: DiffMedium, Prompt: "do it"}
	_, err := h.judgeScore(context.Background(), task, rubric, "agent output")
	if err == nil {
		t.Errorf("expected error from bad JSON")
	}
}

// TestJudgeScore_CompleterError covers the completer-error path.
func TestJudgeScore_CompleterError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	judge := LLMBackend{Name: "judge", Model: "jm", APIURL: srv.URL, APIKey: "k"}
	h := &EvalHarness{Judge: judge}
	rubric := DefaultRubrics()[TaskCodeGeneration]
	task := Task{ID: "t1", Type: TaskCodeGeneration, Difficulty: DiffMedium, Prompt: "do it"}
	_, err := h.judgeScore(context.Background(), task, rubric, "agent output")
	if err == nil {
		t.Errorf("expected error from completer")
	}
}

// TestRubricFor_Known covers the success branch (gap coverage).
func TestRubricFor_Known_Lift(t *testing.T) {
	r, err := rubricFor(TaskCodeGeneration)
	if err != nil {
		t.Errorf("rubricFor: %v", err)
	}
	if r == nil {
		t.Errorf("expected non-nil rubric")
	}
}

// TestTaskTypeID_String_AllKnown covers all canonical type strings.
func TestTaskTypeID_String_AllKnown_Lift(t *testing.T) {
	for _, id := range AllTaskTypes() {
		if id.String() == "" {
			t.Errorf("empty String() for %s", id)
		}
	}
}
