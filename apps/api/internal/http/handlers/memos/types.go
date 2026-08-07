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

type UpdateMemoInput struct {
	CalendarID string `path:"calendarId"`
	MemoID     string `path:"memoId"`
	Body       struct {
		Title     string `json:"title" minLength:"1" maxLength:"500"`
		Body      string `json:"body" maxLength:"16000" required:"false"`
		Done      bool   `json:"done"`
		SortOrder int32  `json:"sortOrder"`
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
