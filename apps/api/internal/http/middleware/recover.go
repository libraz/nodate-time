package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
)

// Recoverer turns a panic in a handler into the ordinary error envelope.
//
// Without it a panic unwinds into net/http's per-connection guard, which
// closes the connection without writing a response: the browser reports a
// network error rather than a failure, the client's error handling never runs,
// and the stack goes to stderr while every other log line goes to stdout as
// JSON. Neither chi's router nor Huma recovers around a handler, so this is
// the only place it can happen.
//
// http.ErrAbortHandler is the standard way to abandon a response deliberately
// and is re-panicked so net/http still sees it.
func Recoverer() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.ErrorContext(r.Context(), "panic recovered",
					"method", r.Method,
					"path", r.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				apierrors.WriteSpec(w, apierrors.InternalUnexpected)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
