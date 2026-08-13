package memos

import "time"

type MemoResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Done      bool      `json:"done"`
	SortOrder int32     `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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
		SortOrder int32  `json:"sortOrder,omitempty" required:"false" doc:"omitted means first"`
	}
}
type CreateMemoOutput struct {
	Body MemoResponse
}

// UpdateMemoInput carries only what the caller wants to change. Every field
// is optional and an omitted one keeps its stored value, so ticking a memo off
// is a body of {"done": true} rather than the whole memo read back and sent
// again. Position in particular belongs to the list, not to whoever is editing
// the text, and requiring it made a caller resend a number it had no reason to
// hold. The pointers are what carry the distinction: without them an omitted
// done would arrive as false and quietly reopen a finished memo.
type UpdateMemoInput struct {
	CalendarID string `path:"calendarId"`
	MemoID     string `path:"memoId"`
	Body       struct {
		Title     *string `json:"title,omitempty" minLength:"1" maxLength:"500" required:"false"`
		Body      *string `json:"body,omitempty" maxLength:"16000" required:"false"`
		Done      *bool   `json:"done,omitempty" required:"false"`
		SortOrder *int32  `json:"sortOrder,omitempty" required:"false"`
	}
}
type UpdateMemoOutput struct {
	Body MemoResponse
}

type DeleteMemoInput struct {
	CalendarID string `path:"calendarId"`
	MemoID     string `path:"memoId"`
}
type DeleteMemoOutput struct{}
