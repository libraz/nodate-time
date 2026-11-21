package memos

import (
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

type MemoResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Done      bool      `json:"done"`
	SortOrder int32     `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// ETag names this revision of the memo; send it as If-Match to change or delete it.
	ETag string `json:"etag"`
}

type ListMemosInput struct {
	CalendarID string `path:"calendarId"`
	Cursor     string `query:"cursor" required:"false" doc:"Opaque cursor from a previous response"`
	Limit      int    `query:"limit" required:"false" minimum:"1" maximum:"200" default:"100"`
}

type ListMemosPage struct {
	Items      []MemoResponse `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type ListMemosOutput struct {
	Body ListMemosPage
}

type CreateMemoInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Title     string `json:"title" minLength:"1" maxLength:"500"`
		Body      string `json:"body" maxLength:"16000" required:"false"`
		SortOrder int32  `json:"sortOrder,omitempty" required:"false" doc:"Position in the list, ascending; equal values keep creation order. Omitted stores 0, so the memo follows existing memos of weight 0."`
	}
}
type CreateMemoOutput struct {
	ETag string `header:"ETag"`
	Body MemoResponse
}

// UpdateMemoInput is a merge patch: an omitted member keeps its stored value,
// so ticking a memo off is a body of {"done": true}. Only body can be cleared
// with null; the others have no empty state.
type UpdateMemoInput struct {
	CalendarID string               `path:"calendarId"`
	MemoID     string               `path:"memoId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true"`
	Body       struct {
		Title     patch.Field[string] `json:"title,omitzero" required:"false" minLength:"1" maxLength:"500"`
		Body      patch.Field[string] `json:"body,omitzero" required:"false" maxLength:"16000"`
		Done      patch.Field[bool]   `json:"done,omitzero" required:"false"`
		SortOrder patch.Field[int32]  `json:"sortOrder,omitzero" required:"false" doc:"Position in the list, ascending; equal values keep creation order."`
	}
}
type UpdateMemoOutput struct {
	ETag string `header:"ETag"`
	Body MemoResponse
}

type DeleteMemoInput struct {
	CalendarID string               `path:"calendarId"`
	MemoID     string               `path:"memoId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true"`
}
type DeleteMemoOutput struct{}
