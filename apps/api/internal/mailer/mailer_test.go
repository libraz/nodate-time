package mailer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSanitizeHeaderStripsCRLF(t *testing.T) {
	in := "Subject line\r\nBcc: victim@example.com"
	got := sanitizeHeader(in)
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("sanitizeHeader left CR/LF in %q", got)
	}
	if got != "Subject lineBcc: victim@example.com" {
		t.Fatalf("unexpected sanitized value: %q", got)
	}
}

func TestNoopMailerRefusesToSend(t *testing.T) {
	err := NoopMailer{}.Send(context.Background(), Message{To: "a@b.com", Subject: "s", Text: "t"})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestNewSelectsSMTPWhenHostSet(t *testing.T) {
	m := New(SMTPConfig{Host: "smtp.example.com", From: "no-reply@example.com", Port: 587})
	if _, ok := m.(SMTPMailer); !ok {
		t.Fatalf("expected SMTPMailer when host+from set, got %T", m)
	}
	m2 := New(SMTPConfig{})
	if _, ok := m2.(NoopMailer); !ok {
		t.Fatalf("expected NoopMailer when host unset, got %T", m2)
	}
}

func TestConfigured(t *testing.T) {
	smtp := SMTPMailer{}
	cases := []struct {
		name string
		m    Mailer
		want bool
	}{
		{"nil", nil, false},
		{"noop", NoopMailer{}, false},
		{"smtp", smtp, true},
		{"dispatched noop", NewDispatcher(NoopMailer{}, DispatcherOptions{}), false},
		{"dispatched smtp", NewDispatcher(smtp, DispatcherOptions{}), true},
	}
	for _, c := range cases {
		if got := Configured(c.m); got != c.want {
			t.Errorf("%s: Configured = %v, want %v", c.name, got, c.want)
		}
	}
}
