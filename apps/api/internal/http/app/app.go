// Package app assembles the HTTP handler the process serves.
//
// It exists so that there is exactly one description of the stack. The routes
// alone are not the server: security headers, CORS and panic recovery all sit
// outside them, and a test that mounts only the routes exercises something the
// deployment never runs — a class of defect that is invisible until production.
package app

import (
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
)

// Options carries the settings the outer layers need. Everything the routes
// themselves need is in router.Deps.
type Options struct {
	// CORSAllowedOrigins is the exact origin list. Empty denies every origin,
	// which is what a same-origin deployment wants -- see corsOptions for why
	// that takes saying so explicitly.
	CORSAllowedOrigins []string
	// AccessLogger receives one line per request. Nil disables the access log
	// while leaving the metrics in place, which is what the tests want: a
	// suite that builds a server per test would otherwise bury its own
	// failures under the traffic it generates.
	AccessLogger *slog.Logger
	// TrustedProxies lists the hops allowed to supply an inbound request
	// identifier. It is the same list router.Deps carries for rate limiting;
	// it is repeated here because the identifier is stamped before the routes
	// are reached.
	TrustedProxies []netip.Prefix
}

// corsOptions translates the origin list into a CORS configuration.
//
// An empty list has to be spelled out as a denial: the library reads "no
// origins configured" as "every origin", and answers a wildcard. That would
// leave the unauthenticated routes -- sign-in and public share links -- open to
// being read and driven from any page, and config validation only guards
// against it in production.
func corsOptions(allowedOrigins []string) cors.Options {
	opts := cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		// If-Match carries the revision an event save was built on.
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "If-Match"},
		// ETag names that revision, Content-Disposition the export's file
		// name, X-Result-Truncated a capped listing or import.
		ExposedHeaders: []string{
			"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset",
			"ETag", "Content-Disposition", "X-Result-Truncated",
		},
		AllowCredentials: true,
		MaxAge:           300,
	}
	if len(allowedOrigins) == 0 {
		// The list is only consulted when no function is set, so this is also
		// what keeps the empty list from becoming the wildcard.
		opts.AllowOriginFunc = func(*http.Request, string) bool { return false }
	}
	return opts
}

// NewHandler builds the full stack: request identity and measurement, then
// recovery, then security headers, then CORS, then the routes.
//
// Recovery sits outside everything that can fail on behalf of a request, so it
// also covers a panic raised by another middleware and runs before anything
// has been written. The two observability middlewares are the only ones
// outside it, and deliberately so: a request that panicked is one of the
// requests worth counting, and counting it from within the recovery would
// record the status as whatever had been written before the panic -- nothing.
func NewHandler(deps router.Deps, opts Options) http.Handler {
	outer := chi.NewRouter()
	outer.Use(middleware.RequestID(opts.TrustedProxies))
	outer.Use(middleware.Observe(opts.AccessLogger))
	outer.Use(middleware.Recoverer())
	outer.Use(middleware.SecurityHeaders())
	outer.Use(cors.Handler(corsOptions(opts.CORSAllowedOrigins)))
	outer.Mount("/", router.Build(deps))
	return outer
}
