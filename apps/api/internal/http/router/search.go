package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
)

// registerSearch mounts event search.
func registerSearch(g apiGroups, deps Deps) {
	huma.Register(g.protected, huma.Operation{
		OperationID: "search-events",
		Method:      http.MethodGet,
		Path:        "/search",
		Summary:     "Search events across the caller's calendars",
		Tags:        []string{"Event"},
		Errors:      errorStatuses(apierrors.Validation, apierrors.SearchTooBroad),
	}, events.SearchEvents(newEventDeps(deps)))
}
