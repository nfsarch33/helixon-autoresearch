// Package security provides the helixon-autoresearch security suite and
// helpers for asserting that the suite passes in a clean environment
// (no API tokens present, no live network calls, no secret-bearing
// env vars).
//
// v16751-4 CARRY-QA-03 introduces TestSecuritySuite_AllPassInCleanEnv
// which is the env-stripped contract: the security suite MUST pass
// without any tokens set. This catches regressions where a new code
// path silently depends on a vendor credential being present.
//
// Security gates enforced (in order):
//  1. No API tokens in environment (RESEND/BREVO/MINIMAX_API_KEY unset)
//  2. No live HTTP calls during the suite (httptest.Server only)
//  3. No filesystem writes outside t.TempDir()
//  4. No subprocess spawning (no exec.Command)
//
// Adding a new gate: append a CheckFunc to the Suite and ensure
// AllPassInCleanEnv still returns nil.
package security

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// CheckFunc is a single security gate that returns nil on PASS.
type CheckFunc func(ctx context.Context) error

// Suite aggregates ordered security checks.
type Suite struct {
	mu     sync.RWMutex
	checks []namedCheck
}

type namedCheck struct {
	Name string
	Fn   CheckFunc
}

// New returns an empty suite.
func New() *Suite {
	return &Suite{}
}

// Add appends a named check to the suite.
func (s *Suite) Add(name string, fn CheckFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks = append(s.checks, namedCheck{Name: name, Fn: fn})
}

// Len returns the number of registered checks.
func (s *Suite) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.checks)
}

// Run executes every check in registration order. Returns the joined
// error of all failures. Order matters: a later check may legitimately
// depend on a side effect of an earlier check (e.g. creating a
// httptest.Server in check N for use in check N+1).
func (s *Suite) Run(ctx context.Context) error {
	s.mu.RLock()
	checks := make([]namedCheck, len(s.checks))
	copy(checks, s.checks)
	s.mu.RUnlock()

	var errs []string
	for _, c := range checks {
		if err := c.Fn(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("[%s] %v", c.Name, err))
		}
	}
	if len(errs) > 0 {
		return errors.New("security suite failed: " + strings.Join(errs, "; "))
	}
	return nil
}

// DefaultTokenEnvVars lists env var names that MUST NOT be present for
// a "clean env" test run. Tests should call os.Unsetenv on each of
// these before invoking AllPassInCleanEnv.
//
// Order matters: keys are checked in the order declared.
var DefaultTokenEnvVars = []string{
	"RESEND_API_KEY",
	"BREVO_API_KEY",
	"MINIMAX_API_KEY",
	"MINIMAXI_API_KEY",
	"ALIYUN_TOKEN_PLAN_KEY",
	"GRAFANA_ADMIN_PASSWORD",
	"OP_SERVICE_ACCOUNT_TOKEN",
	"GH_TOKEN",
	"HF_TOKEN",
}

// AssertCleanEnv fails the test if any of DefaultTokenEnvVars is set.
// It is the entry point for TestSecuritySuite_AllPassInCleanEnv.
func AssertCleanEnv(t interface {
	Helper()
	Fatalf(format string, args ...interface{})
}) {
	t.Helper()
	for _, k := range DefaultTokenEnvVars {
		if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
			t.Fatalf("env %s is set (value redacted, length=%d); clean-env contract violated", k, len(v))
		}
	}
}
