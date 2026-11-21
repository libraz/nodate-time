package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/invites"
)

func registerInvites(g apiGroups, deps Deps) {
	invDeps := invites.Deps{DB: deps.DB, Queries: deps.Queries, WorkspaceID: deps.WorkspaceID}

	// Public share links
	api := g.share
	huma.Register(api, huma.Operation{
		OperationID: "public-calendar",
		Method:      http.MethodGet,
		Path:        "/share/{token}",
		Summary:     "Get calendar info via share token",
		Tags:        []string{"Share"},
	}, invites.PublicCalendar(invDeps))

	huma.Register(api, huma.Operation{
		OperationID: "public-events",
		Method:      http.MethodGet,
		Path:        "/share/{token}/events",
		Summary:     "List events via share token",
		Tags:        []string{"Share"},
	}, invites.PublicEvents(invDeps))

	// Invites
	api = g.protected

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
}
