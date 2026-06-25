package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	resendAPIURL = "https://api.resend.com/emails"
	brevoAPIURL  = "https://api.brevo.com/v3/smtp/email"
)

// EmailMessage represents an email payload.
type EmailMessage struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html,omitempty"`
}

// SendEmail sends an email via Resend or Brevo (round-robin).
func SendEmail(ctx context.Context, cfg Config, subject, body string) error {
	if cfg.ResendAPIKey == "" && cfg.BrevoAPIKey == "" {
		return fmt.Errorf("no email provider configured")
	}

	msg := EmailMessage{
		From:    cfg.EmailFrom,
		To:      []string{cfg.EmailTo},
		Subject: subject,
		Text:    body,
	}

	// Try Resend first, fallback to Brevo
	if cfg.ResendAPIKey != "" {
		if err := sendViaResend(ctx, cfg.ResendAPIKey, msg); err == nil {
			return nil
		}
	}

	if cfg.BrevoAPIKey != "" {
		return sendViaBrevo(ctx, cfg.BrevoAPIKey, msg)
	}

	return fmt.Errorf("all email providers failed")
}

func sendViaResend(ctx context.Context, apiKey string, msg EmailMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal email: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, resendAPIURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("resend request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("resend API returned status %d", resp.StatusCode)
	}

	return nil
}

func sendViaBrevo(ctx context.Context, apiKey string, msg EmailMessage) error {
	payload := map[string]interface{}{
		"sender":      map[string]string{"email": msg.From},
		"to":          []map[string]string{{"email": msg.To[0]}},
		"subject":     msg.Subject,
		"textContent": msg.Text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal email: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, brevoAPIURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create brevo request: %w", err)
	}
	req.Header.Set("api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("brevo request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("brevo API returned status %d", resp.StatusCode)
	}

	return nil
}

// FormatAlertEmail formats an alert for email display.
func FormatAlertEmail(alert Alert) string {
	return fmt.Sprintf("Alert: %s\n\nSeverity: P%d\nSource: %s\n\n%s",
		alert.Title, alert.Severity, alert.Source, alert.Message)
}
