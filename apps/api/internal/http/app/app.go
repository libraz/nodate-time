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
	// CORSAllowedOrigins is the exact origin list. Empty allows no origin,
	// which is what a same-origin deployment wants.
	CORSAllowedOrigins []string
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
	outer.Use(cors.Handler(cors.Options{
		AllowedOrigins:   opts.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	outer.Mount("/", router.Build(deps))
	return outer
}
