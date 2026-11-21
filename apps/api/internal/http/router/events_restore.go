package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
)

// registerEventRestore mounts restoring a deleted event and undoing an
// occurrence exception.
func registerEventRestore(g apiGroups, deps Deps) {
	api := g.protected
	evtDeps := newEventDeps(deps)

	huma.Register(api, huma.Operation{
		OperationID: "restore-event",
		Method:      http.MethodPost,
		Path:        "/calendars/{calendarId}/events/{eventId}/restore",
		Summary:     "Restore a deleted event",
		Tags:        []string{"Event"},
		Errors: errorStatuses(apierrors.EventNotFound, apierrors.EventEditDenied, apierrors.EventNotDeleted,
			apierrors.EventRestoreExpired, apierrors.EventRestoreConflict, apierrors.EventTaskOwned),
	}, events.RestoreEvent(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "undo-event-exception",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/events/{eventId}/exceptions/{occurrenceDate}",
		Summary:       "Put a cancelled occurrence back",
		Tags:          []string{"Event"},
		DefaultStatus: http.StatusNoContent,
		Errors: preconditionedErrors(apierrors.EventNotFound, apierrors.EventEditDenied, apierrors.EventStale,
			apierrors.EventNotRecurring, apierrors.EventExceptionNotFound, apierrors.EventDateInvalid),
	}, events.UndoException(evtDeps))
}
