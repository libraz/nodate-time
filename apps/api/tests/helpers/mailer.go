package helpers

import (
	"context"
	"sync"

	"github.com/libraz/nodate-time/apps/api/internal/mailer"
)

// CapturingMailer records all emails sent during a test so the body can be
// inspected (e.g. to extract a password-reset token).
//
// NewCapturingMailer wraps the recorder in the same background dispatcher the
// server wires the real mailer with, so the suite exercises the asynchronous
// send path rather than a shape production does not run. LastFor then waits
// for every accepted message to be recorded, which is what keeps a test that
// reads mail straight after the HTTP response deterministic. The zero value
// records inline, for a caller that wants no hand-off at all.
type CapturingMailer struct {
	mu       sync.Mutex
	Messages []mailer.Message
	dispatch *mailer.Dispatcher
}

// NewCapturingMailer returns a capturing double that delivers in the
// background, the way the server does.
func NewCapturingMailer() *CapturingMailer {
	m := &CapturingMailer{}
	m.dispatch = mailer.NewDispatcher(mailer.SenderFunc(m.record), mailer.DispatcherOptions{})
	return m
}

func (m *CapturingMailer) Send(ctx context.Context, msg mailer.Message) error {
	if m.dispatch == nil {
		return m.record(ctx, msg)
	}
	return m.dispatch.Send(ctx, msg)
}

func (m *CapturingMailer) record(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Messages = append(m.Messages, msg)
	return nil
}

// LastFor returns the most recent message addressed to the given recipient. It
// first waits for the sends already accepted, so a message handed off by the
// request under test is recorded by the time it looks.
func (m *CapturingMailer) LastFor(to string) (mailer.Message, bool) {
	if m.dispatch != nil {
		m.dispatch.Wait()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.Messages) - 1; i >= 0; i-- {
		if m.Messages[i].To == to {
			return m.Messages[i], true
		}
	}
	return mailer.Message{}, false
}
