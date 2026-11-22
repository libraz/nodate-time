package router

import (
	"github.com/go-chi/chi/v5"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"

	_ "github.com/emersion/go-webdav/caldav"
)

// mountDAV mounts the CalDAV endpoint on the bare mux. rateLimit is the
// resolved per-IP budget per minute; zero or negative means no limiter.
func mountDAV(r chi.Router, deps Deps, eventDeps events.Deps, rateLimit int) {
}
