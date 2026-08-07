package mailer

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// blackHoleListener accepts connections and then says nothing, the way a relay
// behind a dropped route looks to a client that has completed the handshake.
// Without a deadline a client waits on the greeting for the OS TCP timeout.
func blackHoleListener(t *testing.T) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})

	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func TestSMTPSendAbortsWhenTheContextIsCancelled(t *testing.T) {
	host, port := blackHoleListener(t)
	// A timeout far longer than the test: only the cancellation can end this.
	m := SMTPMailer{cfg: SMTPConfig{Host: host, Port: port, From: "no-reply@example.com", Timeout: time.Minute}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := m.Send(ctx, Message{To: "user@example.com", Subject: "s", Text: "body"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from a send against a silent relay")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancellation to surface, got %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("cancelling took %v to reach the send; it did not abort it", elapsed)
	}
}

func TestSMTPSendTimesOutWhenTheCallerSetsNoDeadline(t *testing.T) {
	host, port := blackHoleListener(t)
	m := SMTPMailer{cfg: SMTPConfig{Host: host, Port: port, From: "no-reply@example.com", Timeout: 200 * time.Millisecond}}

	start := time.Now()
	err := m.Send(context.Background(), Message{To: "user@example.com", Subject: "s", Text: "body"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from a send against a silent relay")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the mailer's own deadline to end the send, got %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("send ran for %v with a 200ms budget", elapsed)
	}
}

func TestSMTPSendHasATimeoutByDefault(t *testing.T) {
	if defaultSMTPTimeout <= 0 || defaultSMTPTimeout > time.Minute {
		t.Fatalf("default send timeout is not a sensible bound: %v", defaultSMTPTimeout)
	}
}
