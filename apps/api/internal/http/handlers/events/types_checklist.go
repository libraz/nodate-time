package events

import (
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

// Checklist

type ChecklistItemResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	SortOrder int       `json:"sortOrder"`
	ETag      string    `json:"etag" doc:"Version of this item; send it as If-Match on a change"`
	CreatedAt time.Time `json:"createdAt"`
}

type ListChecklistInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
}

// ListChecklistOutput is the whole checklist, a bare array: its size is
// bounded when an item is added.
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
	ETag string `header:"ETag" required:"false"`
	Body ChecklistItemResponse
}

// UpdateChecklistItemInput is a merge patch: a member that is absent keeps its
// stored value. Ticking an item is a body of {"done": true}; a caller resending
// a title and position it read earlier would revert another member's rename or
// reorder made since. No member accepts null.
type UpdateChecklistItemInput struct {
	CalendarID string               `path:"calendarId"`
	EventID    string               `path:"eventId"`
	ItemID     string               `path:"itemId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true" doc:"etag of the item being changed"`
	Body       struct {
		Title     patch.Field[string] `json:"title,omitzero" required:"false" minLength:"1" maxLength:"500" doc:"omit to keep the current title"`
		Done      patch.Field[bool]   `json:"done,omitzero" required:"false" doc:"omit to keep the current state"`
		SortOrder patch.Field[int]    `json:"sortOrder,omitzero" required:"false" doc:"omit to keep the current position"`
	}
}
type UpdateChecklistItemOutput struct {
	ETag string `header:"ETag" required:"false"`
	Body ChecklistItemResponse
}

type DeleteChecklistItemInput struct {
	CalendarID string               `path:"calendarId"`
	EventID    string               `path:"eventId"`
	ItemID     string               `path:"itemId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true" doc:"etag of the item being deleted"`
}
type DeleteChecklistItemOutput struct{}
