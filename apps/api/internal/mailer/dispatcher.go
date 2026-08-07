package mailer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SenderFunc adapts a plain function to the Mailer interface.
type SenderFunc func(ctx context.Context, msg Message) error

func (f SenderFunc) Send(ctx context.Context, msg Message) error { return f(ctx, msg) }

const (
	// defaultDispatchTimeout bounds one background delivery attempt.
	defaultDispatchTimeout = 30 * time.Second
	// defaultMaxInFlight caps the goroutines a stalled relay can accumulate.
	// Beyond it messages are refused rather than queued without limit.
	defaultMaxInFlight = 64
)

// DispatcherOptions tunes a Dispatcher; the zero value is the production one.
type DispatcherOptions struct {
	// Timeout bounds one background delivery. Zero selects defaultDispatchTimeout.
	Timeout time.Duration
	// MaxInFlight caps concurrent deliveries. Zero selects defaultMaxInFlight.
	MaxInFlight int
}

// Dispatcher hands each message to a background goroutine so an unresponsive
// relay cannot hold the goroutine that asked for the send. It is a Mailer
// itself, so callers stay unaware of the hand-off.
//
// The background send deliberately does not run on the caller's context. A
// request context is cancelled the moment the response is written, which would
// abort every delivery this type exists to detach. Deliveries run on a
// value-preserving copy of it (context.WithoutCancel) under their own timeout,
// so request-scoped log attributes survive while the caller's cancellation
// does not reach the relay.
//
// Delivery outcomes cannot reach the caller either, so failures are logged.
// The recipient is not logged: the endpoints that send mail answer uniformly
// whether or not an address exists, and their logs keep that property.
type Dispatcher struct {
	inner       Mailer
	timeout     time.Duration
	maxInFlight int

	mu       sync.Mutex
	idle     *sync.Cond
	inFlight int
}

// NewDispatcher wraps inner so sends run in the background.
func NewDispatcher(inner Mailer, opts DispatcherOptions) *Dispatcher {
	d := &Dispatcher{
		inner:       inner,
		timeout:     opts.Timeout,
		maxInFlight: opts.MaxInFlight,
	}
	if d.timeout <= 0 {
		d.timeout = defaultDispatchTimeout
	}
	if d.maxInFlight <= 0 {
		d.maxInFlight = defaultMaxInFlight
	}
	d.idle = sync.NewCond(&d.mu)
	return d
}

// Send accepts msg for delivery and returns without waiting for the relay. The
// error it returns says only whether the message was accepted, never whether
// it was delivered.
func (d *Dispatcher) Send(ctx context.Context, msg Message) error {
	d.mu.Lock()
	if d.inFlight >= d.maxInFlight {
		d.mu.Unlock()
		return fmt.Errorf("mail not queued: %d deliveries already in flight", d.maxInFlight)
	}
	d.inFlight++
	d.mu.Unlock()

	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.timeout)
	go func() {
		defer cancel()
		defer d.finish()
		if err := d.inner.Send(sendCtx, msg); err != nil {
			slog.ErrorContext(sendCtx, "background mail delivery failed", "subject", msg.Subject, "error", err)
		}
	}()
	return nil
}

// Wait blocks until every accepted message has been delivered or given up on.
// Shutdown uses it to drain in-flight mail; a test uses it as the completion
// signal an inline send used to give the caller.
func (d *Dispatcher) Wait() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.inFlight > 0 {
		d.idle.Wait()
	}
}

func (d *Dispatcher) finish() {
	d.mu.Lock()
	d.inFlight--
	if d.inFlight == 0 {
		d.idle.Broadcast()
	}
	d.mu.Unlock()
}
