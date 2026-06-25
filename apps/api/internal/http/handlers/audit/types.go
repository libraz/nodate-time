package audit

import "time"

// ActorBrief identifies the user who performed an audited action.
type ActorBrief struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// HistoryItem is one audit-log entry for a single entity's history.
type HistoryItem struct {
	ID        uint64      `json:"id"`
	Action    string      `json:"action"`
	Summary   string      `json:"summary"`
	CreatedAt time.Time   `json:"createdAt"`
	Actor     *ActorBrief `json:"actor"`
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
	Limit      int    `query:"limit" required:"false" minimum:"1" maximum:"200"`
}
type ActivityOutput struct {
	Body []FeedItem
}
