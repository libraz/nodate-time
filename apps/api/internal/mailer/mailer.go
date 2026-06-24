package mailer

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"time"
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

// ConsoleMailer logs the email contents instead of sending. Use this in
// development to inspect outgoing mail without an SMTP server. When Verbose is
// false (the default outside development) only metadata is logged — never the
// body, which may contain password-reset tokens or invite links.
type ConsoleMailer struct {
	Verbose bool
}

func (c ConsoleMailer) Send(_ context.Context, msg Message) error {
	if !c.Verbose {
		// Do not log the body: it can contain reset tokens / invite secrets.
		slog.Info("[mailer:console] suppressed message body (enable development mode to view)",
			"to", msg.To, "subject", msg.Subject)
		return nil
	}
	slog.Info("[mailer:console]", "to", msg.To, "subject", msg.Subject, "body", msg.Text)
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

// SMTPMailer sends mail via a real SMTP server using STARTTLS when the server
// advertises it (the common submission setup on port 587).
type SMTPMailer struct {
	cfg SMTPConfig
}

func (m SMTPMailer) Send(_ context.Context, msg Message) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	var auth smtp.Auth
	if m.cfg.Username != "" {
		auth = smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	}

	// Build a minimal RFC 5322 message. Header values are sanitized to prevent
	// header injection via attacker-controlled recipient/subject.
	from := sanitizeHeader(m.cfg.From)
	to := sanitizeHeader(msg.To)
	subject := sanitizeHeader(msg.Subject)
	body := strings.ReplaceAll(msg.Text, "\r\n", "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))

	if err := smtp.SendMail(addr, auth, m.cfg.From, []string{msg.To}, []byte(b.String())); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

// sanitizeHeader strips CR/LF so a value cannot inject additional headers.
func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// New selects a mailer implementation. When cfg.Host is set, mail is delivered
// via SMTP; otherwise a console mailer is returned (development only — the
// config layer rejects an unset host in production). devMode enables verbose
// console logging of message bodies.
func New(cfg SMTPConfig, devMode bool) Mailer {
	if cfg.Host != "" && cfg.From != "" {
		return SMTPMailer{cfg: cfg}
	}
	if cfg.Host != "" {
		slog.Warn("SMTP host set but sender (TC_SMTP_FROM) missing; using console mailer")
	}
	return ConsoleMailer{Verbose: devMode}
}
