package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-autoresearch/eval"
	"github.com/nfsarch33/helixon-autoresearch/integration"
)

// stubOpReader is a test-only opReader that returns canned responses.
type stubOpReader struct {
	values map[string]string
	errs   map[string]error
	calls  []string
}

func (s *stubOpReader) Read(ref string) (string, error) {
	s.calls = append(s.calls, ref)
	if err, ok := s.errs[ref]; ok {
		return "", err
	}
	if v, ok := s.values[ref]; ok {
		return v, nil
	}
	return "", errors.New("stub: ref not found: " + ref)
}

// TestResolveKeys_AllSuccess confirms resolveKeys aggregates the three
// 1Password references into a keyBundle.
func TestResolveKeys_AllSuccess(t *testing.T) {
	stub := &stubOpReader{
		values: map[string]string{
			opRefAliyun:   "aliyun-key",
			opRefMinimax1: "minimax-1-key",
			opRefMinimax2: "minimax-2-key",
		},
	}
	bundle, err := resolveKeys(stub)
	if err != nil {
		t.Fatalf("resolveKeys: %v", err)
	}
	if bundle.aliyun != "aliyun-key" {
		t.Errorf("aliyun: got %q", bundle.aliyun)
	}
	if bundle.minimax1 != "minimax-1-key" {
		t.Errorf("minimax1: got %q", bundle.minimax1)
	}
	if bundle.minimax2 != "minimax-2-key" {
		t.Errorf("minimax2: got %q", bundle.minimax2)
	}
	if len(stub.calls) != 3 {
		t.Errorf("expected 3 read calls, got %d", len(stub.calls))
	}
}

// TestResolveKeys_AliyunError covers the aliyun-key read failure branch.
func TestResolveKeys_AliyunError(t *testing.T) {
	stub := &stubOpReader{
		errs: map[string]error{opRefAliyun: errors.New("op timeout")},
	}
	_, err := resolveKeys(stub)
	if err == nil {
		t.Fatalf("expected error from aliyun key read")
	}
	if !strings.Contains(err.Error(), "aliyun key") {
		t.Errorf("expected error to mention 'aliyun key', got %v", err)
	}
}

// TestResolveKeys_Minimax1Error covers the first MiniMax key read failure.
func TestResolveKeys_Minimax1Error(t *testing.T) {
	stub := &stubOpReader{
		values: map[string]string{opRefAliyun: "aliyun"},
		errs:   map[string]error{opRefMinimax1: errors.New("not found")},
	}
	_, err := resolveKeys(stub)
	if err == nil {
		t.Fatalf("expected error from minimax1 read")
	}
	if !strings.Contains(err.Error(), "minimax key1") {
		t.Errorf("expected 'minimax key1' in error, got %v", err)
	}
}

// TestResolveKeys_Minimax2Error covers the second MiniMax key read failure.
func TestResolveKeys_Minimax2Error(t *testing.T) {
	stub := &stubOpReader{
		values: map[string]string{
			opRefAliyun:   "aliyun",
			opRefMinimax1: "m1",
		},
		errs: map[string]error{opRefMinimax2: errors.New("not found")},
	}
	_, err := resolveKeys(stub)
	if err == nil {
		t.Fatalf("expected error from minimax2 read")
	}
	if !strings.Contains(err.Error(), "minimax key2") {
		t.Errorf("expected 'minimax key2' in error, got %v", err)
	}
}

// TestMasked_Short covers the <=4 chars branch.
func TestMasked_Short(t *testing.T) {
	cases := map[string]string{
		"":     "",
		"a":    "*",
		"ab":   "**",
		"abcd": "****",
	}
	for in, want := range cases {
		got := masked(in)
		if got != want {
			t.Errorf("masked(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMasked_Long covers the >4 chars branch.
func TestMasked_Long(t *testing.T) {
	got := masked("sk-1234567890")
	if !strings.HasPrefix(got, "****") {
		t.Errorf("expected prefix '****', got %q", got)
	}
	if !strings.Contains(got, "13 chars") {
		t.Errorf("expected '13 chars' in output, got %q", got)
	}
}

// TestPickMinimaxKey_AllHealthy covers the no-unhealthy path.
func TestPickMinimaxKey_AllHealthy(t *testing.T) {
	keys := keyBundle{aliyun: "a", minimax1: "m1", minimax2: "m2"}
	got, label := pickMinimaxKey(keys, nil)
	if got != "m1" || label != "minimax-api-1" {
		t.Errorf("expected m1/minimax-api-1, got %q/%q", got, label)
	}
}

// TestPickMinimaxKey_Key1Unhealthy covers the key1-down branch.
func TestPickMinimaxKey_Key1Unhealthy(t *testing.T) {
	keys := keyBundle{minimax1: "m1", minimax2: "m2"}
	got, label := pickMinimaxKey(keys, []int{1})
	if got != "m2" || label != "minimax-api-2" {
		t.Errorf("expected m2/minimax-api-2, got %q/%q", got, label)
	}
}

// TestPickMinimaxKey_Key2Unhealthy covers the key2-down branch (key1 picked).
func TestPickMinimaxKey_Key2Unhealthy(t *testing.T) {
	keys := keyBundle{minimax1: "m1", minimax2: "m2"}
	got, label := pickMinimaxKey(keys, []int{2})
	if got != "m1" || label != "minimax-api-1" {
		t.Errorf("expected m1/minimax-api-1, got %q/%q", got, label)
	}
}

// TestPickMinimaxKey_BothUnhealthy covers the all-down fallback.
func TestPickMinimaxKey_BothUnhealthy(t *testing.T) {
	keys := keyBundle{minimax1: "m1", minimax2: "m2"}
	got, label := pickMinimaxKey(keys, []int{1, 2})
	if got != "m1" || label != "minimax-api-1" {
		t.Errorf("expected m1/minimax-api-1 as fallback, got %q/%q", got, label)
	}
}

// TestBuildBackendsWithHealth_AllFlag covers the empty / "all" path.
func TestBuildBackendsWithHealth_AllFlag(t *testing.T) {
	keys := keyBundle{aliyun: "a", minimax1: "m1", minimax2: "m2"}
	got := buildBackendsWithHealth("", "", keys, nil)
	if len(got) != 3 {
		t.Errorf("expected 3 default backends, got %d", len(got))
	}
	// Verify the minimax backend has the picked key.
	for _, b := range got {
		if b.Model == "MiniMax-M3" {
			if b.APIKey != "m1" {
				t.Errorf("expected MiniMax-M3 APIKey=m1, got %q", b.APIKey)
			}
		}
	}
}

// TestBuildBackendsWithHealth_FilterByName covers the "name1,name2" path.
func TestBuildBackendsWithHealth_FilterByName(t *testing.T) {
	keys := keyBundle{aliyun: "a", minimax1: "m1", minimax2: "m2"}
	got := buildBackendsWithHealth("aliyun-qwen3.7-plus", "", keys, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(got))
	}
	if got[0].Name != "aliyun-qwen3.7-plus" {
		t.Errorf("expected aliyun-qwen3.7-plus, got %s", got[0].Name)
	}
	if got[0].APIKey != "a" {
		t.Errorf("expected aliyun APIKey, got %q", got[0].APIKey)
	}
}

// TestBuildBackendsWithHealth_FilterByModel covers filtering by model id.
func TestBuildBackendsWithHealth_FilterByModel(t *testing.T) {
	keys := keyBundle{aliyun: "a", minimax1: "m1", minimax2: "m2"}
	got := buildBackendsWithHealth("MiniMax-M3", "", keys, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(got))
	}
	if got[0].Model != "MiniMax-M3" {
		t.Errorf("expected MiniMax-M3 model, got %s", got[0].Model)
	}
	if got[0].APIKey != "m1" {
		t.Errorf("expected minimax APIKey, got %q", got[0].APIKey)
	}
}

// TestBuildBackendsWithHealth_FilterByMultipleNames covers comma-separated
// names with whitespace tolerance.
func TestBuildBackendsWithHealth_FilterByMultipleNames(t *testing.T) {
	keys := keyBundle{aliyun: "a", minimax1: "m1", minimax2: "m2"}
	got := buildBackendsWithHealth("aliyun-qwen3.7-plus, aliyun-qwen3.7-max", "", keys, nil)
	if len(got) != 2 {
		t.Errorf("expected 2 backends, got %d", len(got))
	}
}

// TestBuildBackendsWithHealth_FilterNoMatch covers the "no match" case.
func TestBuildBackendsWithHealth_FilterNoMatch(t *testing.T) {
	keys := keyBundle{aliyun: "a", minimax1: "m1", minimax2: "m2"}
	got := buildBackendsWithHealth("nonexistent-backend", "", keys, nil)
	if len(got) != 0 {
		t.Errorf("expected 0 backends for no-match, got %d", len(got))
	}
}

// TestBuildBackendsWithHealth_Router covers the router URL propagation.
func TestBuildBackendsWithHealth_Router(t *testing.T) {
	keys := keyBundle{aliyun: "a", minimax1: "m1", minimax2: "m2"}
	got := buildBackendsWithHealth("all", "http://router.local:8000", keys, nil)
	if len(got) != 3 {
		t.Fatalf("expected 3 backends, got %d", len(got))
	}
	for _, b := range got {
		if b.Router != "http://router.local:8000" {
			t.Errorf("expected Router=http://router.local:8000, got %q", b.Router)
		}
	}
}

// TestBuildBackendsWithHealth_UnhealthyKey covers the unhealthy-key path.
func TestBuildBackendsWithHealth_UnhealthyKey(t *testing.T) {
	keys := keyBundle{aliyun: "a", minimax1: "m1", minimax2: "m2"}
	got := buildBackendsWithHealth("MiniMax-M3", "", keys, []int{1})
	if len(got) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(got))
	}
	if got[0].APIKey != "m2" {
		t.Errorf("expected APIKey=m2 (key1 unhealthy), got %q", got[0].APIKey)
	}
}

// TestBuildBackends_CoversWrapper checks the no-args buildBackends path.
func TestBuildBackends_CoversWrapper(t *testing.T) {
	keys := keyBundle{aliyun: "a", minimax1: "m1", minimax2: "m2"}
	got := buildBackends("all", "", keys)
	if len(got) != 3 {
		t.Errorf("expected 3 backends, got %d", len(got))
	}
}

// TestBuildTasks_AllFlag covers the empty / "all" path.
func TestBuildTasks_AllFlag(t *testing.T) {
	for _, flag := range []string{"", "all"} {
		got := buildTasks(flag)
		if len(got) != 7 {
			t.Errorf("flag=%q: expected 7 tasks, got %d", flag, len(got))
		}
	}
}

// TestBuildTasks_FilterByType covers filtering by task type.
func TestBuildTasks_FilterByType(t *testing.T) {
	got := buildTasks("code_generation")
	if len(got) != 1 {
		t.Errorf("expected 1 task for code_generation, got %d", len(got))
	}
	if len(got) > 0 && got[0].Type != "code_generation" {
		t.Errorf("expected code_generation type, got %s", got[0].Type)
	}
}

// TestBuildTasks_FilterByID covers filtering by task ID.
func TestBuildTasks_FilterByID(t *testing.T) {
	got := buildTasks("cg-001")
	if len(got) != 1 {
		t.Errorf("expected 1 task, got %d", len(got))
	}
	if len(got) > 0 && got[0].ID != "cg-001" {
		t.Errorf("expected cg-001, got %s", got[0].ID)
	}
}

// TestBuildTasks_NoMatchFallsBackToAll covers the no-match fallback.
func TestBuildTasks_NoMatchFallsBackToAll(t *testing.T) {
	got := buildTasks("nonexistent-id")
	if len(got) != 7 {
		t.Errorf("expected fallback to all 7 tasks, got %d", len(got))
	}
}

// TestFindBackend_Found covers the found case.
func TestFindBackend_Found(t *testing.T) {
	backends := []eval.LLMBackend{
		{Name: "qwen-plus", Model: "qwen3.7-plus"},
		{Name: "minimax", Model: "MiniMax-M3"},
	}
	b, ok := findBackend(backends, "MiniMax-M3")
	if !ok {
		t.Errorf("expected to find MiniMax-M3")
	}
	if b.Model != "MiniMax-M3" {
		t.Errorf("expected MiniMax-M3 model, got %s", b.Model)
	}
}

// TestFindBackend_NotFound covers the not-found case.
func TestFindBackend_NotFound(t *testing.T) {
	backends := []eval.LLMBackend{
		{Name: "qwen-plus", Model: "qwen3.7-plus"},
	}
	_, ok := findBackend(backends, "nonexistent")
	if ok {
		t.Errorf("expected not-found for nonexistent")
	}
}

// TestFindBackend_ByName covers name-based lookup (not just model).
func TestFindBackend_ByName(t *testing.T) {
	backends := []eval.LLMBackend{
		{Name: "aliyun-qwen3.7-plus", Model: "qwen3.7-plus"},
	}
	b, ok := findBackend(backends, "aliyun-qwen3.7-plus")
	if !ok {
		t.Errorf("expected to find by Name")
	}
	if b.Name != "aliyun-qwen3.7-plus" {
		t.Errorf("expected aliyun-qwen3.7-plus, got %s", b.Name)
	}
}

// TestOpReadConstants_HasExpectedRefs ensures the 1Password refs follow
// the op://Cursor_IronClaw/<item>/<field> pattern.
func TestOpReadConstants_HasExpectedRefs(t *testing.T) {
	for name, ref := range map[string]string{
		"aliyun":   opRefAliyun,
		"minimax1": opRefMinimax1,
		"minimax2": opRefMinimax2,
	} {
		if !strings.HasPrefix(ref, "op://Cursor_IronClaw/") {
			t.Errorf("%s ref %q does not start with op://Cursor_IronClaw/", name, ref)
		}
	}
}

// stubEngramSender is a fake engramSender used by engram-related tests.
type stubEngramSender struct {
	status   int
	err      error
	calls    int
	lastURL  string
	lastCT   string
	lastBody []byte
}

func (s *stubEngramSender) Post(_ context.Context, url, contentType string, body []byte) (int, error) {
	s.calls++
	s.lastURL = url
	s.lastCT = contentType
	s.lastBody = body
	return s.status, s.err
}

// TestEngramConfig_IsEnabled covers the URL-trim path.
func TestEngramConfig_IsEnabled(t *testing.T) {
	if (EngramConfig{URL: ""}).IsEnabled() {
		t.Errorf("empty URL should be disabled")
	}
	if (EngramConfig{URL: "   "}).IsEnabled() {
		t.Errorf("whitespace URL should be disabled")
	}
	if !(EngramConfig{URL: "http://x:1"}).IsEnabled() {
		t.Errorf("non-empty URL should be enabled")
	}
}

// TestPersistReportOptional_DisabledNoOp covers the disabled branch.
func TestPersistReportOptional_DisabledNoOp(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sender := &stubEngramSender{}
	persistReportOptional(context.Background(), logger, sender, EngramConfig{}, integration.EvalExperiment{ID: "exp-1"})
	if sender.calls != 0 {
		t.Errorf("expected no sender call when disabled")
	}
}

// TestPersistReportOptional_PushFailsDoesNotPanic covers the warn-and-continue path.
func TestPersistReportOptional_PushFailsDoesNotPanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sender := &stubEngramSender{err: errors.New("network down")}
	cfg := EngramConfig{URL: "http://engram:8280", AppID: "app", UserID: "user", Timeout: time.Second}
	report := eval.EvalReport{RubricVersion: "v1"}
	persistReportOptional(context.Background(), logger, sender, cfg, BuildEngramExperiment("exp-1", "n", "h", report))
	if sender.calls != 1 {
		t.Errorf("expected exactly 1 call attempt")
	}
}

// TestOpRead_InvalidBinary covers the exec-failure path. The `op` binary is
// almost certainly not on PATH in CI, so exec should fail. This is a
// best-effort smoke test; if `op` is installed it returns the value, but
// we only assert that no panic occurs and the function returns something.
func TestOpRead_InvalidBinary(t *testing.T) {
	// Point PATH at an empty dir so exec.LookPath fails fast.
	t.Setenv("PATH", t.TempDir())
	_, err := opRead("op://test/test/test")
	if err == nil {
		t.Logf("op CLI available; skipping negative assertion")
		return
	}
	if !strings.Contains(err.Error(), "op read") {
		t.Errorf("expected error to mention op read, got %v", err)
	}
}

// TestEnvOrDefault exercises the helper directly.
func TestEnvOrDefault(t *testing.T) {
	if got := envOrDefault("__no_such_var__", "fallback"); got != "fallback" {
		t.Errorf("unset: got %q", got)
	}
	t.Setenv("__env_or_default_test__", "real")
	if got := envOrDefault("__env_or_default_test__", "fallback"); got != "real" {
		t.Errorf("set: got %q", got)
	}
}

// TestRealHTTPSender_BadURL covers the request-build error path.
func TestRealHTTPSender_BadURL(t *testing.T) {
	sender := &realHTTPSender{client: &http.Client{}}
	_, err := sender.Post(context.Background(), "://no-scheme", "application/json", []byte("{}"))
	if err == nil {
		t.Errorf("expected error for malformed URL")
	}
}

// TestRealHTTPSender_BadBody covers the network-error path. We use a port
// that should be closed.
func TestRealHTTPSender_BadBody(t *testing.T) {
	sender := &realHTTPSender{client: &http.Client{Timeout: 500 * time.Millisecond}}
	_, err := sender.Post(context.Background(), "http://127.0.0.1:1/", "application/json", []byte("{}"))
	if err == nil {
		t.Errorf("expected network error for unreachable host")
	}
}

// avoid unused-import lint errors when this file is compiled in isolation.
var _ = filepath.Join
var _ = os.Stat

// TestLiveOpRead_Delegates covers the production liveOpRead.Read path.
func TestLiveOpRead_Delegates(t *testing.T) {
	r := liveOpRead{}
	_, err := r.Read("op://this/does/not/exist")
	if err == nil {
		t.Skip("op CLI is available and ref is unexpectedly valid; skipping")
	}
	if !strings.Contains(err.Error(), "op read") {
		t.Errorf("expected 'op read' in error, got %v", err)
	}
}

// TestOpRead_NoBinary covers the case where `op` is not on PATH.
func TestOpRead_NoBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := opRead("op://test/test/test")
	if err == nil {
		t.Errorf("expected error when op is missing")
		return
	}
	if !strings.Contains(err.Error(), "op read") {
		t.Errorf("expected 'op read' in error, got %v", err)
	}
}

// TestPersistReportOptional_EmptyURL_Noop covers the empty-URL branch.
func TestPersistReportOptional_EmptyURL_Noop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sender := &stubEngramSender{}
	cfg := EngramConfig{URL: ""} // disabled
	report := eval.EvalReport{RubricVersion: "v1"}
	persistReportOptional(context.Background(), logger, sender, cfg, BuildEngramExperiment("exp-1", "n", "h", report))
	if sender.calls != 0 {
		t.Errorf("expected 0 calls when URL is empty, got %d", sender.calls)
	}
}

// TestPersistReportOptional_EmptyExperiment covers the experiment-empty branch.
func TestPersistReportOptional_EmptyExperiment(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sender := &stubEngramSender{}
	cfg := EngramConfig{URL: "http://engram:8280", AppID: "app", UserID: "user", Timeout: time.Second}
	persistReportOptional(context.Background(), logger, sender, cfg, integration.EvalExperiment{})
	if sender.calls != 0 {
		t.Errorf("expected 0 calls for empty experiment, got %d", sender.calls)
	}
}
