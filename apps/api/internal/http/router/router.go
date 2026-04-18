package router

import (
	"database/sql"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/calendars"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/invites"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/memos"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

type Deps struct {
	DB        *sql.DB
	Queries   *generated.Queries
	JWTSecret string
	Storage   *storage.Client
}

func Build(deps Deps) http.Handler {
	r := chi.NewRouter()

	// Health check
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// --- Public routes (no auth) ---
	r.Group(func(pub chi.Router) {
		api := humachi.New(pub, huma.DefaultConfig("Nodate Time", "1.0.0"))

		userDeps := users.Deps{Queries: deps.Queries, JWTSecret: deps.JWTSecret}

		huma.Register(api, huma.Operation{
			OperationID: "register",
			Method:      http.MethodPost,
			Path:        "/auth/register",
			Summary:     "Register a new user",
			Tags:        []string{"Auth"},
		}, users.Register(userDeps))

		huma.Register(api, huma.Operation{
			OperationID: "login",
			Method:      http.MethodPost,
			Path:        "/auth/login",
			Summary:     "Login with email and password",
			Tags:        []string{"Auth"},
		}, users.Login(userDeps))

		// Public share (no auth)
		invPubDeps := invites.Deps{DB: deps.DB, Queries: deps.Queries}

		huma.Register(api, huma.Operation{
			OperationID: "public-calendar",
			Method:      http.MethodGet,
			Path:        "/share/{token}",
			Summary:     "Get calendar info via share token",
			Tags:        []string{"Share"},
		}, invites.PublicCalendar(invPubDeps))

		huma.Register(api, huma.Operation{
			OperationID: "public-events",
			Method:      http.MethodGet,
			Path:        "/share/{token}/events",
			Summary:     "List events via share token",
			Tags:        []string{"Share"},
		}, invites.PublicEvents(invPubDeps))
	})

	// --- Protected routes (require auth) ---
	r.Group(func(prot chi.Router) {
		prot.Use(middleware.RequireAuth(deps.JWTSecret))
		api := humachi.New(prot, huma.DefaultConfig("Nodate Time", "1.0.0"))

		userDeps := users.Deps{Queries: deps.Queries, JWTSecret: deps.JWTSecret}
		calDeps := calendars.Deps{DB: deps.DB, Queries: deps.Queries}
		evtDeps := events.Deps{DB: deps.DB, Queries: deps.Queries, Storage: deps.Storage}
		memoDeps := memos.Deps{Queries: deps.Queries}
		invDeps := invites.Deps{DB: deps.DB, Queries: deps.Queries}

		// User
		huma.Register(api, huma.Operation{
			OperationID: "get-me",
			Method:      http.MethodGet,
			Path:        "/user",
			Summary:     "Get current user",
			Tags:        []string{"User"},
		}, users.GetMe(userDeps))

		huma.Register(api, huma.Operation{
			OperationID: "update-me",
			Method:      http.MethodPut,
			Path:        "/user",
			Summary:     "Update current user",
			Tags:        []string{"User"},
		}, users.UpdateMe(userDeps))

		huma.Register(api, huma.Operation{
			OperationID:   "change-password",
			Method:        http.MethodPut,
			Path:          "/user/password",
			Summary:       "Change current user password",
			Tags:          []string{"User"},
			DefaultStatus: 204,
		}, users.ChangePassword(userDeps))

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
			OperationID: "create-calendar",
			Method:      http.MethodPost,
			Path:        "/calendars",
			Summary:     "Create a calendar",
			Tags:        []string{"Calendar"},
		}, calendars.CreateCalendar(calDeps))

		huma.Register(api, huma.Operation{
			OperationID: "update-calendar",
			Method:      http.MethodPut,
			Path:        "/calendars/{calendarId}",
			Summary:     "Update a calendar",
			Tags:        []string{"Calendar"},
		}, calendars.UpdateCalendar(calDeps))

		huma.Register(api, huma.Operation{
			OperationID: "delete-calendar",
			Method:      http.MethodDelete,
			Path:        "/calendars/{calendarId}",
			Summary:     "Delete a calendar",
			Tags:        []string{"Calendar"},
		}, calendars.DeleteCalendar(calDeps))

		// Calendar members
		huma.Register(api, huma.Operation{
			OperationID: "list-members",
			Method:      http.MethodGet,
			Path:        "/calendars/{calendarId}/members",
			Summary:     "List calendar members",
			Tags:        []string{"Member"},
		}, calendars.ListMembers(calDeps))

		// Calendar labels
		huma.Register(api, huma.Operation{
			OperationID: "list-labels",
			Method:      http.MethodGet,
			Path:        "/calendars/{calendarId}/labels",
			Summary:     "List calendar labels (colors)",
			Tags:        []string{"Label"},
		}, calendars.ListLabels(calDeps))

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
			OperationID: "create-event",
			Method:      http.MethodPost,
			Path:        "/calendars/{calendarId}/events",
			Summary:     "Create an event",
			Tags:        []string{"Event"},
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

		// Memos
		huma.Register(api, huma.Operation{
			OperationID: "list-memos",
			Method:      http.MethodGet,
			Path:        "/calendars/{calendarId}/memos",
			Summary:     "List memos in a calendar",
			Tags:        []string{"Memo"},
		}, memos.ListMemos(memoDeps))

		huma.Register(api, huma.Operation{
			OperationID: "create-memo",
			Method:      http.MethodPost,
			Path:        "/calendars/{calendarId}/memos",
			Summary:     "Create a memo",
			Tags:        []string{"Memo"},
		}, memos.CreateMemo(memoDeps))

		huma.Register(api, huma.Operation{
			OperationID: "update-memo",
			Method:      http.MethodPut,
			Path:        "/calendars/{calendarId}/memos/{memoId}",
			Summary:     "Update a memo",
			Tags:        []string{"Memo"},
		}, memos.UpdateMemo(memoDeps))

		huma.Register(api, huma.Operation{
			OperationID: "delete-memo",
			Method:      http.MethodDelete,
			Path:        "/calendars/{calendarId}/memos/{memoId}",
			Summary:     "Delete a memo",
			Tags:        []string{"Memo"},
		}, memos.DeleteMemo(memoDeps))

		// Invites
		huma.Register(api, huma.Operation{
			OperationID: "create-invite",
			Method:      http.MethodPost,
			Path:        "/calendars/{calendarId}/invites",
			Summary:     "Create a calendar invite link",
			Tags:        []string{"Invite"},
		}, invites.CreateInvite(invDeps))

		huma.Register(api, huma.Operation{
			OperationID: "list-invites",
			Method:      http.MethodGet,
			Path:        "/calendars/{calendarId}/invites",
			Summary:     "List invite links for a calendar",
			Tags:        []string{"Invite"},
		}, invites.ListInvites(invDeps))

		huma.Register(api, huma.Operation{
			OperationID: "delete-invite",
			Method:      http.MethodDelete,
			Path:        "/calendars/{calendarId}/invites/{inviteId}",
			Summary:     "Delete/revoke an invite link",
			Tags:        []string{"Invite"},
		}, invites.DeleteInviteHandler(invDeps))

		huma.Register(api, huma.Operation{
			OperationID: "accept-invite",
			Method:      http.MethodPost,
			Path:        "/invites/{token}/accept",
			Summary:     "Accept a calendar invite",
			Tags:        []string{"Invite"},
		}, invites.AcceptInvite(invDeps))
	})

	return r
}
