package mailer

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

// scriptedRelay is a plaintext SMTP relay that answers RCPT with rcptReply
// and hands back the DATA it was given.
func scriptedRelay(t *testing.T, rcptReply string) (host string, port int, data <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		say("220 relay ready")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch verb := strings.ToUpper(strings.Fields(line + " x")[0]); verb {
			case "EHLO", "HELO":
				say("250 relay")
			case "MAIL":
				say("250 ok")
			case "RCPT":
				say(rcptReply)
			case "DATA":
				say("354 go ahead")
				var msg strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					msg.WriteString(l)
				}
				out <- msg.String()
				say("250 queued")
			case "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, out
}

// A deployment name outside ASCII reaches the subject, and the body is
// whatever the template says; neither may put 8-bit bytes on the wire.
func TestSMTPMessageIsSevenBitOnTheWire(t *testing.T) {
	host, port, data := scriptedRelay(t, "250 ok")
	m := SMTPMailer{cfg: SMTPConfig{Host: host, Port: port, From: "Nodate 時間 <no-reply@example.com>", Timeout: 5 * time.Second}}
	const subject = "パスワードの再設定 — Nodate"
	const body = "こんにちは\nリンク: https://example.com/reset?token=abc\n"
	if err := m.Send(context.Background(), Message{To: "user@example.com", Subject: subject, Text: body}); err != nil {
		t.Fatalf("send: %v", err)
	}
	raw := <-data
	for i := range len(raw) {
		if raw[i] > 0x7f {
			t.Fatalf("byte %#x at %d is not 7-bit:\n%s", raw[i], i, raw)
		}
	}

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var dec mime.WordDecoder
	if got, err := dec.DecodeHeader(msg.Header.Get("Subject")); err != nil || got != subject {
		t.Errorf("subject decodes to %q (%v), want %q", got, err, subject)
	}
	if from, err := msg.Header.AddressList("From"); err != nil || from[0].Name != "Nodate 時間" {
		t.Errorf("From = %v (%v)", from, err)
	}
	if cte := msg.Header.Get("Content-Transfer-Encoding"); cte != "quoted-printable" {
		t.Fatalf("Content-Transfer-Encoding = %q", cte)
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got := strings.ReplaceAll(string(decoded), "\r\n", "\n"); got != body {
		t.Errorf("body decodes to %q, want %q", got, body)
	}
}

// A relay's refusal quotes the address back; the error the dispatcher logs
// must not carry it.
func TestSMTPRefusalDoesNotCarryTheRecipient(t *testing.T) {
	host, port, _ := scriptedRelay(t, "550 5.1.1 <person@example.com>: Recipient address rejected")
	m := SMTPMailer{cfg: SMTPConfig{Host: host, Port: port, From: "no-reply@example.com", Timeout: 5 * time.Second}}
	err := m.Send(context.Background(), Message{To: "person@example.com", Subject: "s", Text: "b"})
	if err == nil {
		t.Fatal("a refused recipient should fail the send")
	}
	if strings.Contains(err.Error(), "person@example.com") {
		t.Fatalf("send error carries the recipient: %v", err)
	}
	if !strings.Contains(err.Error(), "550") {
		t.Errorf("send error should keep the reply code: %v", err)
	}
}

// Outside development the console mailer says that a message was written,
// not to whom.
func TestConsoleMailerDoesNotLogTheRecipient(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if err := (ConsoleMailer{}).Send(context.Background(), Message{To: "person@example.com", Subject: "s", Text: "secret-token"}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); strings.Contains(out, "person@example.com") || strings.Contains(out, "secret-token") {
		t.Fatalf("console mailer logged the recipient or the body: %s", out)
	}
}
