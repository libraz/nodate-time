package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
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

// ErrNotConfigured is returned by the mailer used when no SMTP relay is set.
var ErrNotConfigured = errors.New("mailer: no SMTP relay configured")

// NoopMailer stands in when mail delivery is off. Every Send returns
// ErrNotConfigured; features whose only channel is mail are not offered while
// this is the mailer in use (see Configured).
type NoopMailer struct{}

// Send returns ErrNotConfigured.
func (NoopMailer) Send(context.Context, Message) error { return ErrNotConfigured }

// Configured reports whether m can deliver mail: false for nil and for the
// noop mailer, including when either sits behind a Dispatcher. The router and
// the handlers decide on this alone, so what is offered and what is reported
// to the SPA cannot disagree.
func Configured(m Mailer) bool {
	switch v := m.(type) {
	case nil:
		return false
	case NoopMailer:
		return false
	case *Dispatcher:
		return v != nil && Configured(v.inner)
	}
	return true
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
	// Brand heads the HTML alternative of every message.
	Brand Brand
}

// defaultSMTPTimeout bounds a conversation when neither the config nor the
// caller's context sets a shorter one. A relay that accepts the connection and
// then goes silent would otherwise hold the sender for the OS TCP timeout,
// which is minutes.
const defaultSMTPTimeout = 20 * time.Second

// deadlineGrace is how much later than the context the connection's own
// deadline is set. See deliver.
const deadlineGrace = time.Second

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
	// header injection via attacker-controlled recipient/subject, and anything
	// outside ASCII goes out as an RFC 2047 encoded-word: the deployment's
	// name is interpolated into the subject and is not limited to ASCII.
	from := sanitizeHeader(m.cfg.From)
	if addr, err := mail.ParseAddress(from); err == nil {
		from = addr.String()
	}
	to := sanitizeHeader(msg.To)
	subject := mime.QEncoding.Encode("utf-8", sanitizeHeader(msg.Subject))
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	mw := multipart.NewWriter(&b)
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", mw.Boundary())
	text := strings.ReplaceAll(msg.Text, "\r\n", "\n")
	for _, part := range []struct{ ctype, body string }{
		{"text/plain", text},
		{"text/html", renderHTML(m.cfg.Brand, text)},
	} {
		if err := writeQPPart(mw, part.ctype, part.body); err != nil {
			return fmt.Errorf("encode body: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("encode body: %w", err)
	}

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

// writeQPPart adds one utf-8, quoted-printable part to mw.
func writeQPPart(mw *multipart.Writer, ctype, body string) error {
	w, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {ctype + "; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return err
	}
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(body)); err != nil {
		return err
	}
	return qp.Close()
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
		// A hair past the context's own deadline, so the two cannot race to
		// end the same send: the watchdog below reaches the connection first
		// and the caller is told the deadline passed, rather than being handed
		// whichever of "i/o timeout" and "context deadline exceeded" the
		// scheduler happened to produce. The connection deadline stays as the
		// backstop for a watchdog that never gets to run.
		if err := conn.SetDeadline(deadline.Add(deadlineGrace)); err != nil {
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
	sender := m.cfg.From
	if addr, err := mail.ParseAddress(sender); err == nil {
		sender = addr.Address
	}
	if err := c.Mail(sender); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		// A relay's refusal usually quotes the address back, and this error
		// ends up in the log; the reply code is what an operator acts on.
		var reply *textproto.Error
		if errors.As(err, &reply) {
			return fmt.Errorf("rcpt to: refused with %d", reply.Code)
		}
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

// New selects a mailer implementation. When cfg.Host and cfg.From are set,
// mail is delivered via SMTP; otherwise a NoopMailer is returned and mail
// delivery is off.
func New(cfg SMTPConfig) Mailer {
	if cfg.Host != "" && cfg.From != "" {
		return SMTPMailer{cfg: cfg}
	}
	if cfg.Host != "" {
		slog.Warn("SMTP host set but sender (TC_SMTP_FROM) missing; mail delivery is off")
	}
	return NoopMailer{}
}
