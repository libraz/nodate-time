package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
)

func registerEventDetails(g apiGroups, deps Deps) {
	api := g.protected
	evtDeps := newEventDeps(deps)

	// Comments (activities)
	huma.Register(api, huma.Operation{
		OperationID: "list-comments",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/events/{eventId}/activities",
		Summary:     "List event comments",
		Tags:        []string{"Comment"},
	}, events.ListComments(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "create-comment",
		Method:      http.MethodPost,
		Path:        "/calendars/{calendarId}/events/{eventId}/activities",
		Summary:     "Create a comment on an event",
		Tags:        []string{"Comment"},
	}, events.CreateComment(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-comment",
		Method:      http.MethodPut,
		Path:        "/calendars/{calendarId}/events/{eventId}/activities/{commentId}",
		Summary:     "Update a comment",
		Tags:        []string{"Comment"},
	}, events.UpdateComment(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-comment",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/events/{eventId}/activities/{commentId}",
		Summary:       "Delete a comment",
		Tags:          []string{"Comment"},
		DefaultStatus: 204,
	}, events.DeleteComment(evtDeps))

	// Checklist items
	huma.Register(api, huma.Operation{
		OperationID: "list-checklist-items",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/events/{eventId}/checklist",
		Summary:     "List checklist items for an event",
		Tags:        []string{"Checklist"},
	}, events.ListChecklistItems(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "create-checklist-item",
		Method:      http.MethodPost,
		Path:        "/calendars/{calendarId}/events/{eventId}/checklist",
		Summary:     "Create a checklist item",
		Tags:        []string{"Checklist"},
	}, events.CreateChecklistItem(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-checklist-item",
		Method:      http.MethodPut,
		Path:        "/calendars/{calendarId}/events/{eventId}/checklist/{itemId}",
		Summary:     "Update a checklist item",
		Tags:        []string{"Checklist"},
	}, events.UpdateChecklistItem(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-checklist-item",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/events/{eventId}/checklist/{itemId}",
		Summary:       "Delete a checklist item",
		Tags:          []string{"Checklist"},
		DefaultStatus: 204,
	}, events.DeleteChecklistItem(evtDeps))

	// Attachments
	huma.Register(api, huma.Operation{
		OperationID: "list-attachments",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/events/{eventId}/attachments",
		Summary:     "List event attachments",
		Tags:        []string{"Attachment"},
	}, events.ListAttachments(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "presign-upload",
		Method:      http.MethodPost,
		Path:        "/calendars/{calendarId}/events/{eventId}/attachments/presign",
		Summary:     "Get a presigned URL for uploading a file",
		Tags:        []string{"Attachment"},
	}, events.PresignUpload(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "confirm-attachment",
		Method:      http.MethodPost,
		Path:        "/calendars/{calendarId}/events/{eventId}/attachments/{attachmentId}/confirm",
		Summary:     "Confirm a previously uploaded attachment",
		Tags:        []string{"Attachment"},
	}, events.ConfirmAttachment(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "get-attachment-download",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/events/{eventId}/attachments/{attachmentId}/download",
		Summary:     "Get a presigned download URL",
		Tags:        []string{"Attachment"},
	}, events.GetAttachmentDownload(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-attachment",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/events/{eventId}/attachments/{attachmentId}",
		Summary:       "Delete an attachment",
		Tags:          []string{"Attachment"},
		DefaultStatus: 204,
	}, events.DeleteAttachment(evtDeps))
}
