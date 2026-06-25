package notify

import (
	"context"
	"fmt"
	"log/slog"
)

// Notifier manages multi-channel notification delivery.
type Notifier struct {
	cfg    Config
	logger *slog.Logger
}

// NewNotifier creates a notifier with the given configuration.
func NewNotifier(cfg Config, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{cfg: cfg, logger: logger}
}

// Send routes an alert to appropriate channels based on severity.
// P0/P1 -> Telegram + Email
// P2+   -> Email only
func (n *Notifier) Send(ctx context.Context, alert Alert) error {
	targets := n.routeBySeverity(alert.Severity)
	var errors []error

	for _, target := range targets {
		if err := n.sendToTarget(ctx, target, alert); err != nil {
			n.logger.Error("notification delivery failed",
				"target", target,
				"severity", alert.Severity,
				"err", err,
			)
			errors = append(errors, fmt.Errorf("%s: %w", target, err))
		} else {
			n.logger.Info("notification delivered",
				"target", target,
				"severity", alert.Severity,
				"title", alert.Title,
			)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to send to %d target(s): %v", len(errors), errors)
	}
	return nil
}

// routeBySeverity determines which targets to use for a given severity.
func (n *Notifier) routeBySeverity(sev Severity) []Target {
	switch {
	case sev <= SeverityP1:
		return []Target{TargetTelegram, TargetEmail}
	default:
		return []Target{TargetEmail}
	}
}

// sendToTarget dispatches an alert to a specific notification channel.
func (n *Notifier) sendToTarget(ctx context.Context, target Target, alert Alert) error {
	switch target {
	case TargetTelegram:
		text := FormatAlertTelegram(alert)
		return SendTelegramMessage(ctx, n.cfg.TelegramBotToken, n.cfg.TelegramChatID, text)

	case TargetEmail:
		subject := fmt.Sprintf("[%s] P%d: %s", alert.Source, alert.Severity, alert.Title)
		body := FormatAlertEmail(alert)
		return SendEmail(ctx, n.cfg, subject, body)

	case TargetWebhook:
		if n.cfg.WebhookURL == "" {
			return fmt.Errorf("webhook URL not configured")
		}
		return n.sendWebhook(ctx, alert)

	default:
		return fmt.Errorf("unknown target: %s", target)
	}
}

// sendWebhook posts alert data to a webhook URL.
func (n *Notifier) sendWebhook(ctx context.Context, alert Alert) error {
	// Implementation omitted for brevity - can be added later
	return fmt.Errorf("webhook not implemented")
}
