package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// rateBucket is a fixed-window counter for a single client key.
type rateBucket struct {
	count       int
	windowStart time.Time
}

// RateLimiter is a simple in-memory fixed-window limiter keyed by client IP.
// It is intended for low-volume sensitive endpoints (auth, password reset,
// OAuth) to blunt brute-force and mail-bombing; it is process-local and not a
// substitute for an edge limiter in a multi-instance deployment.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	limit   int
	window  time.Duration
}

// NewRateLimiter creates a limiter allowing limit requests per window per IP.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*rateBucket),
		limit:   limit,
		window:  window,
	}
	return rl
}

// clientIP extracts the direct peer IP. X-Forwarded-For is intentionally not
// trusted here: unless the deployment has explicitly configured trusted proxy
// hops, callers can spoof it to bypass per-client limits.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// allow records a request for key and reports whether it is within the limit,
// along with the remaining quota and the window reset time.
func (rl *RateLimiter) allow(key string, now time.Time) (ok bool, remaining int, reset time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, exists := rl.buckets[key]
	if !exists || now.Sub(b.windowStart) >= rl.window {
		b = &rateBucket{count: 0, windowStart: now}
		rl.buckets[key] = b
		// Opportunistic eviction to bound memory.
		if len(rl.buckets) > 10000 {
			for k, bb := range rl.buckets {
				if now.Sub(bb.windowStart) >= rl.window {
					delete(rl.buckets, k)
				}
			}
		}
	}
	b.count++
	reset = b.windowStart.Add(rl.window)
	if b.count > rl.limit {
		return false, 0, reset
	}
	return true, rl.limit - b.count, reset
}

// Middleware returns an http middleware enforcing the limit and advertising the
// X-RateLimit-* headers.
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now()
			ok, remaining, reset := rl.allow(clientIP(r), now)
			h := w.Header()
			h.Set("X-RateLimit-Limit", strconv.Itoa(rl.limit))
			h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
			if !ok {
				retryAfter := int(time.Until(reset).Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				h.Set("Retry-After", strconv.Itoa(retryAfter))
				h.Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprint(w, `{"status":429,"code":"RATE.LIMITED","message":"Too many requests, please try again later"}`)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
