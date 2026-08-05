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
	ID         string `json:"id"`
	CalendarID string `json:"calendarId"`
	Title      string `json:"title"`
	AllDay     bool   `json:"allDay"`
	// StartAt and EndAt are always set on events created through this API.
	// The column is nullable because the shared schema also serves
	// planning-stage placeholders, which this product does not create.
	StartAt  time.Time `json:"startAt"`
	EndAt    time.Time `json:"endAt"`
	Timezone string    `json:"timezone"`
	// Color is derived from the owner's colour on the calendar, not stored
	// per event: an event belongs to a person's layer, and the layer is
	// what carries the colour. Writes ignore it.
	Color    string `json:"color" doc:"read-only; the owner's colour on this calendar"`
	Location string `json:"location"`
	Memo     string `json:"memo"`
	URL      string `json:"url"`
	// ShowAs is the iCalendar TRANSP axis: whether the time reads as taken.
	// It is what an external free/busy consumer sees.
	ShowAs string `json:"showAs" enum:"busy,free,tentative,oof"`
	// Flexibility is whether the commitment could move, which is a separate
	// question from whether the time is taken. A meeting its owner would
	// gladly reschedule and one that cannot move are both busy.
	Flexibility string `json:"flexibility" enum:"fixed,negotiable,conditional"`
	// Visibility decides what a reader outside the calendar's membership is
	// told: default follows the calendar, private shows the time with no
	// details, and confidential is not published at all.
	Visibility         string   `json:"visibility" enum:"default,public,private,confidential"`
	NotificationOffset *int     `json:"notificationOffset"`
	Participants       []string `json:"participants"`
	// Attendees carries what Participants cannot: each participant's answer
	// and whether the owner has trusted them to change the event. Participants
	// stays as the id-only list because it is also the write format.
	Attendees        []AttendeeResponse      `json:"attendees"`
	OwnerID          string                  `json:"ownerId" doc:"public user ID of the member whose layer this event sits on"`
	RecurrenceRule   *RecurrenceRuleResponse `json:"recurrenceRule"`
	IsRecurrence     bool                    `json:"isRecurrence"`
	RecurrenceDate   *string                 `json:"recurrenceDate,omitempty"`
	CreatedBy        string                  `json:"createdBy" doc:"public user ID of the creator"`
	CreatorName      string                  `json:"creatorName"`
	CreatorAvatarURL string                  `json:"creatorAvatarUrl,omitempty"`
	CreatedAt        time.Time               `json:"createdAt"`
	UpdatedAt        time.Time               `json:"updatedAt"`
}

// AttendeeResponse is one participant's state on an event: whether they have
// answered, and whether the owner has delegated editing to them.
type AttendeeResponse struct {
	UserID  string `json:"userId" doc:"public user ID"`
	Rsvp    string `json:"rsvp" enum:"pending,accepted,declined,tentative"`
	CanEdit bool   `json:"canEdit" doc:"read-only here; set through the attendee endpoint"`
}

type SetRsvpInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Body       struct {
		Rsvp string `json:"rsvp" enum:"pending,accepted,declined,tentative"`
	}
}

type SetRsvpOutput struct {
	Body AttendeeResponse
}

type SetAttendeeCanEditInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	UserID     string `path:"userId"`
	Body       struct {
		CanEdit bool `json:"canEdit"`
	}
}

type SetAttendeeCanEditOutput struct {
	Body AttendeeResponse
}

type CommentResponse struct {
	ID           string    `json:"id"`
	UserPublicID string    `json:"userPublicId"`
	UserName     string    `json:"userName"`
	UserAvatar   string    `json:"userAvatar,omitempty"`
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
		Location           string           `json:"location,omitempty" maxLength:"500" required:"false"`
		Memo               string           `json:"memo,omitempty" required:"false"`
		URL                string           `json:"url,omitempty" maxLength:"2000" required:"false"`
		ShowAs             string           `json:"showAs,omitempty" enum:"busy,free,tentative,oof" required:"false" doc:"defaults to busy"`
		Flexibility        string           `json:"flexibility,omitempty" enum:"fixed,negotiable,conditional" required:"false" doc:"defaults to fixed"`
		Visibility         string           `json:"visibility,omitempty" enum:"default,public,private,confidential" required:"false" doc:"defaults to the calendar's setting; omitted on update keeps the current value"`
		NotificationOffset *int             `json:"notificationOffset,omitempty" required:"false"`
		Participants       []string         `json:"participants,omitempty" required:"false"`
		OwnerID            *string          `json:"ownerId,omitempty" required:"false" doc:"public user ID of the owning member; defaults to the caller"`
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
		Location           string           `json:"location" maxLength:"500"`
		Memo               string           `json:"memo"`
		URL                string           `json:"url" maxLength:"2000"`
		ShowAs             string           `json:"showAs,omitempty" enum:"busy,free,tentative,oof" required:"false" doc:"defaults to busy"`
		Flexibility        string           `json:"flexibility,omitempty" enum:"fixed,negotiable,conditional" required:"false" doc:"defaults to fixed"`
		Visibility         string           `json:"visibility,omitempty" enum:"default,public,private,confidential" required:"false" doc:"defaults to the calendar's setting; omitted on update keeps the current value"`
		NotificationOffset *int             `json:"notificationOffset" doc:"null clears the notification"`
		Participants       []string         `json:"participants" doc:"full participant list; an empty array removes all participants"`
		OwnerID            *string          `json:"ownerId" doc:"public user ID of the owning member; null leaves the owner unchanged"`
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
		// SHA256 is the hex digest of the bytes about to be uploaded. The
		// blob is content-addressed, so the same file attached twice is
		// stored once; the digest is also what the storage key is built
		// from, which keeps the client's filename out of the key entirely.
		SHA256 string `json:"sha256" minLength:"64" maxLength:"64" pattern:"^[0-9a-fA-F]{64}$" doc:"lowercase hex SHA-256 of the file contents"`
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
