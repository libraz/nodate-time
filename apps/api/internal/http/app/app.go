// Package app assembles the HTTP handler the process serves.
//
// It exists so that there is exactly one description of the stack. The routes
// alone are not the server: security headers, CORS and panic recovery all sit
// outside them, and a test that mounts only the routes exercises something the
// deployment never runs — a class of defect that is invisible until production.
package app

import (
	"net/http"

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
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
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

// NewHandler builds the full stack: recovery, then security headers, then
// CORS, then the routes.
//
// Recovery is outermost so it also covers a panic raised by another middleware,
// and so it runs before anything has been written.
func NewHandler(deps router.Deps, opts Options) http.Handler {
	outer := chi.NewRouter()
	outer.Use(middleware.Recoverer())
	outer.Use(middleware.SecurityHeaders())
	outer.Use(cors.Handler(corsOptions(opts.CORSAllowedOrigins)))
	outer.Mount("/", router.Build(deps))
	return outer
}
