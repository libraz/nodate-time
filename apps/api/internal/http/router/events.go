package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
)

func registerEvents(g apiGroups, deps Deps) {
	api := g.protected
	evtDeps := newEventDeps(deps)

	// Events
	huma.Register(api, huma.Operation{
		OperationID: "list-events",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/events",
		Summary:     "List events in a calendar",
		Tags:        []string{"Event"},
	}, events.ListEvents(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "get-event",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/events/{eventId}",
		Summary:     "Get an event",
		Tags:        []string{"Event"},
	}, events.GetEvent(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "create-event",
		Method:        http.MethodPost,
		Path:          "/calendars/{calendarId}/events",
		Summary:       "Create an event",
		Tags:          []string{"Event"},
		DefaultStatus: http.StatusCreated,
	}, events.CreateEvent(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-event",
		Method:      http.MethodPut,
		Path:        "/calendars/{calendarId}/events/{eventId}",
		Summary:     "Update an event",
		Tags:        []string{"Event"},
	}, events.UpdateEvent(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "delete-event",
		Method:      http.MethodDelete,
		Path:        "/calendars/{calendarId}/events/{eventId}",
		Summary:     "Delete an event",
		Tags:        []string{"Event"},
	}, events.DeleteEvent(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "set-event-rsvp",
		Method:      http.MethodPut,
		Path:        "/calendars/{calendarId}/events/{eventId}/rsvp",
		Summary:     "Answer an event invitation",
		Tags:        []string{"Event"},
	}, events.SetRsvp(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "set-event-attendee-can-edit",
		Method:      http.MethodPut,
		Path:        "/calendars/{calendarId}/events/{eventId}/attendees/{userId}",
		Summary:     "Delegate editing of an event to a participant",
		Tags:        []string{"Event"},
	}, events.SetAttendeeCanEdit(evtDeps))
}
