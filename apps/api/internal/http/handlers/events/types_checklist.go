package events

import (
	"time"
)

// Checklist

type ChecklistItemResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
}

type ListChecklistInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
}
type ListChecklistOutput struct {
	Body []ChecklistItemResponse
}

type CreateChecklistItemInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Body       struct {
		Title     string `json:"title" minLength:"1" maxLength:"500"`
		SortOrder int    `json:"sortOrder,omitempty" required:"false"`
	}
}
type CreateChecklistItemOutput struct {
	Body ChecklistItemResponse
}

// UpdateChecklistItemInput carries only what the caller wants to change, the
// way UpdateMemoInput does. Ticking an item is a body of {"done": true}: a
// caller resending a title and position it read earlier would revert another
// member's rename or reorder made since.
type UpdateChecklistItemInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	ItemID     string `path:"itemId"`
	Body       struct {
		Title     *string `json:"title,omitempty" minLength:"1" maxLength:"500" required:"false" doc:"omit to keep the current title"`
		Done      *bool   `json:"done,omitempty" required:"false" doc:"omit to keep the current state"`
		SortOrder *int    `json:"sortOrder,omitempty" required:"false" doc:"omit to keep the current position"`
	}
}
type UpdateChecklistItemOutput struct {
	Body ChecklistItemResponse
}

type DeleteChecklistItemInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	ItemID     string `path:"itemId"`
}
type DeleteChecklistItemOutput struct{}
