package notify

// Severity represents alert priority levels.
type Severity int

const (
	SeverityP0 Severity = 0 // Critical: immediate attention required
	SeverityP1 Severity = 1 // High: urgent issues
	SeverityP2 Severity = 2 // Medium: important but not urgent
	SeverityP3 Severity = 3 // Low: informational
	SeverityP4 Severity = 4 // Debug: development only
)

// Target represents notification delivery channels.
type Target string

const (
	TargetEmail    Target = "email"
	TargetTelegram Target = "telegram"
	TargetWebhook  Target = "webhook"
)

// Alert represents a notification to be sent.
type Alert struct {
	Title    string   `json:"title"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
	Source   string   `json:"source"`
}

// Config holds notification system configuration.
type Config struct {
	// Telegram configuration
	TelegramBotToken string
	TelegramChatID   string

	// Email configuration (Resend/Brevo)
	ResendAPIKey string
	BrevoAPIKey  string
	EmailFrom    string
	EmailTo      string

	// Webhook configuration
	WebhookURL string
}
