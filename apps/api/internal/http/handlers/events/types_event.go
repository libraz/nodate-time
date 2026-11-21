package events

import (
	"encoding/json"
	"time"
)

type RecurrenceRuleResponse struct {
	Freq       string   `json:"freq"`
	Interval   int      `json:"interval"`
	ByDay      []string `json:"byDay,omitempty"`
	ByMonthDay []int    `json:"byMonthDay,omitempty"`
	BySetPos   []int    `json:"bySetPos,omitempty"`
	Wkst       string   `json:"wkst,omitempty"`
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
	ParticipantIDs     []string `json:"participantIds"`
	// Attendees carries what ParticipantIDs cannot: each participant's answer
	// and whether the owner has trusted them to change the event. ParticipantIDs
	// stays as the id-only list because it is also the write format.
	Attendees        []AttendeeResponse      `json:"attendees"`
	OwnerID          string                  `json:"ownerId" doc:"public user ID of the member whose layer this event sits on"`
	OwnerName        string                  `json:"ownerName" doc:"display name of the owning member; empty once they have left"`
	OwnerAvatarURL   string                  `json:"ownerAvatarUrl,omitempty"`
	RecurrenceRule   *RecurrenceRuleResponse `json:"recurrenceRule"`
	TaskOwned        bool                    `json:"taskOwned" doc:"true when a task projects this event; its title, dates and recurrence then change on the task"`
	IsRecurrence     bool                    `json:"isRecurrence"`
	RecurrenceDate   *string                 `json:"recurrenceDate,omitempty"`
	CreatorID        string                  `json:"creatorId" doc:"public user ID of the creator"`
	CreatorName      string                  `json:"creatorName"`
	CreatorAvatarURL string                  `json:"creatorAvatarUrl,omitempty"`
	CreatedAt        time.Time               `json:"createdAt"`
	UpdatedAt        time.Time               `json:"updatedAt"`
}

// --- Inputs/Outputs ---

type ListEventsInput struct {
	CalendarID string `path:"calendarId"`
	StartDate  string `query:"start" doc:"ISO date YYYY-MM-DD; requires end"`
	EndDate    string `query:"end" doc:"ISO date YYYY-MM-DD, inclusive; requires start"`
	Days       int    `query:"days" default:"30" minimum:"1" maximum:"366" doc:"Number of days ahead (used if neither start nor end is set)"`
	// TZ names the zone the requested dates are days in. A calendar day is a
	// different span of instants in each zone, so without it a listing has to
	// guess which day the caller meant. Defaults to the caller's own setting.
	TZ string `query:"tz" required:"false" doc:"IANA timezone the dates are read in"`
}
type ListEventsOutput struct {
	// Truncated says the listing stopped at the per-request instance cap. A
	// caller that cannot tell a capped listing from a complete one would
	// render a calendar with events missing and no sign of it.
	Truncated string `header:"X-Result-Truncated" required:"false"`
	Body      []EventResponse
}

type GetEventInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
}
type GetEventOutput struct {
	// ETag names this exact revision of the event, so a later full-replace
	// update can say which copy it is replacing.
	ETag string `header:"ETag" required:"false"`
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
		Memo               string           `json:"memo,omitempty" maxLength:"16000" required:"false"`
		URL                string           `json:"url,omitempty" maxLength:"2000" required:"false"`
		ShowAs             string           `json:"showAs,omitempty" enum:"busy,free,tentative,oof" required:"false" doc:"defaults to busy"`
		Flexibility        string           `json:"flexibility,omitempty" enum:"fixed,negotiable,conditional" required:"false" doc:"defaults to fixed"`
		Visibility         string           `json:"visibility,omitempty" enum:"default,public,private,confidential" required:"false" doc:"defaults to the calendar's setting; omitted on update keeps the current value"`
		NotificationOffset *int             `json:"notificationOffset,omitempty" required:"false"`
		ParticipantIDs     []string         `json:"participantIds,omitempty" required:"false"`
		OwnerID            *string          `json:"ownerId,omitempty" required:"false" doc:"public user ID of the owning member; defaults to the caller"`
		RecurrenceRule     *json.RawMessage `json:"recurrenceRule,omitempty" required:"false" doc:"Weekly rules with interval > 1 use a fixed Sunday week start (WKST=SU)."`
	}
}
type CreateEventOutput struct {
	ETag string `header:"ETag" required:"false"`
	Body EventResponse
}

// UpdateEventInput is a full-replace update: every required content field is
// authoritative and must carry the complete desired state -- clearing is done
// with an explicit empty string, empty array, or null. The optional fields
// (timezone, showAs, flexibility, visibility) keep their current value when
// omitted, so a client that does not know one cannot reset it.
type UpdateEventInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Scope      string `query:"scope" enum:"this,all" default:"all" required:"false" doc:"'this' edits only the occurrence a composite id names and is refused on any other id; 'all' edits the whole series, measuring a move from the dates GET returned for that occurrence"`
	// IfMatch carries the entity tag of the copy being replaced. Because the
	// update is a full replace, an unconditional save from a stale copy undoes
	// whatever the other writer stored in between without either of them
	// seeing it happen. Omitting the header keeps the old last-write-wins
	// behaviour, so an existing integration is not broken by the addition.
	IfMatch string `header:"If-Match" required:"false" doc:"entity tag from the copy being replaced; a mismatch answers 409"`
	Body    struct {
		Title              string           `json:"title" minLength:"1" maxLength:"500"`
		AllDay             bool             `json:"allDay"`
		StartAt            string           `json:"startAt"`
		EndAt              string           `json:"endAt"`
		Timezone           string           `json:"timezone,omitempty" maxLength:"64" required:"false" doc:"IANA timezone (defaults to the event's current zone)"`
		Location           string           `json:"location" maxLength:"500"`
		Memo               string           `json:"memo" maxLength:"16000"`
		URL                string           `json:"url" maxLength:"2000"`
		ShowAs             string           `json:"showAs,omitempty" enum:"busy,free,tentative,oof" required:"false" doc:"omitted keeps the current value"`
		Flexibility        string           `json:"flexibility,omitempty" enum:"fixed,negotiable,conditional" required:"false" doc:"omitted keeps the current value"`
		Visibility         string           `json:"visibility,omitempty" enum:"default,public,private,confidential" required:"false" doc:"omitted keeps the current value"`
		NotificationOffset *int             `json:"notificationOffset" doc:"null clears the notification"`
		ParticipantIDs     []string         `json:"participantIds" doc:"full participant list; an empty array removes all participants"`
		OwnerID            *string          `json:"ownerId" doc:"public user ID of the owning member; null leaves the owner unchanged"`
		RecurrenceRule     *json.RawMessage `json:"recurrenceRule" doc:"full recurrence rule; null makes the event non-recurring. Weekly rules with interval > 1 use a fixed Sunday week start (WKST=SU)."`
	}
}
type UpdateEventOutput struct {
	ETag string `header:"ETag" required:"false"`
	Body EventResponse
}

type DeleteEventInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Scope      string `query:"scope" enum:"this,all" default:"all" required:"false" doc:"'this' deletes only the occurrence a composite id names and is refused on any other id; 'all' deletes the whole series"`
}
type DeleteEventOutput struct{}
