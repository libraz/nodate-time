package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
)

// subresourceErrors are the refusals every operation on an event's comments,
// checklist or attachments can answer with, before its own.
var subresourceErrors = []*apierrors.Spec{apierrors.CalendarNotFound, apierrors.EventNotFound}

// subresourceWriteErrors adds those of a write by a member of the calendar.
var subresourceWriteErrors = append([]*apierrors.Spec{apierrors.CalendarRoleRequired, apierrors.EventEditDenied}, subresourceErrors...)

// withWriteErrors is subresourceWriteErrors plus an operation's own.
func withWriteErrors(own ...*apierrors.Spec) []*apierrors.Spec {
	return append(append([]*apierrors.Spec{}, subresourceWriteErrors...), own...)
}

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
		Errors:      errorStatuses(subresourceErrors...),
	}, events.ListComments(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "create-comment",
		Method:        http.MethodPost,
		Path:          "/calendars/{calendarId}/events/{eventId}/activities",
		Summary:       "Create a comment on an event",
		Tags:          []string{"Comment"},
		Errors:        errorStatuses(subresourceWriteErrors...),
		DefaultStatus: http.StatusCreated,
	}, events.CreateComment(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-comment",
		Method:      http.MethodPatch,
		Path:        "/calendars/{calendarId}/events/{eventId}/activities/{commentId}",
		Summary:     "Update a comment",
		Tags:        []string{"Comment"},
		Errors:      preconditionedErrors(withWriteErrors(apierrors.CommentStale, apierrors.CommentNotFound, apierrors.CommentAccessDenied)...),
	}, events.UpdateComment(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-comment",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/events/{eventId}/activities/{commentId}",
		Summary:       "Delete a comment",
		Tags:          []string{"Comment"},
		Errors:        preconditionedErrors(withWriteErrors(apierrors.CommentStale, apierrors.CommentNotFound, apierrors.CommentAccessDenied)...),
		DefaultStatus: 204,
	}, events.DeleteComment(evtDeps))

	// Checklist items
	huma.Register(api, huma.Operation{
		OperationID: "list-checklist-items",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/events/{eventId}/checklist",
		Summary:     "List checklist items for an event",
		Tags:        []string{"Checklist"},
		Errors:      errorStatuses(subresourceErrors...),
	}, events.ListChecklistItems(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "create-checklist-item",
		Method:        http.MethodPost,
		Path:          "/calendars/{calendarId}/events/{eventId}/checklist",
		Summary:       "Create a checklist item",
		Tags:          []string{"Checklist"},
		Errors:        errorStatuses(append([]*apierrors.Spec{apierrors.ChecklistLimitReached}, subresourceWriteErrors...)...),
		DefaultStatus: http.StatusCreated,
	}, events.CreateChecklistItem(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-checklist-item",
		Method:      http.MethodPatch,
		Path:        "/calendars/{calendarId}/events/{eventId}/checklist/{itemId}",
		Summary:     "Update a checklist item",
		Tags:        []string{"Checklist"},
		Errors:      preconditionedErrors(withWriteErrors(apierrors.ChecklistStale, apierrors.ChecklistItemNotFound)...),
	}, events.UpdateChecklistItem(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-checklist-item",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/events/{eventId}/checklist/{itemId}",
		Summary:       "Delete a checklist item",
		Tags:          []string{"Checklist"},
		Errors:        preconditionedErrors(withWriteErrors(apierrors.ChecklistStale, apierrors.ChecklistItemNotFound)...),
		DefaultStatus: 204,
	}, events.DeleteChecklistItem(evtDeps))

	// Attachments
	huma.Register(api, huma.Operation{
		OperationID: "list-attachments",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/events/{eventId}/attachments",
		Summary:     "List event attachments",
		Tags:        []string{"Attachment"},
		Errors:      errorStatuses(subresourceErrors...),
	}, events.ListAttachments(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "presign-upload",
		Method:      http.MethodPost,
		Path:        "/calendars/{calendarId}/events/{eventId}/attachments/presign",
		Summary:     "Get a presigned URL for uploading a file",
		Tags:        []string{"Attachment"},
		Errors: errorStatuses(append([]*apierrors.Spec{
			apierrors.AttachmentLimitReached, apierrors.AttachmentTooLarge, apierrors.StorageUnavailable,
		}, subresourceWriteErrors...)...),
		DefaultStatus: http.StatusCreated,
	}, events.PresignUpload(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "confirm-attachment",
		Method:      http.MethodPost,
		Path:        "/calendars/{calendarId}/events/{eventId}/attachments/{attachmentId}/confirm",
		Summary:     "Confirm a previously uploaded attachment",
		Tags:        []string{"Attachment"},
		Errors: errorStatuses(append([]*apierrors.Spec{
			apierrors.AttachmentNotFound, apierrors.AttachmentLimitReached,
			apierrors.UploadDigestMismatch, apierrors.UploadTooLarge, apierrors.StorageUnavailable,
		}, subresourceWriteErrors...)...),
	}, events.ConfirmAttachment(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID: "get-attachment-download",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/events/{eventId}/attachments/{attachmentId}/download",
		Summary:     "Get a presigned download URL",
		Tags:        []string{"Attachment"},
		Errors:      errorStatuses(append([]*apierrors.Spec{apierrors.AttachmentNotFound, apierrors.StorageUnavailable}, subresourceErrors...)...),
	}, events.GetAttachmentDownload(evtDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-attachment",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/events/{eventId}/attachments/{attachmentId}",
		Summary:       "Delete an attachment",
		Tags:          []string{"Attachment"},
		Errors:        errorStatuses(append([]*apierrors.Spec{apierrors.AttachmentNotFound}, subresourceWriteErrors...)...),
		DefaultStatus: 204,
	}, events.DeleteAttachment(evtDeps))
}
