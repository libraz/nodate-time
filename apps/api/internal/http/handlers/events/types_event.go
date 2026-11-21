package events

import (
	"encoding/json"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
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
	// Kind says how to draw the event: a block holds time under a label, free
	// marks time as open, a milestone is a point.
	Kind       string  `json:"kind" enum:"event,block,free,milestone"`
	BlockLabel *string `json:"blockLabel,omitempty" doc:"free-text label of a block; absent on any other kind"`
	AllDay     bool    `json:"allDay"`
	// StartAt and EndAt are the instants the event occupies, for ordering. An
	// all-day event sits on UTC midnights; its StartDate and EndDate are what
	// to display and edit.
	StartAt time.Time `json:"startAt"`
	EndAt   time.Time `json:"endAt"`
	// StartDate and EndDate (exclusive; the same day for a milestone) are set
	// on all-day events only.
	StartDate *string `json:"startDate,omitempty"`
	EndDate   *string `json:"endDate,omitempty"`
	Timezone  string  `json:"timezone"`
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
	// DetailsHidden says the reader sees this event with fields withheld; a
	// write naming a withheld field is refused.
	DetailsHidden bool `json:"detailsHidden"`
	// Etag names the row Id points at: an edited occurrence's own row, else the
	// series. Send it as If-Match to change the event.
	Etag string `json:"etag"`
	// SeriesEtag names the series row of a recurring event's occurrence.
	SeriesEtag string `json:"seriesEtag,omitempty"`
}

// --- Inputs/Outputs ---

type ListEventsInput struct {
	CalendarID string `path:"calendarId"`
	StartDate  string `query:"start" doc:"ISO date YYYY-MM-DD; requires end"`
	EndDate    string `query:"end" doc:"ISO date YYYY-MM-DD, inclusive; requires start"`
	Days       int    `query:"days" default:"30" minimum:"1" maximum:"366" doc:"Number of days ahead (used if neither start nor end is set)"`
	// Limit is capped at daterange.MaxInstances.
	Limit int `query:"limit" default:"5000" minimum:"1" maximum:"5000" doc:"Most events to return; the earliest are kept"`
	// TZ names the zone the requested dates are days in. A calendar day is a
	// different span of instants in each zone, so without it a listing has to
	// guess which day the caller meant. Defaults to the caller's own setting.
	TZ string `query:"tz" required:"false" doc:"IANA timezone the dates are read in"`
}
type ListEventsBody struct {
	Items []EventResponse `json:"items"`
	// HasMore says events the reader can see were left out at Limit. Events the
	// reader cannot see never count.
	HasMore bool `json:"hasMore"`
}
type ListEventsOutput struct {
	Body ListEventsBody
}

type GetEventInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
}
type GetEventOutput struct {
	ETag string `header:"ETag"`
	Body EventResponse
}

type CreateEventInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Title              string           `json:"title" minLength:"1" maxLength:"500"`
		AllDay             bool             `json:"allDay"`
		StartAt            string           `json:"startAt,omitempty" required:"false" doc:"ISO 8601 datetime; timed events only"`
		EndAt              string           `json:"endAt,omitempty" required:"false" doc:"ISO 8601 datetime; timed events only"`
		StartDate          string           `json:"startDate,omitempty" required:"false" pattern:"^\\d{4}-\\d{2}-\\d{2}$" doc:"first day, YYYY-MM-DD; all-day events only"`
		EndDate            string           `json:"endDate,omitempty" required:"false" pattern:"^\\d{4}-\\d{2}-\\d{2}$" doc:"day after the last day (exclusive), YYYY-MM-DD; all-day events only"`
		Timezone           string           `json:"timezone,omitempty" maxLength:"64" required:"false" doc:"IANA timezone (defaults to UTC); a record of the author's zone for an all-day event"`
		Location           string           `json:"location,omitempty" maxLength:"500" required:"false"`
		Memo               string           `json:"memo,omitempty" maxLength:"16000" required:"false"`
		URL                string           `json:"url,omitempty" maxLength:"2000" required:"false"`
		ShowAs             string           `json:"showAs,omitempty" enum:"busy,free,tentative,oof" required:"false" doc:"defaults to busy"`
		Flexibility        string           `json:"flexibility,omitempty" enum:"fixed,negotiable,conditional" required:"false" doc:"defaults to fixed"`
		Visibility         string           `json:"visibility,omitempty" enum:"default,public,private,confidential" required:"false" doc:"defaults to the calendar's setting"`
		NotificationOffset *int             `json:"notificationOffset,omitempty" minimum:"0" maximum:"10080" required:"false" doc:"minutes before the start"`
		ParticipantIDs     []string         `json:"participantIds,omitempty" required:"false"`
		OwnerID            *string          `json:"ownerId,omitempty" required:"false" doc:"public user ID of the owning member; defaults to the caller"`
		RecurrenceRule     *json.RawMessage `json:"recurrenceRule,omitempty" required:"false" doc:"Weekly rules with interval > 1 use a fixed Sunday week start (WKST=SU). until is YYYY-MM-DD for an all-day series and an RFC 3339 instant otherwise."`
	}
}
type CreateEventOutput struct {
	ETag string `header:"ETag"`
	Body EventResponse
}

// PatchEventInput is a merge-patch: a member left out keeps its stored value, a
// null clears a member that can be cleared, an array replaces the stored one.
type PatchEventInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Scope      string `query:"scope" enum:"this,all" default:"all" required:"false" doc:"'this' edits only the occurrence a composite id names and is refused on any other id; 'all' edits the whole series, measuring a move from the dates GET returned for that occurrence"`
	// IfMatch is compared with the etag of the copy being changed: the series'
	// for scope 'all', else the occurrence's own row or, before it has one, the
	// series'.
	IfMatch precondition.IfMatch `header:"If-Match" required:"true"`
	Body    struct {
		Title              string                       `json:"title,omitempty" required:"false" minLength:"1" maxLength:"500"`
		AllDay             *bool                        `json:"allDay,omitempty" required:"false" doc:"switching needs both ends of the new form"`
		StartAt            string                       `json:"startAt,omitempty" required:"false" doc:"ISO 8601 datetime; timed events only"`
		EndAt              string                       `json:"endAt,omitempty" required:"false" doc:"ISO 8601 datetime; timed events only"`
		StartDate          string                       `json:"startDate,omitempty" required:"false" pattern:"^\\d{4}-\\d{2}-\\d{2}$" doc:"first day, YYYY-MM-DD; all-day events only"`
		EndDate            string                       `json:"endDate,omitempty" required:"false" pattern:"^\\d{4}-\\d{2}-\\d{2}$" doc:"day after the last day (exclusive), YYYY-MM-DD; all-day events only"`
		Timezone           string                       `json:"timezone,omitempty" maxLength:"64" required:"false" doc:"IANA timezone"`
		Location           patch.Field[string]          `json:"location,omitzero" required:"false" maxLength:"500"`
		Memo               patch.Field[string]          `json:"memo,omitzero" required:"false" maxLength:"16000"`
		URL                patch.Field[string]          `json:"url,omitzero" required:"false" maxLength:"2000"`
		BlockLabel         patch.Field[string]          `json:"blockLabel,omitzero" required:"false" maxLength:"200" doc:"label of a block; refused on any other kind"`
		ShowAs             string                       `json:"showAs,omitempty" enum:"busy,free,tentative,oof" required:"false"`
		Flexibility        string                       `json:"flexibility,omitempty" enum:"fixed,negotiable,conditional" required:"false"`
		Visibility         string                       `json:"visibility,omitempty" enum:"default,public,private,confidential" required:"false"`
		NotificationOffset patch.Field[int]             `json:"notificationOffset,omitzero" required:"false" minimum:"0" maximum:"10080" doc:"minutes before the start; null clears it"`
		ParticipantIDs     []string                     `json:"participantIds,omitempty" required:"false" doc:"full participant list; an empty array removes all participants"`
		OwnerID            *string                      `json:"ownerId,omitempty" required:"false" doc:"public user ID of the owning member"`
		RecurrenceRule     patch.Field[json.RawMessage] `json:"recurrenceRule,omitzero" required:"false" doc:"full recurrence rule; null makes the event non-recurring. until is YYYY-MM-DD for an all-day series and an RFC 3339 instant otherwise."`
	}
}
type PatchEventOutput struct {
	ETag string `header:"ETag"`
	Body EventResponse
}

// UpdateEventInput is a patch resolved against the stored row: every member
// holds the value the write will store, whether the request sent it or not.
// The edit paths read their fields from here.
type UpdateEventInput struct {
	Scope string
	Body  struct {
		Title              string
		AllDay             bool
		Timezone           string
		Location           string
		Memo               string
		URL                string
		ShowAs             string
		Flexibility        string
		Visibility         string
		NotificationOffset *int
		ParticipantIDs     []string
		OwnerID            *string
		RecurrenceRule     *json.RawMessage
	}
}

type DeleteEventInput struct {
	CalendarID string               `path:"calendarId"`
	EventID    string               `path:"eventId"`
	Scope      string               `query:"scope" enum:"this,all" default:"all" required:"false" doc:"'this' deletes only the occurrence a composite id names and is refused on any other id; 'all' deletes the whole series"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true"`
}
type DeleteEventOutput struct{}
