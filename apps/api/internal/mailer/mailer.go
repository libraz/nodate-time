package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
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
	// Timeout bounds one whole SMTP conversation. Zero selects
	// defaultSMTPTimeout.
	Timeout time.Duration
}

// defaultSMTPTimeout bounds a conversation when neither the config nor the
// caller's context sets a shorter one. A relay that accepts the connection and
// then goes silent would otherwise hold the sender for the OS TCP timeout,
// which is minutes.
const defaultSMTPTimeout = 20 * time.Second

// SMTPMailer sends mail via a real SMTP server using STARTTLS when the server
// advertises it (the common submission setup on port 587).
type SMTPMailer struct {
	cfg SMTPConfig
}

func (m SMTPMailer) Send(ctx context.Context, msg Message) error {
	timeout := m.cfg.Timeout
	if timeout <= 0 {
		timeout = defaultSMTPTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

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

	if err := m.deliver(ctx, msg.To, []byte(b.String())); err != nil {
		// A context that ended mid-conversation surfaces as whatever I/O error
		// the connection reported; report the cause the caller can act on.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("smtp send: %w", ctxErr)
		}
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

// deliver runs the SMTP conversation under ctx. net/smtp has no context-aware
// API, so ctx is enforced on the connection instead: its deadline covers every
// read and write, and a cancellation closes the connection, which is the only
// thing that unblocks a call already waiting on the relay.
func (m SMTPMailer) deliver(ctx context.Context, to string, body []byte) error {
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("set deadline: %w", err)
		}
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return fmt.Errorf("greeting: %w", err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if m.cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
				return fmt.Errorf("auth: %w", err)
			}
		}
	}
	if err := c.Mail(m.cfg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}
	return c.Quit()
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
