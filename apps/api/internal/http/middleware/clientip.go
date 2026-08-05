package middleware

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type (
	ctxKeyClientIP  struct{}
	ctxKeyUserAgent struct{}
)

// ClientIP resolves the client IP for a request. If the direct TCP peer is
// listed in trustedProxies, the rightmost X-Forwarded-For hop that is not
// itself a trusted proxy is used (the standard way to defeat a client
// prepending its own spoofed entries); otherwise X-Forwarded-For is ignored
// entirely and the direct peer is returned, so a client behind no configured
// proxy cannot spoof its way past per-client limits.
func ClientIP(r *http.Request, trustedProxies []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !addrIsTrusted(peer, trustedProxies) {
		return host
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return host
	}
	hops := strings.Split(xff, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(hops[i])
		addr, err := netip.ParseAddr(candidate)
		if err != nil {
			continue
		}
		if !addrIsTrusted(addr, trustedProxies) {
			return candidate
		}
	}
	// Every hop, including the client-supplied leftmost one, claimed to be a
	// trusted proxy — fall back to the direct peer rather than trusting that.
	return host
}

func addrIsTrusted(addr netip.Addr, trustedProxies []netip.Prefix) bool {
	for _, p := range trustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// WithClientIP stores the resolved client IP in the context.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ctxKeyClientIP{}, ip)
}

// ClientIPFromContext retrieves the client IP stored by ClientIPMiddleware.
func ClientIPFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyClientIP{}).(string)
	return v, ok
}

// maxUserAgentLen bounds what is stored. The column is finite and a client
// controls this header, so it is cut rather than trusted whole.
const maxUserAgentLen = 255

// WithUserAgent stores the client hint for the current request.
func WithUserAgent(ctx context.Context, ua string) context.Context {
	return context.WithValue(ctx, ctxKeyUserAgent{}, ua)
}

// UserAgentFromContext retrieves the client hint stored by ClientIPMiddleware.
func UserAgentFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyUserAgent{}).(string)
	return v, ok
}

// ClientIPMiddleware resolves and stores the client IP for every request, so
// handlers that need to key off it (e.g. per-target rate limiting) do not
// have to repeat the trusted-proxy logic.
//
// It also carries the User-Agent through, for the same reason: a session row
// records where a sign-in came from, and a handler built on huma's typed
// inputs has no other route to a raw header it did not declare. Without it
// every session in the list reads identically, which makes the list useless
// for the one thing it is for -- recognising a device that is not yours.
func ClientIPMiddleware(trustedProxies []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithClientIP(r.Context(), ClientIP(r, trustedProxies))
			ua := r.UserAgent()
			if len(ua) > maxUserAgentLen {
				ua = ua[:maxUserAgentLen]
			}
			ctx = WithUserAgent(ctx, ua)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
