package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/audit"
)

func registerAudit(g apiGroups, deps Deps) {
	api := g.protected
	auditDeps := audit.Deps{DB: deps.DB, Queries: deps.Queries, Storage: deps.Storage, WorkspaceID: deps.WorkspaceID}

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
}
