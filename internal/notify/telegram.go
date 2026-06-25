package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const telegramAPIURL = "https://api.telegram.org/bot%s/sendMessage"

// TelegramMessage represents a Telegram bot message payload.
type TelegramMessage struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// SendTelegramMessage sends a formatted message via Telegram bot API.
func SendTelegramMessage(ctx context.Context, botToken, chatID, text string) error {
	if botToken == "" {
		return fmt.Errorf("telegram bot token not configured")
	}
	if chatID == "" {
		return fmt.Errorf("telegram chat ID not configured")
	}

	payload := TelegramMessage{
		ChatID:    chatID,
		Text:      text,
		ParseMode: "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram message: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, fmt.Sprintf(telegramAPIURL, botToken), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	return nil
}

// FormatAlertTelegram formats an alert for Telegram display.
func FormatAlertTelegram(alert Alert) string {
	var icon string
	switch alert.Severity {
	case SeverityP0:
		icon = "🔴"
	case SeverityP1:
		icon = "🟠"
	case SeverityP2:
		icon = "🟡"
	case SeverityP3:
		icon = "🟢"
	default:
		icon = "⚪"
	}

	return fmt.Sprintf("%s <b>%s</b>\n\nSeverity: P%d\nSource: %s\n\n%s",
		icon, alert.Title, alert.Severity, alert.Source, alert.Message)
}
