package router

import (
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/libraz/nodate-time/apps/api/internal/dav"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// mountDAV mounts the CalDAV endpoint on the bare mux. rateLimit is the
// resolved per-IP budget per minute; zero or negative means no limiter.
func mountDAV(r chi.Router, deps Deps, eventDeps events.Deps, rateLimit int) {
	r.Group(func(g chi.Router) {
		if rateLimit > 0 {
			g.Use(middleware.NewRateLimiter("dav", rateLimit, time.Minute, deps.TrustedProxies).Middleware())
		}
		g.Mount(dav.MountPath, dav.Handler(dav.Deps{
			Queries:      deps.Queries,
			WorkspaceID:  deps.WorkspaceID,
			AppName:      appNameOf(deps),
			APIPublicURL: deps.APIPublicURL,
			Events:       eventDeps,
		}))
	})
}
