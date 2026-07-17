package events

import (
	"encoding/json"
	"time"
)

type RecurrenceRuleResponse struct {
	Freq       string   `json:"freq"`
	Interval   int      `json:"interval"`
	ByDay      []string `json:"byDay,omitempty"`
	ByMonthDay int      `json:"byMonthDay,omitempty"`
	BySetPos   int      `json:"bySetPos,omitempty"`
	Until      *string  `json:"until,omitempty"`
	Count      int      `json:"count,omitempty"`
}

type EventResponse struct {
	ID                 string                  `json:"id"`
	CalendarID         string                  `json:"calendarId"`
	Title              string                  `json:"title"`
	AllDay             bool                    `json:"allDay"`
	StartAt            time.Time               `json:"startAt"`
	EndAt              time.Time               `json:"endAt"`
	Timezone           string                  `json:"timezone"`
	Color              string                  `json:"color"`
	Location           string                  `json:"location"`
	Memo               string                  `json:"memo"`
	URL                string                  `json:"url"`
	NotificationOffset *int                    `json:"notificationOffset"`
	Participants       []string                `json:"participants"`
	AssignedTo         *string                 `json:"assignedTo"`
	RecurrenceRule     *RecurrenceRuleResponse `json:"recurrenceRule"`
	IsRecurrence       bool                    `json:"isRecurrence"`
	RecurrenceDate     *string                 `json:"recurrenceDate,omitempty"`
	CreatedBy          string                  `json:"createdBy" doc:"public user ID of the creator"`
	CreatorName        string                  `json:"creatorName"`
	CreatorIcon        string                  `json:"creatorIcon"`
	CreatorAvatarURL   string                  `json:"creatorAvatarUrl,omitempty"`
	CreatedAt          time.Time               `json:"createdAt"`
	UpdatedAt          time.Time               `json:"updatedAt"`
}

type CommentResponse struct {
	ID           string    `json:"id"`
	UserPublicID string    `json:"userPublicId"`
	UserName     string    `json:"userName"`
	UserIcon     string    `json:"userIcon"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"createdAt"`
}

// --- Inputs/Outputs ---

type ListEventsInput struct {
	CalendarID string `path:"calendarId"`
	StartDate  string `query:"start" doc:"ISO date YYYY-MM-DD"`
	EndDate    string `query:"end" doc:"ISO date YYYY-MM-DD"`
	Days       int    `query:"days" default:"30" minimum:"1" maximum:"366" doc:"Number of days ahead (used if start/end not set)"`
}
type ListEventsOutput struct {
	Body []EventResponse
}

type GetEventInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
}
type GetEventOutput struct {
	Body EventResponse
}

type CreateEventInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Title              string           `json:"title" minLength:"1" maxLength:"500"`
		AllDay             bool             `json:"allDay"`
		StartAt            string           `json:"startAt" doc:"ISO 8601 datetime"`
		EndAt              string           `json:"endAt" doc:"ISO 8601 datetime"`
		Timezone           string           `json:"timezone,omitempty" maxLength:"64" required:"false" doc:"IANA timezone (defaults to UTC)"`
		Color              string           `json:"color,omitempty" maxLength:"7" required:"false"`
		Location           string           `json:"location,omitempty" maxLength:"500" required:"false"`
		Memo               string           `json:"memo,omitempty" required:"false"`
		URL                string           `json:"url,omitempty" maxLength:"2000" required:"false"`
		NotificationOffset *int             `json:"notificationOffset,omitempty" required:"false"`
		Participants       []string         `json:"participants,omitempty" required:"false"`
		AssignedTo         *string          `json:"assignedTo,omitempty" required:"false" doc:"public user ID of the assigned member"`
		RecurrenceRule     *json.RawMessage `json:"recurrenceRule,omitempty" required:"false" doc:"Weekly rules with interval > 1 use a fixed Sunday week start (WKST=SU)."`
	}
}
type CreateEventOutput struct {
	Body EventResponse
}

// UpdateEventInput is a full-replace update: every content field is
// authoritative and must carry the complete desired state. Omitting a field is
// a contract violation, not a "keep current value" request — clearing is done
// with an explicit empty string, empty array, or null.
type UpdateEventInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Scope      string `query:"scope" enum:"this,all" default:"all" required:"false" doc:"For recurring events: 'this' edits only this occurrence, 'all' edits the whole series"`
	Body       struct {
		Title              string           `json:"title" minLength:"1" maxLength:"500"`
		AllDay             bool             `json:"allDay"`
		StartAt            string           `json:"startAt"`
		EndAt              string           `json:"endAt"`
		Timezone           string           `json:"timezone,omitempty" maxLength:"64" required:"false" doc:"IANA timezone (defaults to the event's current zone)"`
		Color              string           `json:"color" maxLength:"7" doc:"empty applies the default color"`
		Location           string           `json:"location" maxLength:"500"`
		Memo               string           `json:"memo"`
		URL                string           `json:"url" maxLength:"2000"`
		NotificationOffset *int             `json:"notificationOffset" doc:"null clears the notification"`
		Participants       []string         `json:"participants" doc:"full participant list; an empty array removes all participants"`
		AssignedTo         *string          `json:"assignedTo" doc:"public user ID of the assigned member; null clears the assignment"`
		RecurrenceRule     *json.RawMessage `json:"recurrenceRule" doc:"full recurrence rule; null makes the event non-recurring. Weekly rules with interval > 1 use a fixed Sunday week start (WKST=SU)."`
	}
}
type UpdateEventOutput struct {
	Body EventResponse
}

type DeleteEventInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Scope      string `query:"scope" enum:"this,all" default:"all" required:"false" doc:"For recurring events: 'this' deletes only this occurrence, 'all' deletes the whole series"`
}
type DeleteEventOutput struct{}

// Comments

type ListCommentsInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
}
type ListCommentsOutput struct {
	Body []CommentResponse
}

type CreateCommentInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Body       struct {
		Content string `json:"content" minLength:"1"`
	}
}
type CreateCommentOutput struct {
	Body CommentResponse
}

type UpdateCommentInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	CommentID  string `path:"commentId"`
	Body       struct {
		Content string `json:"content" minLength:"1"`
	}
}
type UpdateCommentOutput struct {
	Body CommentResponse
}

type DeleteCommentInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	CommentID  string `path:"commentId"`
}
type DeleteCommentOutput struct{}

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

type UpdateChecklistItemInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	ItemID     string `path:"itemId"`
	Body       struct {
		Title     string `json:"title" minLength:"1" maxLength:"500"`
		Done      bool   `json:"done"`
		SortOrder *int   `json:"sortOrder,omitempty" required:"false" doc:"omit to keep the current position"`
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

// Attachments

type AttachmentResponse struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	ByteSize    int64     `json:"byteSize"`
	CreatedAt   time.Time `json:"createdAt"`
}

type PresignUploadInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Body       struct {
		Filename    string `json:"filename" minLength:"1" maxLength:"500"`
		ContentType string `json:"contentType,omitempty" maxLength:"255" required:"false"`
		ByteSize    int64  `json:"byteSize"`
	}
}
type PresignUploadOutput struct {
	Body struct {
		AttachmentID string `json:"attachmentId"`
		UploadURL    string `json:"uploadUrl"`
	}
}

type ConfirmAttachmentInput struct {
	CalendarID   string `path:"calendarId"`
	EventID      string `path:"eventId"`
	AttachmentID string `path:"attachmentId"`
}
type ConfirmAttachmentOutput struct {
	Body AttachmentResponse
}

type ListAttachmentsInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
}
type ListAttachmentsOutput struct {
	Body []AttachmentResponse
}

type GetAttachmentDownloadInput struct {
	CalendarID   string `path:"calendarId"`
	EventID      string `path:"eventId"`
	AttachmentID string `path:"attachmentId"`
}
type GetAttachmentDownloadOutput struct {
	Body struct {
		DownloadURL string `json:"downloadUrl"`
	}
}

type DeleteAttachmentInput struct {
	CalendarID   string `path:"calendarId"`
	EventID      string `path:"eventId"`
	AttachmentID string `path:"attachmentId"`
}
type DeleteAttachmentOutput struct{}
