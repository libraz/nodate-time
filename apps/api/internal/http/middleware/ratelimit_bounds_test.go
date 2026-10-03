package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// However many keys arrive inside one window, the counter holds no more than
// its cap, and the oldest window is the one forgotten.
func TestWindowCounterNeverHoldsMoreThanItsCap(t *testing.T) {
	c := NewWindowCounter(time.Minute, 100)
	now := time.Now()
	for i := range 1000 {
		c.Take(fmt.Sprintf("10.0.%d.%d", i/256, i%256), now)
		if n := c.Len(); n > 100 || n != c.order.Len() {
			t.Fatalf("after %d keys: %d indexed, %d ordered, cap 100", i+1, n, c.order.Len())
		}
	}
	if _, ok := c.keys["10.0.0.0"]; ok {
		t.Error("the first key should have been evicted first")
	}
	if _, ok := c.keys["10.0.3.231"]; !ok {
		t.Error("the latest key should still be held")
	}
}

// Expired windows are dropped as time passes, not only once the cap is hit.
func TestWindowCounterDropsExpiredWindows(t *testing.T) {
	c := NewWindowCounter(time.Minute, 100)
	start := time.Now()
	for i := range 50 {
		c.Take(fmt.Sprintf("10.1.0.%d", i), start)
	}
	if count, _ := c.Take("10.1.0.0", start.Add(30*time.Second)); count != 2 {
		t.Fatalf("a key inside its window counted %d, want 2", count)
	}
	c.Take("10.2.0.1", start.Add(2*time.Minute))
	if n := c.Len(); n != 1 {
		t.Fatalf("%d keys after every earlier window ran out, want 1", n)
	}
}

// Every address in one IPv6 /64 is one client: rotating through them must not
// buy a fresh budget each time.
func TestRateLimiterSharesABudgetAcrossAnIPv6Slash64(t *testing.T) {
	rl := NewRateLimiter("test", 3, time.Minute, nil)
	h := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	statuses := make([]int, 0, 5)
	for i := range 5 {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8:1:2::%x]:443", i+1)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		statuses = append(statuses, rec.Code)
	}
	if statuses[3] != http.StatusTooManyRequests || statuses[4] != http.StatusTooManyRequests {
		t.Fatalf("statuses %v: the fourth and fifth address of one /64 should be over the shared limit", statuses)
	}

	// A different /64 is a different client.
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "[2001:db8:1:3::1]:443"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("another /64 got %d, want its own budget", rec.Code)
	}
}

func TestRateKeyKeepsIPv4AndUnmapsIPv4InIPv6(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.7":          "203.0.113.7",
		"::ffff:203.0.113.7":   "203.0.113.7",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1:2::/64",
		"not-an-ip":            "not-an-ip",
	} {
		if got := rateKey(in); got != want {
			t.Errorf("rateKey(%q) = %q, want %q", in, got, want)
		}
	}
}
