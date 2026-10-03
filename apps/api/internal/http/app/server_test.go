package app

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
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
		{"transfer", d.Transfer},
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

// slowServer runs handler under read and write limits a slow transfer
// outlives, with a transfer budget it does not.
func slowServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := NewServer(ln.Addr().String(), handler, Timeouts{
		Read:     200 * time.Millisecond,
		Write:    200 * time.Millisecond,
		Idle:     time.Second,
		Transfer: 5 * time.Second,
	})
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String()
}

// trickle yields chunks spaced out well past the read limit.
type trickle struct{ left int }

func (r *trickle) Read(p []byte) (int, error) {
	if r.left == 0 {
		return 0, io.EOF
	}
	time.Sleep(150 * time.Millisecond)
	r.left--
	return copy(p, "chunk"), nil
}

func uploadSlowly(t *testing.T, url string) (int, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, &trickle{left: 5})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func countingBody(w http.ResponseWriter, r *http.Request) {
	n, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fmt.Fprint(w, n)
}

// A blob upload that takes longer than the read limit still arrives whole:
// the relay carries bodies up to the size it granted, not ones that fit in
// the budget for a JSON request.
func TestBlobRelayOutlivesTheReadLimit(t *testing.T) {
	base := slowServer(t, http.HandlerFunc(countingBody))
	status, err := uploadSlowly(t, base+"/blobs/some/key")
	if err != nil || status != http.StatusOK {
		t.Fatalf("slow blob upload: status %d, err %v", status, err)
	}
}

// Every other route keeps the read limit.
func TestOtherRoutesKeepTheReadLimit(t *testing.T) {
	base := slowServer(t, http.HandlerFunc(countingBody))
	if status, err := uploadSlowly(t, base+"/calendars"); err == nil && status == http.StatusOK {
		t.Fatal("a slow body on an ordinary route should be cut off by the read limit")
	}
}

// A download is held to the same transfer budget rather than the write limit.
func TestBlobRelayOutlivesTheWriteLimit(t *testing.T) {
	base := slowServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for range 5 {
			time.Sleep(150 * time.Millisecond)
			fmt.Fprint(w, "chunk")
			http.NewResponseController(w).Flush()
		}
	}))
	resp, err := http.Get(base + "/blobs/some/key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != strings.Repeat("chunk", 5) {
		t.Fatalf("slow blob download cut short: %q, %v", body, err)
	}
}
