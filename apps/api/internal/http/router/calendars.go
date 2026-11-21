package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/calendars"
)

func registerCalendars(g apiGroups, deps Deps) {
	api := g.protected
	calDeps := calendars.Deps{DB: deps.DB, Queries: deps.Queries, Storage: deps.Storage, WorkspaceID: deps.WorkspaceID, AppName: appNameOf(deps)}

	// Calendars
	huma.Register(api, huma.Operation{
		OperationID: "list-calendars",
		Method:      http.MethodGet,
		Path:        "/calendars",
		Summary:     "List calendars for current user",
		Tags:        []string{"Calendar"},
	}, calendars.ListCalendars(calDeps))

	huma.Register(api, huma.Operation{
		OperationID: "get-calendar",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}",
		Summary:     "Get a calendar",
		Tags:        []string{"Calendar"},
	}, calendars.GetCalendar(calDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "create-calendar",
		Method:        http.MethodPost,
		Path:          "/calendars",
		Summary:       "Create a calendar",
		Tags:          []string{"Calendar"},
		DefaultStatus: http.StatusCreated,
		Errors:        errorStatuses(apierrors.CalendarLimitReached),
	}, calendars.CreateCalendar(calDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-calendar",
		Method:      http.MethodPatch,
		Path:        "/calendars/{calendarId}",
		Summary:     "Update a calendar",
		Tags:        []string{"Calendar"},
		Errors:      calendarWriteErrors(),
	}, calendars.UpdateCalendar(calDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "presign-calendar-cover",
		Method:        http.MethodPost,
		Path:          "/calendars/{calendarId}/cover/presign",
		Summary:       "Reserve an upload for a shared calendar cover image",
		Tags:          []string{"Calendar"},
		DefaultStatus: http.StatusCreated,
		Errors: errorStatuses(apierrors.CalendarCoverForbidden, apierrors.CalendarCoverTooLarge,
			apierrors.CalendarCoverConflict, apierrors.UploadDigestMismatch, apierrors.StorageUnavailable),
	}, calendars.PresignCalendarCover(calDeps))

	huma.Register(api, huma.Operation{
		OperationID: "confirm-calendar-cover",
		Method:      http.MethodPut,
		Path:        "/calendars/{calendarId}/cover",
		Summary:     "Confirm an uploaded image as the shared calendar cover",
		Tags:        []string{"Calendar"},
		Errors: errorStatuses(apierrors.CalendarCoverForbidden, apierrors.CalendarCoverTooLarge,
			apierrors.CalendarCoverInvalid, apierrors.CalendarCoverConflict, apierrors.UploadDigestMismatch,
			apierrors.UploadTooLarge, apierrors.StorageUnavailable, apierrors.NotFound),
	}, calendars.PutCalendarCover(calDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-calendar-cover",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/cover",
		Summary:       "Remove a calendar cover image",
		Tags:          []string{"Calendar"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errorStatuses(apierrors.CalendarCoverForbidden),
	}, calendars.DeleteCalendarCover(calDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-calendar",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}",
		Summary:       "Delete a calendar",
		Tags:          []string{"Calendar"},
		DefaultStatus: http.StatusNoContent,
		Errors:        calendarWriteErrors(),
	}, calendars.DeleteCalendar(calDeps))

	// Calendar members
	huma.Register(api, huma.Operation{
		OperationID: "list-members",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/members",
		Summary:     "List calendar members",
		Tags:        []string{"Member"},
	}, calendars.ListMembers(calDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "add-member",
		Method:        http.MethodPost,
		Path:          "/calendars/{calendarId}/members",
		Summary:       "Add a workspace member to a calendar",
		Tags:          []string{"Member"},
		DefaultStatus: http.StatusCreated,
		Errors:        errorStatuses(apierrors.MemberLimitReached, apierrors.CalendarLimitReached, apierrors.MemberAlreadyAdded),
	}, calendars.AddMember(calDeps))

	huma.Register(api, huma.Operation{
		OperationID: "list-member-candidates",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/member-candidates",
		Summary:     "List workspace members who can be added to a calendar",
		Tags:        []string{"Member"},
	}, calendars.ListMemberCandidates(calDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-member-role",
		Method:      http.MethodPut,
		Path:        "/calendars/{calendarId}/members/{userId}/role",
		Summary:     "Update a member's role",
		Tags:        []string{"Member"},
	}, calendars.UpdateMemberRole(calDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-member-color",
		Method:      http.MethodPut,
		Path:        "/calendars/{calendarId}/members/{userId}/color",
		Summary:     "Change the colour a member's layer is drawn in",
		Tags:        []string{"Member"},
	}, calendars.UpdateMemberColor(calDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "remove-member",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/members/{userId}",
		Summary:       "Remove a member from a calendar",
		Tags:          []string{"Member"},
		DefaultStatus: http.StatusNoContent,
	}, calendars.RemoveMember(calDeps))

	// Calendar labels
	huma.Register(api, huma.Operation{
		OperationID: "list-labels",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/labels",
		Summary:     "List calendar labels (colors)",
		Tags:        []string{"Label"},
	}, calendars.ListLabels(calDeps))

	// Export / Import
	huma.Register(api, huma.Operation{
		OperationID: "export-events",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/export",
		Summary:     "Export calendar events as iCal/CSV",
		Tags:        []string{"Calendar"},
		Errors:      errorStatuses(apierrors.ExportRangeTooLarge),
	}, calendars.ExportEvents(calDeps))

	huma.Register(api, huma.Operation{
		OperationID:  "import-events",
		Method:       http.MethodPost,
		Path:         "/calendars/{calendarId}/import",
		Summary:      "Import events from iCal text",
		Tags:         []string{"Calendar"},
		MaxBodyBytes: 12 * 1024 * 1024,
	}, calendars.ImportEvents(calDeps))
}

// calendarWriteErrors lists the statuses of a conditional calendar write.
func calendarWriteErrors() []int {
	return preconditionedErrors(apierrors.CalendarStale, apierrors.CalendarAccessDenied,
		apierrors.CalendarRoleRequired, apierrors.Validation)
}
