package main

import (
	"errors"
	"testing"

	"github.com/nfsarch33/helixon-autoresearch/eval"
)

// fakeOpRead is a configurable 1Password reader used by resolveKeys. Each
// entry maps an `op://` reference to a return value; missing references
// return opReadMissingErr so tests can assert failure paths.
type fakeOpRead struct {
	values map[string]string
	calls  []string
}

var opReadMissingErr = errors.New("op ref missing in fake")

func (f *fakeOpRead) Read(ref string) (string, error) {
	f.calls = append(f.calls, ref)
	v, ok := f.values[ref]
	if !ok {
		return "", opReadMissingErr
	}
	return v, nil
}

// Test 1: keyBundle carries both MiniMax keys when both op refs resolve.
func TestResolveKeys_ReadsBothMinimaxKeys(t *testing.T) {
	f := &fakeOpRead{
		values: map[string]string{
			"op://Cursor_IronClaw/Aliyun Team Qwen Token Plan Key/password": "aliyun-secret",
			"op://Cursor_IronClaw/minimax-api-1/api-key":                    "minimax-key-1",
			"op://Cursor_IronClaw/minimax-api-2/api-key":                    "minimax-key-2",
		},
	}
	keys, err := resolveKeys(f)
	if err != nil {
		t.Fatalf("resolveKeys: %v", err)
	}
	if keys.aliyun != "aliyun-secret" {
		t.Errorf("aliyun = %q, want %q", keys.aliyun, "aliyun-secret")
	}
	if keys.minimax1 != "minimax-key-1" {
		t.Errorf("minimax1 = %q, want %q", keys.minimax1, "minimax-key-1")
	}
	if keys.minimax2 != "minimax-key-2" {
		t.Errorf("minimax2 = %q, want %q", keys.minimax2, "minimax-key-2")
	}
}

// Test 2: resolveKeys fails fast when aliyun ref is missing — we never want
// to silently run with no aliyun credential.
func TestResolveKeys_FailsOnMissingAliyun(t *testing.T) {
	f := &fakeOpRead{
		values: map[string]string{
			"op://Cursor_IronClaw/minimax-api-1/api-key": "minimax-key-1",
			"op://Cursor_IronClaw/minimax-api-2/api-key": "minimax-key-2",
		},
	}
	_, err := resolveKeys(f)
	if err == nil {
		t.Fatal("expected error on missing aliyun ref, got nil")
	}
}

// Test 3: resolveKeys fails fast when minimax-api-2 ref is missing.
func TestResolveKeys_FailsOnMissingMinimax2(t *testing.T) {
	f := &fakeOpRead{
		values: map[string]string{
			"op://Cursor_IronClaw/Aliyun Team Qwen Token Plan Key/password": "aliyun-secret",
			"op://Cursor_IronClaw/minimax-api-1/api-key":                    "minimax-key-1",
		},
	}
	_, err := resolveKeys(f)
	if err == nil {
		t.Fatal("expected error on missing minimax2 ref, got nil")
	}
}

// Test 4: pickMinimaxKey defaults to key1 when both are healthy.
func TestPickMinimaxKey_DefaultsToKey1(t *testing.T) {
	keys := keyBundle{minimax1: "k1", minimax2: "k2"}
	got, label := pickMinimaxKey(keys, nil)
	if got != "k1" {
		t.Errorf("got %q, want %q", got, "k1")
	}
	if label != "minimax-api-1" {
		t.Errorf("label = %q, want %q", label, "minimax-api-1")
	}
}

// Test 5: pickMinimaxKey rotates to key2 when key1 is marked unhealthy.
func TestPickMinimaxKey_FailoverToKey2(t *testing.T) {
	keys := keyBundle{minimax1: "k1", minimax2: "k2"}
	got, label := pickMinimaxKey(keys, []int{1}) // key1 unhealthy
	if got != "k2" {
		t.Errorf("got %q, want %q", got, "k2")
	}
	if label != "minimax-api-2" {
		t.Errorf("label = %q, want %q", label, "minimax-api-2")
	}
}

// Test 6: pickMinimaxKey falls through to last-known-good key1 when both
// keys are marked unhealthy (operator sees a clear 401 instead of a 503).
func TestPickMinimaxKey_BothUnhealthy_ReturnsKey1(t *testing.T) {
	keys := keyBundle{minimax1: "k1", minimax2: "k2"}
	got, label := pickMinimaxKey(keys, []int{1, 2})
	if got != "k1" {
		t.Errorf("got %q, want %q (last-known-good key1)", got, "k1")
	}
	if label != "minimax-api-1" {
		t.Errorf("label = %q, want %q (last-known-good key1)", label, "minimax-api-1")
	}
}

// Test 6b: pickMinimaxKey prefers key2 over key1 when key1 alone is unhealthy.
func TestPickMinimaxKey_OnlyKey2Healthy(t *testing.T) {
	keys := keyBundle{minimax1: "k1", minimax2: "k2"}
	got, label := pickMinimaxKey(keys, []int{1}) // only key2 healthy
	if got != "k2" {
		t.Errorf("got %q, want %q", got, "k2")
	}
	if label != "minimax-api-2" {
		t.Errorf("label = %q, want %q", label, "minimax-api-2")
	}
}

// Test 7: buildBackends attaches the picked MiniMax key to the M3 backend.
func TestBuildBackends_AttachesPickedMinimaxKey(t *testing.T) {
	keys := keyBundle{
		aliyun:   "a",
		minimax1: "k1",
		minimax2: "k2",
	}
	all := buildBackends("all", "", keys)
	var m3 *eval.LLMBackend
	for i := range all {
		if all[i].Model == "MiniMax-M3" {
			m3 = &all[i]
			break
		}
	}
	if m3 == nil {
		t.Fatal("MiniMax-M3 backend not found in buildBackends output")
	}
	if m3.APIKey != "k1" {
		t.Errorf("MiniMax-M3 APIKey = %q, want %q (default key1)", m3.APIKey, "k1")
	}
}

// Test 8: buildBackends honors health signal by rotating key.
func TestBuildBackends_RotatesOnHealthSignal(t *testing.T) {
	keys := keyBundle{
		aliyun:   "a",
		minimax1: "k1",
		minimax2: "k2",
	}
	all := buildBackendsWithHealth("all", "", keys, []int{1}) // key1 unhealthy → rotate to key2
	var m3 *eval.LLMBackend
	for i := range all {
		if all[i].Model == "MiniMax-M3" {
			m3 = &all[i]
			break
		}
	}
	if m3 == nil {
		t.Fatal("MiniMax-M3 backend not found")
	}
	if m3.APIKey != "k2" {
		t.Errorf("MiniMax-M3 APIKey = %q, want %q (rotated to key2)", m3.APIKey, "k2")
	}
}

// Test 9: call counter — ensure resolveKeys calls opRead exactly once per ref.
func TestResolveKeys_CallsOpReadOncePerRef(t *testing.T) {
	f := &fakeOpRead{
		values: map[string]string{
			"op://Cursor_IronClaw/Aliyun Team Qwen Token Plan Key/password": "a",
			"op://Cursor_IronClaw/minimax-api-1/api-key":                    "k1",
			"op://Cursor_IronClaw/minimax-api-2/api-key":                    "k2",
		},
	}
	if _, err := resolveKeys(f); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 3 {
		t.Errorf("opRead called %d times, want 3 (aliyun + minimax1 + minimax2)", len(f.calls))
	}
}
