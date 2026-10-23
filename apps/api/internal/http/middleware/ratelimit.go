package middleware

import (
	"net/http"
	"net/netip"
	"strconv"
	"time"

	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/observability"
)

// maxRateBuckets caps the clients one limiter tracks.
const maxRateBuckets = 10000

// RateLimiter is a simple in-memory fixed-window limiter keyed by client IP.
// It is intended for low-volume sensitive endpoints (auth, password reset,
// OAuth) to blunt brute-force and mail-bombing; it is process-local and not a
// substitute for an edge limiter in a multi-instance deployment.
type RateLimiter struct {
	counter        *WindowCounter
	scope          string
	limit          int
	trustedProxies []netip.Prefix
}

// NewRateLimiter creates a limiter allowing limit requests per window per
// client IP. trustedProxies lists the reverse-proxy hops allowed to set
// X-Forwarded-For (see ClientIP); pass nil to trust only the direct peer.
//
// scope names this limiter in the metrics. The budgets are separate for a
// reason -- a shared calendar's audience must not be able to exhaust the
// sign-in budget -- and a single rejection count would hide exactly the
// distinction the split exists to make.
func NewRateLimiter(scope string, limit int, window time.Duration, trustedProxies []netip.Prefix) *RateLimiter {
	rl := &RateLimiter{
		counter:        NewWindowCounter(window, maxRateBuckets),
		scope:          scope,
		limit:          limit,
		trustedProxies: trustedProxies,
	}
	return rl
}

// allow records a request for key and reports whether it is within the limit,
// along with the remaining quota and the window reset time.
func (rl *RateLimiter) allow(key string, now time.Time) (ok bool, remaining int, reset time.Time) {
	count, reset := rl.counter.Take(key, now)
	if count > rl.limit {
		return false, 0, reset
	}
	return true, rl.limit - count, reset
}

// RateKey is the budget a client address draws on. An IPv6 client is
// normally handed a whole /64, so each of its addresses is the same client.
func RateKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is6() {
		if p, err := addr.Prefix(64); err == nil {
			return p.String()
		}
	}
	return addr.String()
}

// Middleware returns an http middleware enforcing the limit and advertising the
// X-RateLimit-* headers.
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now()
			ok, remaining, reset := rl.allow(RateKey(ClientIP(r, rl.trustedProxies)), now)
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
				observability.RecordRateLimitRejection(rl.scope)
				// Through the shared writer rather than a hand-written literal,
				// so this envelope cannot drift from the one every handler
				// returns — the client maps a single shape, not two.
				apierrors.WriteSpec(w, apierrors.RateLimited)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
