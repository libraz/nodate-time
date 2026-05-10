package mailer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Mailer is an abstraction over email delivery.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

type Message struct {
	To      string
	Subject string
	Text    string
}

// ConsoleMailer logs the email contents instead of sending.
// Use this in development to inspect outgoing mail without an SMTP server.
type ConsoleMailer struct{}

func (ConsoleMailer) Send(_ context.Context, msg Message) error {
	slog.Info("[mailer:console]",
		"to", msg.To,
		"subject", msg.Subject,
		"body", msg.Text,
	)
	fmt.Printf("\n========== EMAIL ==========\nTo: %s\nSubject: %s\n\n%s\n===========================\n\n",
		msg.To, msg.Subject, msg.Text)
	return nil
}

// SMTPConfig holds SMTP server credentials.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// SMTPMailer sends mail via a real SMTP server. Stub: implementing the
// transport is left to deployment time so we can pull in the appropriate
// dependency (net/smtp, go-mail, etc.) without forcing it on dev users.
type SMTPMailer struct {
	cfg SMTPConfig
}

func (m SMTPMailer) Send(_ context.Context, msg Message) error {
	// Real SMTP delivery is not yet implemented; fall back to console output
	// while loudly warning so a misconfigured prod environment is obvious.
	slog.Warn("[mailer:smtp] not yet implemented, falling back to console",
		"host", m.cfg.Host, "to", msg.To, "subject", msg.Subject)
	return ConsoleMailer{}.Send(context.Background(), msg)
}

// New picks a mailer implementation based on TC_MAIL_PROVIDER.
//   "console" (default) — log to stdout/slog
//   "smtp"              — deliver via SMTP (host/port/from configured separately)
// Any other value falls back to console with a warning.
func New() Mailer {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("TC_MAIL_PROVIDER")))
	switch provider {
	case "", "console":
		if provider == "" {
			slog.Warn("TC_MAIL_PROVIDER not set; using console mailer (development only)")
		}
		return ConsoleMailer{}
	case "smtp":
		port, _ := strconv.Atoi(os.Getenv("TC_SMTP_PORT"))
		if port == 0 {
			port = 587
		}
		cfg := SMTPConfig{
			Host:     os.Getenv("TC_SMTP_HOST"),
			Port:     port,
			Username: os.Getenv("TC_SMTP_USERNAME"),
			Password: os.Getenv("TC_SMTP_PASSWORD"),
			From:     os.Getenv("TC_SMTP_FROM"),
		}
		if cfg.Host == "" || cfg.From == "" {
			slog.Warn("SMTP mailer requested but TC_SMTP_HOST/TC_SMTP_FROM missing; using console")
			return ConsoleMailer{}
		}
		return SMTPMailer{cfg: cfg}
	default:
		slog.Warn("unknown TC_MAIL_PROVIDER; using console mailer", "provider", provider)
		return ConsoleMailer{}
	}
}
