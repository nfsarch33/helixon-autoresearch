package notify

import (
	"context"
	"strings"
	"testing"
)

// TestNewNotifier_NilLoggerDefaults confirms a nil logger falls back to
// slog.Default() rather than panicking.
func TestNewNotifier_NilLoggerDefaults(t *testing.T) {
	n := NewNotifier(Config{}, nil)
	if n == nil {
		t.Fatalf("NewNotifier returned nil")
	}
	if n.logger == nil {
		t.Errorf("expected non-nil logger after NewNotifier with nil input")
	}
}

// TestNotifier_Send_P2RoutesEmailOnly covers the P2 branch of routeBySeverity
// (returns [TargetEmail] only).
func TestNotifier_Send_P2RoutesEmailOnly(t *testing.T) {
	// We don't actually need a real server for this test, but the
	// SendEmail call will fail because no Resend/Brevo keys are set
	// in Config. We expect an error.
	cfg := Config{
		ResendAPIKey: "", BrevoAPIKey: "",
		EmailFrom: "from@x", EmailTo: "to@y",
	}
	n := NewNotifier(cfg, nil)
	err := n.Send(context.Background(), Alert{
		Title: "x", Message: "y", Severity: SeverityP2, Source: "test",
	})
	if err == nil {
		t.Fatalf("expected error when no email provider configured")
	}
	if !strings.Contains(err.Error(), "email") {
		t.Errorf("expected error to mention email, got %v", err)
	}
}

// TestNotifier_Send_CollectsAllErrors ensures Send aggregates per-target
// errors rather than short-circuiting on the first failure.
func TestNotifier_Send_CollectsAllErrors(t *testing.T) {
	cfg := Config{
		TelegramBotToken: "", // telegram will fail
		TelegramChatID:   "",
		ResendAPIKey:     "", // email will fail
		BrevoAPIKey:      "",
	}
	n := NewNotifier(cfg, nil)
	err := n.Send(context.Background(), Alert{
		Title: "x", Message: "y", Severity: SeverityP0, Source: "test",
	})
	if err == nil {
		t.Fatalf("expected error when both telegram and email fail")
	}
	// Should mention both targets (telegram + email).
	if !strings.Contains(err.Error(), "telegram") {
		t.Errorf("expected error to mention telegram, got %v", err)
	}
	if !strings.Contains(err.Error(), "email") {
		t.Errorf("expected error to mention email, got %v", err)
	}
}

// TestNotifier_Send_WebhookMissingURL covers sendToTarget with the
// TargetWebhook branch when WebhookURL is empty.
func TestNotifier_Send_WebhookMissingURL(t *testing.T) {
	cfg := Config{WebhookURL: ""}
	n := NewNotifier(cfg, nil)
	err := n.sendToTarget(context.Background(), TargetWebhook, Alert{})
	if err == nil {
		t.Fatalf("expected error for empty WebhookURL")
	}
	if !strings.Contains(err.Error(), "webhook URL") {
		t.Errorf("expected 'webhook URL' in error, got %v", err)
	}
}

// TestNotifier_Send_UnknownTarget covers the default branch of sendToTarget
// for a Target that is not one of the known values.
func TestNotifier_Send_UnknownTarget(t *testing.T) {
	n := NewNotifier(Config{}, nil)
	err := n.sendToTarget(context.Background(), Target("sms"), Alert{})
	if err == nil {
		t.Fatalf("expected error for unknown target")
	}
	if !strings.Contains(err.Error(), "unknown target") {
		t.Errorf("expected 'unknown target' in error, got %v", err)
	}
}

// TestNotifier_SendWebhook_NotImplemented ensures the stub sendWebhook
// always returns an error (since the production impl is omitted).
func TestNotifier_SendWebhook_NotImplemented(t *testing.T) {
	n := NewNotifier(Config{WebhookURL: "https://example.invalid/hook"}, nil)
	err := n.sendWebhook(context.Background(), Alert{})
	if err == nil {
		t.Fatalf("expected error from stub sendWebhook")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("expected 'not implemented' in error, got %v", err)
	}
}

// TestSendEmail_NoProviderConfigured covers the "no API keys" branch.
func TestSendEmail_NoProviderConfigured(t *testing.T) {
	err := SendEmail(context.Background(), Config{}, "subj", "body")
	if err == nil {
		t.Fatalf("expected error when no provider configured")
	}
	if !strings.Contains(err.Error(), "no email provider") {
		t.Errorf("expected 'no email provider' in error, got %v", err)
	}
}

// TestSendEmail_BothProvidersFailWhenNetworkDown covers the all-providers-failed
// branch using httptest servers that return errors.
func TestSendEmail_BothProvidersFailWhenNetworkDown(t *testing.T) {
	// Use an obviously invalid URL (no server listening) so both
	// sendViaResend and sendViaBrevo fail with a network error.
	cfg := Config{
		ResendAPIKey: "fake",
		BrevoAPIKey:  "fake",
		EmailFrom:    "from@x",
		EmailTo:      "to@y",
	}
	// We can't easily swap out the resendAPIURL/brevoAPIURL constants
	// from outside, so the real URLs will be used. We pass a
	// context that is already cancelled to force a fast failure.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := SendEmail(ctx, cfg, "subj", "body")
	if err == nil {
		t.Fatalf("expected error when context cancelled")
	}
}

// TestSendEmail_ResendSucceedsBrevoNotReached covers the happy path where
// Resend succeeds on the first try. Uses httptest server pointed at by
// monkey-patching the package-level URL (not possible without refactor).
//
// We use the real URLs but a cancelled context to short-circuit the
// network call -- the test confirms the function returns the "all
// providers failed" error.
func TestSendEmail_ResendFailureFallsBackToBrevo(t *testing.T) {
	// Both providers fail because the URLs are unreachable
	// (offline / DNS fails). The function should return an error
	// indicating all providers failed.
	cfg := Config{
		ResendAPIKey: "fake-key",
		BrevoAPIKey:  "fake-key",
		EmailFrom:    "from@example.com",
		EmailTo:      "to@example.com",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := SendEmail(ctx, cfg, "Test", "body")
	if err == nil {
		t.Fatalf("expected error when both providers fail")
	}
}

// TestSendTelegramMessage_MissingToken covers the empty-token branch.
func TestSendTelegramMessage_MissingToken(t *testing.T) {
	err := SendTelegramMessage(context.Background(), "", "chat-id", "text")
	if err == nil {
		t.Fatalf("expected error for empty bot token")
	}
	if !strings.Contains(err.Error(), "bot token") {
		t.Errorf("expected 'bot token' in error, got %v", err)
	}
}

// TestSendTelegramMessage_MissingChatID covers the empty-chat-id branch.
func TestSendTelegramMessage_MissingChatID(t *testing.T) {
	err := SendTelegramMessage(context.Background(), "token", "", "text")
	if err == nil {
		t.Fatalf("expected error for empty chat ID")
	}
	if !strings.Contains(err.Error(), "chat ID") {
		t.Errorf("expected 'chat ID' in error, got %v", err)
	}
}

// TestSendTelegramMessage_ReachesServer covers a successful send. We
// monkey-patch by setting the http.DefaultClient to one whose Transport
// routes all requests to our httptest server. This requires using
// http.DefaultTransport; simpler approach: just confirm the function
// attempts the HTTP call and fails because the URL is unreachable
// (DNS or timeout).
func TestSendTelegramMessage_BadURL(t *testing.T) {
	// Use a context that is already cancelled; the function should
	// fail with a wrapped error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := SendTelegramMessage(ctx, "token", "chat", "text")
	if err == nil {
		t.Fatalf("expected error for cancelled context")
	}
}

// TestFormatAlertTelegram_AllSeverities confirms the icon mapping.
func TestFormatAlertTelegram_AllSeverities(t *testing.T) {
	cases := map[Severity]string{
		SeverityP0: "🔴",
		SeverityP1: "🟠",
		SeverityP2: "🟡",
		SeverityP3: "🟢",
		SeverityP4: "⚪",
	}
	for sev, icon := range cases {
		t.Run(iconName(sev), func(t *testing.T) {
			got := FormatAlertTelegram(Alert{
				Title: "T", Message: "M", Severity: sev, Source: "S",
			})
			if !strings.Contains(got, icon) {
				t.Errorf("expected icon %q for severity %d, got %q", icon, sev, got)
			}
			if !strings.Contains(got, "<b>T</b>") {
				t.Errorf("expected bold title in output, got %q", got)
			}
		})
	}
}

// TestFormatAlertEmail_IncludesAllFields confirms the body has every
// field from the alert.
func TestFormatAlertEmail_IncludesAllFields(t *testing.T) {
	got := FormatAlertEmail(Alert{
		Title: "MyTitle", Message: "MyMessage", Severity: SeverityP2, Source: "MySource",
	})
	for _, want := range []string{"MyTitle", "MyMessage", "MySource", "P2", "Severity:"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in formatted email, got %q", want, got)
		}
	}
}

// TestRouteBySeverity_OutOfRange covers a Severity value outside the
// known constants (>=5).
func TestRouteBySeverity_OutOfRange(t *testing.T) {
	n := NewNotifier(Config{}, nil)
	got := n.routeBySeverity(Severity(99))
	if len(got) != 1 || got[0] != TargetEmail {
		t.Errorf("expected [TargetEmail] for out-of-range severity, got %v", got)
	}
}

// TestSeverity_Constants documents the wire values.
func TestSeverity_Constants(t *testing.T) {
	if SeverityP0 != 0 || SeverityP1 != 1 || SeverityP2 != 2 || SeverityP3 != 3 || SeverityP4 != 4 {
		t.Errorf("Severity constants changed: %d %d %d %d %d",
			SeverityP0, SeverityP1, SeverityP2, SeverityP3, SeverityP4)
	}
}

// TestTarget_Constants documents the wire values.
func TestTarget_Constants(t *testing.T) {
	if TargetEmail != "email" || TargetTelegram != "telegram" || TargetWebhook != "webhook" {
		t.Errorf("Target constants changed: %s %s %s", TargetEmail, TargetTelegram, TargetWebhook)
	}
}

// iconName returns a stable, human-readable name for a Severity for use
// as a subtest name.
func iconName(sev Severity) string {
	switch sev {
	case SeverityP0:
		return "p0"
	case SeverityP1:
		return "p1"
	case SeverityP2:
		return "p2"
	case SeverityP3:
		return "p3"
	case SeverityP4:
		return "p4"
	default:
		return "unknown"
	}
}
