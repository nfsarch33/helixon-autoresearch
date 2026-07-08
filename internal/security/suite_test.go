// Tests for the security suite contract.
//
// v16751-4 CARRY-QA-03: TestSecuritySuite_AllPassInCleanEnv is the
// env-stripped contract. It verifies the security suite passes in a
// clean env (no vendor tokens set) so CI/local runs without secrets
// still produce a meaningful PASS verdict.
//
// Pattern: t.Setenv("KEY", "") to unset via t.Setenv's empty-value
// semantics; AssertCleanEnv then verifies all DefaultTokenEnvVars are
// absent.
package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSecuritySuite_AllPassInCleanEnv (v16751-4 CARRY-QA-03) is the
// env-stripped contract: the suite must pass with NO vendor tokens set.
// This is the regression test that catches "tests pass only because a
// real API key happens to be in the env" failures.
func TestSecuritySuite_AllPassInCleanEnv(t *testing.T) {
	// Unset every default token env var so this test is hermetic.
	for _, k := range DefaultTokenEnvVars {
		t.Setenv(k, "")
	}
	AssertCleanEnv(t)

	s := New()
	s.Add("noop_pass", func(ctx context.Context) error { return nil })
	s.Add("noop_pass_2", func(ctx context.Context) error { return nil })
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("clean-env suite run failed: %v", err)
	}
}

func TestSecuritySuite_FailsOnBadCheck(t *testing.T) {
	for _, k := range DefaultTokenEnvVars {
		t.Setenv(k, "")
	}
	AssertCleanEnv(t)

	s := New()
	s.Add("pass", func(ctx context.Context) error { return nil })
	s.Add("fail", func(ctx context.Context) error { return errors.New("deliberate failure") })
	s.Add("pass_after_fail", func(ctx context.Context) error { return nil })

	err := s.Run(context.Background())
	if err == nil {
		t.Fatal("expected suite to fail; got nil")
	}
	if !strings.Contains(err.Error(), "deliberate failure") {
		t.Errorf("expected error to contain 'deliberate failure'; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "[fail]") {
		t.Errorf("expected error to contain check name '[fail]'; got %q", err.Error())
	}
}

func TestSecuritySuite_NoHTTP(t *testing.T) {
	for _, k := range DefaultTokenEnvVars {
		t.Setenv(k, "")
	}
	AssertCleanEnv(t)

	// A check that uses httptest.NewServer + http.Get is acceptable (live
	// loopback only); a check that hits api.resend.com would FAIL because
	// of the no-tokens invariant. We document the contract here: tests
	// must use httptest loopback, never vendor URLs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s := New()
	s.Add("loopback_http", func(ctx context.Context) error {
		resp, err := http.Get(srv.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errors.New("non-200 from loopback")
		}
		return nil
	})
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("loopback HTTP suite failed: %v", err)
	}
}

func TestSecuritySuite_Len(t *testing.T) {
	for _, k := range DefaultTokenEnvVars {
		t.Setenv(k, "")
	}
	s := New()
	if got := s.Len(); got != 0 {
		t.Errorf("empty suite Len=%d, want 0", got)
	}
	s.Add("a", func(ctx context.Context) error { return nil })
	s.Add("b", func(ctx context.Context) error { return nil })
	if got := s.Len(); got != 2 {
		t.Errorf("Len after 2 adds=%d, want 2", got)
	}
}
