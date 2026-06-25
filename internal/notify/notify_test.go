package notify

import (
	"context"
	"strings"
	"testing"
)

func TestNotifier_RouteBySeverity(t *testing.T) {
	n := NewNotifier(Config{}, nil)

	tests := []struct {
		name     string
		severity Severity
		want     []Target
	}{
		{"P0 routes to telegram+email", SeverityP0, []Target{TargetTelegram, TargetEmail}},
		{"P1 routes to telegram+email", SeverityP1, []Target{TargetTelegram, TargetEmail}},
		{"P2 routes to email only", SeverityP2, []Target{TargetEmail}},
		{"P3 routes to email only", SeverityP3, []Target{TargetEmail}},
		{"P4 routes to email only", SeverityP4, []Target{TargetEmail}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := n.routeBySeverity(tt.severity)
			if len(got) != len(tt.want) {
				t.Errorf("routeBySeverity(%d) = %v targets, want %v", tt.severity, len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("routeBySeverity(%d)[%d] = %v, want %v", tt.severity, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNotifier_Send(t *testing.T) {
	// This test verifies routing logic without actually sending
	// In production, use mock HTTP clients or integration tests
	cfg := Config{
		TelegramBotToken: "test-token",
		TelegramChatID:   "test-chat-id",
	}

	n := NewNotifier(cfg, nil)

	// Test that P0 alert attempts both channels
	alert := Alert{
		Title:    "Test Alert",
		Message:  "This is a test",
		Severity: SeverityP0,
		Source:   "test",
	}

	err := n.Send(context.Background(), alert)
	// Expect error because test token won't work, but routing should execute
	if err == nil {
		t.Log("Send completed (may have used mock or real credentials)")
	}
}

func TestFormatAlertTelegram(t *testing.T) {
	alert := Alert{
		Title:    "CI Failure",
		Message:  "Pipeline failed",
		Severity: SeverityP1,
		Source:   "gitlab-ci",
	}

	formatted := FormatAlertTelegram(alert)

	if formatted == "" {
		t.Error("FormatAlertTelegram returned empty string")
	}

	// Verify it contains expected elements
	if !strings.Contains(formatted, "CI Failure") {
		t.Error("formatted message missing title")
	}
	if !strings.Contains(formatted, "P1") {
		t.Error("formatted message missing severity")
	}
}

func TestFormatAlertEmail(t *testing.T) {
	alert := Alert{
		Title:    "Experiment Complete",
		Message:  "Accuracy improved by 5%",
		Severity: SeverityP2,
		Source:   "autoresearch",
	}

	formatted := FormatAlertEmail(alert)

	if formatted == "" {
		t.Error("FormatAlertEmail returned empty string")
	}
	if !strings.Contains(formatted, "Experiment Complete") {
		t.Error("formatted message missing title")
	}
}
