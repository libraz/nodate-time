package middleware

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type ctxKeyClientIP struct{}

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

// ClientIPMiddleware resolves and stores the client IP for every request, so
// handlers that need to key off it (e.g. per-target rate limiting) do not
// have to repeat the trusted-proxy logic.
func ClientIPMiddleware(trustedProxies []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithClientIP(r.Context(), ClientIP(r, trustedProxies))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
