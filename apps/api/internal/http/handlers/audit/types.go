package audit

import "time"

// HistoryItem is one audit-log entry for a single entity's history.
type HistoryItem struct {
	ID             string    `json:"id"`
	Action         string    `json:"action"`
	Summary        string    `json:"summary"`
	CreatedAt      time.Time `json:"createdAt"`
	ActorID        string    `json:"actorId,omitempty"`
	ActorName      string    `json:"actorName,omitempty"`
	ActorAvatarURL string    `json:"actorAvatarUrl,omitempty"`
	SystemActor    bool      `json:"systemActor,omitempty"`
}

// FeedItem is one audit-log entry in a calendar's activity feed, carrying the
// entity it refers to in addition to the history fields.
type FeedItem struct {
	HistoryItem
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
}

type EventHistoryInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
}
type EventHistoryOutput struct {
	Body []HistoryItem
}

type MemoHistoryInput struct {
	CalendarID string `path:"calendarId"`
	MemoID     string `path:"memoId"`
}
type MemoHistoryOutput struct {
	Body []HistoryItem
}

type ActivityInput struct {
	CalendarID string `path:"calendarId"`
	Cursor     string `query:"cursor" required:"false" doc:"Opaque cursor from a previous response"`
	Limit      int    `query:"limit" required:"false" minimum:"1" maximum:"200" default:"50"`
}
type ActivityPage struct {
	Items      []FeedItem `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
}
type ActivityOutput struct {
	Body ActivityPage
}
