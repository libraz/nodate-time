package router

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/admin"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/albums"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/audit"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/calendars"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/invites"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/memos"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/internal/secrets"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

type Deps struct {
	DB        *sql.DB
	Queries   *generated.Queries
	JWTSecret string
	Storage   *storage.Client
	Mailer    mailer.Mailer
	WebURL    string
	OAuth     users.OAuthConfig
	Cipher    *secrets.Cipher
	// GoogleAllowedDomains restricts Google sign-in to these email domains.
	// Empty means unrestricted. See config.GoogleAllowedDomainList.
	GoogleAllowedDomains []string
	// DevMode enables development-only endpoints (e.g. /auth/dev-login).
	DevMode bool
	// PasswordLoginEnabled gates the email+password auth routes (register,
	// login, password reset). When false, only OAuth/OIDC sign-in is offered.
	PasswordLoginEnabled bool
	// AuthRateLimit sets the per-IP request budget for sensitive unauthenticated
	// endpoints. Zero applies the default (60/min); a negative value disables the
	// limiter entirely (used by parallel integration tests).
	AuthRateLimit int
}

func Build(deps Deps) http.Handler {
	r := chi.NewRouter()

	// Health check
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// --- Public routes (no auth) ---
	// Sensitive unauthenticated endpoints (login, register, password reset,
	// OAuth, public share) are rate-limited per client IP to blunt credential
	// brute-force and password-reset mail-bombing.
	authLimit := deps.AuthRateLimit
	if authLimit == 0 {
		authLimit = 60
	}
	r.Group(func(pub chi.Router) {
		if authLimit > 0 {
			pub.Use(middleware.NewRateLimiter(authLimit, time.Minute).Middleware())
		}
		api := humachi.New(pub, huma.DefaultConfig("Nodate Time", "1.0.0"))

		userDeps := users.Deps{Queries: deps.Queries, JWTSecret: deps.JWTSecret, Storage: deps.Storage, AllowedDomains: deps.GoogleAllowedDomains}

		// Email+password auth (register, login, password reset) is registered
		// only when enabled. Disabling it yields an OAuth/OIDC-only deployment.
		if deps.PasswordLoginEnabled {
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
		}

		// Development-only: password-less login for seeded sample accounts.
		// Gated by DevMode alone, so it survives even when password login is
		// disabled — dev verification is never blocked.
		if deps.DevMode {
			huma.Register(api, huma.Operation{
				OperationID: "dev-login",
				Method:      http.MethodPost,
				Path:        "/auth/dev-login",
				Summary:     "Password-less login for seeded dev accounts (development only)",
				Tags:        []string{"Auth"},
			}, users.DevLogin(userDeps))
		}

		// Password reset only makes sense when password login is enabled.
		if deps.PasswordLoginEnabled {
			resetDeps := users.ResetDeps{DB: deps.DB, Queries: deps.Queries, Mailer: deps.Mailer, WebURL: deps.WebURL}

			huma.Register(api, huma.Operation{
				OperationID: "request-password-reset",
				Method:      http.MethodPost,
				Path:        "/auth/password-reset/request",
				Summary:     "Request a password reset email",
				Tags:        []string{"Auth"},
			}, users.RequestPasswordReset(resetDeps))

			huma.Register(api, huma.Operation{
				OperationID: "confirm-password-reset",
				Method:      http.MethodPost,
				Path:        "/auth/password-reset/confirm",
				Summary:     "Confirm password reset with token",
				Tags:        []string{"Auth"},
			}, users.ConfirmPasswordReset(resetDeps))
		}

		oauthDeps := users.OAuthDeps{
			DB:                   deps.DB,
			Queries:              deps.Queries,
			JWTSecret:            deps.JWTSecret,
			WebURL:               deps.WebURL,
			Config:               deps.OAuth,
			Cipher:               deps.Cipher,
			AllowedDomains:       deps.GoogleAllowedDomains,
			PasswordLoginEnabled: deps.PasswordLoginEnabled,
		}

		huma.Register(api, huma.Operation{
			OperationID: "list-oauth-providers-public",
			Method:      http.MethodGet,
			Path:        "/auth/oauth/providers",
			Summary:     "List enabled OAuth providers for the login screen",
			Tags:        []string{"Auth"},
		}, users.ListEnabledProviders(oauthDeps))

		huma.Register(api, huma.Operation{
			OperationID: "oauth-start",
			Method:      http.MethodGet,
			Path:        "/auth/oauth/{provider}/start",
			Summary:     "Begin OAuth login flow",
			Tags:        []string{"Auth"},
		}, users.OAuthStart(oauthDeps))

		huma.Register(api, huma.Operation{
			OperationID: "oauth-callback",
			Method:      http.MethodGet,
			Path:        "/auth/oauth/{provider}/callback",
			Summary:     "OAuth callback handler",
			Tags:        []string{"Auth"},
		}, users.OAuthCallback(oauthDeps))

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

		userDeps := users.Deps{Queries: deps.Queries, JWTSecret: deps.JWTSecret, Storage: deps.Storage, AllowedDomains: deps.GoogleAllowedDomains}
		calDeps := calendars.Deps{DB: deps.DB, Queries: deps.Queries}
		evtDeps := events.Deps{DB: deps.DB, Queries: deps.Queries, Storage: deps.Storage}
		memoDeps := memos.Deps{Queries: deps.Queries}
		invDeps := invites.Deps{DB: deps.DB, Queries: deps.Queries}
		albumDeps := albums.Deps{DB: deps.DB, Queries: deps.Queries, Storage: deps.Storage}
		auditDeps := audit.Deps{DB: deps.DB, Queries: deps.Queries, Storage: deps.Storage}

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

		huma.Register(api, huma.Operation{
			OperationID: "presign-avatar",
			Method:      http.MethodPost,
			Path:        "/user/avatar/presign",
			Summary:     "Get a presigned URL for uploading a profile avatar",
			Tags:        []string{"User"},
		}, users.PresignAvatar(userDeps))

		huma.Register(api, huma.Operation{
			OperationID: "confirm-avatar",
			Method:      http.MethodPut,
			Path:        "/user/avatar",
			Summary:     "Confirm a previously uploaded avatar",
			Tags:        []string{"User"},
		}, users.ConfirmAvatar(userDeps))

		huma.Register(api, huma.Operation{
			OperationID: "delete-avatar",
			Method:      http.MethodDelete,
			Path:        "/user/avatar",
			Summary:     "Remove the current avatar",
			Tags:        []string{"User"},
		}, users.DeleteAvatar(userDeps))

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

		huma.Register(api, huma.Operation{
			OperationID: "update-member-role",
			Method:      http.MethodPut,
			Path:        "/calendars/{calendarId}/members/{userId}/role",
			Summary:     "Update a member's role",
			Tags:        []string{"Member"},
		}, calendars.UpdateMemberRole(calDeps))

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
		}, calendars.ExportEvents(calDeps))

		huma.Register(api, huma.Operation{
			OperationID: "import-events",
			Method:      http.MethodPost,
			Path:        "/calendars/{calendarId}/import",
			Summary:     "Import events from iCal text",
			Tags:        []string{"Calendar"},
		}, calendars.ImportEvents(calDeps))

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

		// Audit history / activity feed
		huma.Register(api, huma.Operation{
			OperationID: "get-event-history",
			Method:      http.MethodGet,
			Path:        "/calendars/{calendarId}/events/{eventId}/history",
			Summary:     "Get an event's audit history",
			Tags:        []string{"Audit"},
		}, audit.EventHistory(auditDeps))

		huma.Register(api, huma.Operation{
			OperationID: "get-memo-history",
			Method:      http.MethodGet,
			Path:        "/calendars/{calendarId}/memos/{memoId}/history",
			Summary:     "Get a memo's audit history",
			Tags:        []string{"Audit"},
		}, audit.MemoHistory(auditDeps))

		huma.Register(api, huma.Operation{
			OperationID: "get-calendar-activity",
			Method:      http.MethodGet,
			Path:        "/calendars/{calendarId}/activity",
			Summary:     "Get the calendar activity feed",
			Tags:        []string{"Audit"},
		}, audit.Activity(auditDeps))

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

		// Album
		huma.Register(api, huma.Operation{
			OperationID: "list-album-photos",
			Method:      http.MethodGet,
			Path:        "/calendars/{calendarId}/albums",
			Summary:     "List photos in the calendar album",
			Tags:        []string{"Album"},
		}, albums.ListPhotos(albumDeps))

		huma.Register(api, huma.Operation{
			OperationID: "presign-album-photo",
			Method:      http.MethodPost,
			Path:        "/calendars/{calendarId}/albums/presign",
			Summary:     "Get a presigned URL for uploading an album photo",
			Tags:        []string{"Album"},
		}, albums.PresignUpload(albumDeps))

		huma.Register(api, huma.Operation{
			OperationID: "confirm-album-photo",
			Method:      http.MethodPost,
			Path:        "/calendars/{calendarId}/albums/{photoId}/confirm",
			Summary:     "Confirm a previously uploaded album photo",
			Tags:        []string{"Album"},
		}, albums.ConfirmPhoto(albumDeps))

		huma.Register(api, huma.Operation{
			OperationID: "update-album-photo",
			Method:      http.MethodPut,
			Path:        "/calendars/{calendarId}/albums/{photoId}",
			Summary:     "Update an album photo's caption or linked event",
			Tags:        []string{"Album"},
		}, albums.UpdatePhoto(albumDeps))

		huma.Register(api, huma.Operation{
			OperationID:   "delete-album-photo",
			Method:        http.MethodDelete,
			Path:          "/calendars/{calendarId}/albums/{photoId}",
			Summary:       "Delete an album photo",
			Tags:          []string{"Album"},
			DefaultStatus: 204,
		}, albums.DeletePhoto(albumDeps))

		huma.Register(api, huma.Operation{
			OperationID: "get-album-photo-download",
			Method:      http.MethodGet,
			Path:        "/calendars/{calendarId}/albums/{photoId}/download",
			Summary:     "Get a presigned download URL for a single photo",
			Tags:        []string{"Album"},
		}, albums.GetDownload(albumDeps))

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

	// --- Admin routes (require auth + admin allowlist) ---
	r.Group(func(adm chi.Router) {
		adm.Use(middleware.RequireAuth(deps.JWTSecret))
		adm.Use(middleware.RequireAdmin(deps.Queries))
		api := humachi.New(adm, huma.DefaultConfig("Nodate Time", "1.0.0"))

		envHas := func(p string) bool {
			switch p {
			case "google":
				return deps.OAuth.Google.ClientID != ""
			case "line":
				return deps.OAuth.LINE.ClientID != ""
			}
			return false
		}
		adminDeps := admin.Deps{Queries: deps.Queries, Cipher: deps.Cipher, EnvFallback: envHas, AllowedDomains: deps.GoogleAllowedDomains}

		huma.Register(api, huma.Operation{
			OperationID: "list-oauth-providers",
			Method:      http.MethodGet,
			Path:        "/admin/oauth-providers",
			Summary:     "List configured OAuth providers (admin only)",
			Tags:        []string{"Admin"},
		}, admin.ListOAuthProviders(adminDeps))

		huma.Register(api, huma.Operation{
			OperationID: "update-oauth-provider",
			Method:      http.MethodPut,
			Path:        "/admin/oauth-providers/{provider}",
			Summary:     "Update OAuth provider credentials (admin only)",
			Tags:        []string{"Admin"},
		}, admin.UpdateOAuthProvider(adminDeps))

		huma.Register(api, huma.Operation{
			OperationID:   "delete-oauth-provider",
			Method:        http.MethodDelete,
			Path:          "/admin/oauth-providers/{provider}",
			Summary:       "Delete OAuth provider configuration (admin only)",
			Tags:          []string{"Admin"},
			DefaultStatus: 204,
		}, admin.DeleteOAuthProvider(adminDeps))

		huma.Register(api, huma.Operation{
			OperationID: "list-allowed-emails",
			Method:      http.MethodGet,
			Path:        "/admin/allowed-emails",
			Summary:     "List individually allowed OAuth sign-in emails (admin only)",
			Tags:        []string{"Admin"},
		}, admin.ListAllowedEmails(adminDeps))

		huma.Register(api, huma.Operation{
			OperationID: "create-allowed-email",
			Method:      http.MethodPost,
			Path:        "/admin/allowed-emails",
			Summary:     "Allow an individual email to sign in via OAuth (admin only)",
			Tags:        []string{"Admin"},
		}, admin.CreateAllowedEmail(adminDeps))

		huma.Register(api, huma.Operation{
			OperationID:   "delete-allowed-email",
			Method:        http.MethodDelete,
			Path:          "/admin/allowed-emails/{id}",
			Summary:       "Remove an individually allowed email (admin only)",
			Tags:          []string{"Admin"},
			DefaultStatus: 204,
		}, admin.DeleteAllowedEmail(adminDeps))
	})

	return r
}
