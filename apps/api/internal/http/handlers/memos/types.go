package memos

import "time"

type MemoResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	SortOrder int32     `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ListMemosInput struct {
	CalendarID string `path:"calendarId"`
}
type ListMemosOutput struct {
	Body []MemoResponse
}

type CreateMemoInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Title     string `json:"title" minLength:"1" maxLength:"500"`
		SortOrder int32  `json:"sortOrder"`
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
