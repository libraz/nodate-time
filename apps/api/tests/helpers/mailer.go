package helpers

import (
	"context"
	"sync"

	"github.com/libraz/nodate-time/apps/api/internal/mailer"
)

// CapturingMailer records all emails sent during a test so the body can be
// inspected (e.g. to extract a password-reset token).
type CapturingMailer struct {
	mu       sync.Mutex
	Messages []mailer.Message
}

func (m *CapturingMailer) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Messages = append(m.Messages, msg)
	return nil
}

// LastFor returns the most recent message addressed to the given recipient.
func (m *CapturingMailer) LastFor(to string) (mailer.Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.Messages) - 1; i >= 0; i-- {
		if m.Messages[i].To == to {
			return m.Messages[i], true
		}
	}
	return mailer.Message{}, false
}
