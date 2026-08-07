package app

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// Every phase has to be bounded. A zero here is not a slower limit, it is no
// limit: the connection is held for as long as its peer cares to hold it.
func TestDefaultTimeoutsBoundEveryPhase(t *testing.T) {
	d := DefaultTimeouts()
	for _, tc := range []struct {
		phase string
		value time.Duration
	}{
		{"read", d.Read},
		{"write", d.Write},
		{"idle", d.Idle},
	} {
		if tc.value <= 0 {
			t.Errorf("%s timeout is %v, which lets a connection be held indefinitely", tc.phase, tc.value)
		}
	}
}

func TestNewServerServesWithTheTimeoutsItWasGiven(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	timeouts := Timeouts{Read: time.Second, Write: 2 * time.Second, Idle: 3 * time.Second}

	srv := NewServer(":8080", handler, timeouts)
	if srv.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", srv.Addr)
	}
	if srv.ReadTimeout != timeouts.Read {
		t.Errorf("ReadTimeout = %v, want %v", srv.ReadTimeout, timeouts.Read)
	}
	if srv.WriteTimeout != timeouts.Write {
		t.Errorf("WriteTimeout = %v, want %v", srv.WriteTimeout, timeouts.Write)
	}
	if srv.IdleTimeout != timeouts.Idle {
		t.Errorf("IdleTimeout = %v, want %v", srv.IdleTimeout, timeouts.Idle)
	}
	if srv.Handler == nil {
		t.Error("Handler is nil; the server would answer 404 to everything")
	}
}

// And the read budget is real, not just recorded: a request that stops halfway
// through has to lose its connection. Held open, a handful of them costs the
// server every connection it has, with no request ever reaching a handler.
func TestServerDropsARequestThatNeverFinishesArriving(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := NewServer(ln.Addr().String(), http.NotFoundHandler(), Timeouts{
		Read:  150 * time.Millisecond,
		Write: time.Second,
		Idle:  150 * time.Millisecond,
	})
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// A request line and a header, but never the blank line that ends them.
	if _, err := fmt.Fprintf(conn, "GET /health HTTP/1.1\r\nHost: test\r\n"); err != nil {
		t.Fatalf("write partial request: %v", err)
	}

	// Well past the read budget: reaching this deadline means the server was
	// still waiting for the rest of a request that is never coming.
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("the connection was held past the read timeout: %v", err)
	}
}
