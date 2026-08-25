package middleware

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/libraz/nodate-time/apps/api/internal/observability"
)

// RequestID gives every request an identifier, answers it in the response, and
// puts it in the context where the log handler picks it up.
//
// An identifier supplied by the caller is adopted only when the caller is a
// configured reverse proxy, so that a deployment whose proxy already stamps
// one keeps a single value across both logs. From anyone else it is ignored
// rather than trusted: an identifier a client chooses is one a client can
// reuse, and two unrelated requests sharing an id is worse than neither having
// the proxy's.
func RequestID(trustedProxies []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := ""
			if peerIsTrustedProxy(r, trustedProxies) {
				if candidate, ok := observability.SanitizeRequestID(r.Header.Get(observability.RequestIDHeader)); ok {
					id = candidate
				}
			}
			if id == "" {
				id = observability.NewRequestID()
			}
			w.Header().Set(observability.RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(observability.WithRequestID(r.Context(), id)))
		})
	}
}

// peerIsTrustedProxy reports whether the direct TCP peer is a configured hop.
func peerIsTrustedProxy(r *http.Request, trustedProxies []netip.Prefix) bool {
	if len(trustedProxies) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addrIsTrusted(peer.Unmap(), trustedProxies)
}

// Observe measures every request and, when logger is non-nil, writes one line
// per request describing it.
//
// What it does not write is the URL. The paths this application serves carry
// credentials in them -- a share token is a path segment, a password reset and
// an OAuth callback carry theirs in the query -- so a conventional access log
// would file a working credential in plain text, in the one place that is
// routinely copied to a third party, kept longest, and read by people who were
// never given the calendar. The matched route pattern says everything the log
// is for (which endpoint, how often, how slow) and none of that; the request
// identifier is what links the line to the rest of the story.
//
// It is mounted outside panic recovery so that a request that panicked is
// still counted, and counted with the status the recovery actually wrote.
func Observe(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			done := observability.RequestStarted()
			start := time.Now()
			rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}

			defer func() {
				done()
				elapsed := time.Since(start)
				method := observability.NormalizeMethod(r.Method)
				route := observability.NormalizeRoute(routePattern(r))
				observability.RecordRequest(method, route, rec.status, elapsed.Seconds(), rec.written)

				if logger == nil || (route == healthRoute && rec.status == http.StatusOK) {
					// A liveness probe answering normally is the most frequent
					// request a deployment serves and the least informative;
					// logged, it is what the log mostly consists of. A failing
					// one is the opposite and falls through.
					return
				}
				logger.LogAttrs(r.Context(), levelForStatus(rec.status), "request served",
					slog.String("method", r.Method),
					slog.String("route", route),
					slog.Int("status", rec.status),
					slog.Int64("duration_ms", elapsed.Milliseconds()),
					slog.Int64("bytes", rec.written),
					slog.String("client_ip", clientIPForLog(r)),
				)
			}()

			next.ServeHTTP(rec, r)
		})
	}
}

// healthRoute is the liveness endpoint's pattern, excluded from the log.
const healthRoute = "/health"

// routePattern reports the route the router matched, or the empty string when
// nothing did. It is only meaningful after the handler has run.
func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		return rctx.RoutePattern()
	}
	return ""
}

// clientIPForLog reuses the address the rate limiter keys on, so a line in the
// log and a bucket in the limiter name the same client. ClientIPMiddleware
// runs further in, hence the fallback for the requests that never reached it.
func clientIPForLog(r *http.Request) string {
	if ip, ok := ClientIPFromContext(r.Context()); ok {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// levelForStatus keeps the log readable at a glance: a 5xx is the server's
// fault and is an error, a 4xx is a client being refused and is worth seeing
// but is not a fault, and everything else is routine.
func levelForStatus(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// responseRecorder remembers what was answered. net/http offers no way to ask
// a ResponseWriter after the fact, so the status and the size have to be
// observed as they are written.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	written     int64
	wroteHeader bool
}

func (rec *responseRecorder) WriteHeader(status int) {
	if rec.wroteHeader {
		return
	}
	rec.status = status
	rec.wroteHeader = true
	rec.ResponseWriter.WriteHeader(status)
}

func (rec *responseRecorder) Write(b []byte) (int, error) {
	if !rec.wroteHeader {
		// net/http's implicit 200 on first write, made explicit so the
		// recorded status matches what the client receives.
		rec.WriteHeader(http.StatusOK)
	}
	n, err := rec.ResponseWriter.Write(b)
	rec.written += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer, so a
// handler that sets a per-response deadline or flushes still can.
func (rec *responseRecorder) Unwrap() http.ResponseWriter { return rec.ResponseWriter }

func (rec *responseRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rec *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rec.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("responseRecorder: underlying writer does not support hijacking")
}
