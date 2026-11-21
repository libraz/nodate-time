package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/memos"
)

func registerMemos(g apiGroups, deps Deps) {
	api := g.protected
	memoDeps := memos.Deps{DB: deps.DB, Queries: deps.Queries, WorkspaceID: deps.WorkspaceID}

	// Memos
	huma.Register(api, huma.Operation{
		OperationID: "list-memos",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/memos",
		Summary:     "List memos in a calendar",
		Tags:        []string{"Memo"},
	}, memos.ListMemos(memoDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "create-memo",
		Method:        http.MethodPost,
		Path:          "/calendars/{calendarId}/memos",
		Summary:       "Create a memo",
		Tags:          []string{"Memo"},
		DefaultStatus: http.StatusCreated,
	}, memos.CreateMemo(memoDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-memo",
		Method:      http.MethodPatch,
		Path:        "/calendars/{calendarId}/memos/{memoId}",
		Summary:     "Update a memo",
		Tags:        []string{"Memo"},
		Errors:      preconditionedErrors(apierrors.MemoStale, apierrors.MemoNotFound, apierrors.Validation),
	}, memos.UpdateMemo(memoDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-memo",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/memos/{memoId}",
		Summary:       "Delete a memo",
		Tags:          []string{"Memo"},
		DefaultStatus: http.StatusNoContent,
		Errors:        preconditionedErrors(apierrors.MemoStale, apierrors.MemoNotFound),
	}, memos.DeleteMemo(memoDeps))
}
