package mailer

import (
	"context"
	"testing"
	"time"
)

func TestDispatcherReturnsWhileTheRelayIsStillBusy(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	delivered := make(chan Message, 1)
	d := NewDispatcher(SenderFunc(func(_ context.Context, msg Message) error {
		entered <- struct{}{}
		<-release
		delivered <- msg
		return nil
	}), DispatcherOptions{})

	start := time.Now()
	if err := d.Send(context.Background(), Message{To: "user@example.com", Subject: "s"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("Send waited %v for the relay", elapsed)
	}

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("delivery never started")
	}
	close(release)

	d.Wait()
	select {
	case msg := <-delivered:
		if msg.To != "user@example.com" {
			t.Fatalf("delivered to %q", msg.To)
		}
	default:
		t.Fatal("Wait returned before the delivery finished")
	}
}

func TestDispatcherDeliversOnAContextTheCallerCannotCancel(t *testing.T) {
	// The request context dies the moment the response is written; a delivery
	// that inherited it would be cancelled before it reached the relay.
	seen := make(chan error, 1)
	d := NewDispatcher(SenderFunc(func(ctx context.Context, _ Message) error {
		// Give the caller's cancellation time to propagate if it can.
		time.Sleep(50 * time.Millisecond)
		seen <- ctx.Err()
		return nil
	}), DispatcherOptions{})

	ctx, cancel := context.WithCancel(context.Background())
	if err := d.Send(ctx, Message{To: "user@example.com", Subject: "s"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	cancel()

	d.Wait()
	select {
	case err := <-seen:
		if err != nil {
			t.Fatalf("the background delivery inherited the caller's cancellation: %v", err)
		}
	default:
		t.Fatal("delivery did not run")
	}
}

func TestDispatcherRefusesRatherThanQueueingWithoutLimit(t *testing.T) {
	release := make(chan struct{})
	d := NewDispatcher(SenderFunc(func(context.Context, Message) error {
		<-release
		return nil
	}), DispatcherOptions{MaxInFlight: 1})
	defer func() {
		close(release)
		d.Wait()
	}()

	if err := d.Send(context.Background(), Message{To: "a@example.com"}); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if err := d.Send(context.Background(), Message{To: "b@example.com"}); err == nil {
		t.Fatal("expected the second send to be refused while the first is stuck")
	}
}
