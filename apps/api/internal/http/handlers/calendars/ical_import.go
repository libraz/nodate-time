package calendars

import (
	"context"
	"database/sql"

	"github.com/libraz/nodate-time/apps/api/internal/calimport"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// --- Import (iCal) ---

const importMaxEvents = 5000

type ImportInputAlt struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		ICS string `json:"ics" minLength:"1" maxLength:"5242880" doc:"raw .ics content (max 5 MiB)"`
	}
}

type ImportOutput struct {
	Body struct {
		Imported int `json:"imported"`
		Skipped  int `json:"skipped"`
		Failed   int `json:"failed"`
		// Rejected is the part of Failed the file itself caused: a value the
		// calendar cannot hold, however many times it is uploaded. It is
		// counted inside Failed rather than beside it so the four outcomes
		// still account for every event exactly once, and it exists because
		// "failed" on its own invites a retry that can only fail again.
		Rejected int `json:"rejected"`
		// Duplicates is how many events the calendar already held, recognised
		// by what the file called them. They are counted apart from Skipped,
		// which means the parser could not use the event: a clean re-upload
		// reporting "skipped: 40" reads as a failure to whoever just uploaded
		// it, and "already here: 40" is the same fact the other way up.
		Duplicates int `json:"duplicates"`
		// Truncated is how many events past the per-file limit were never
		// looked at. It is reported separately from Skipped because it is the
		// one outcome the file itself cannot explain.
		Truncated int `json:"truncated"`
		// UnknownTimezones is how many imported events named a zone nothing
		// here could resolve. Their wall clocks were read as UTC, which is the
		// wrong instant for every zone that is not UTC. The events are on the
		// calendar and only their times are wrong, which nobody checks -- so a
		// fallback that says nothing is the defect, and one that reports
		// itself is something its reader can go and look at.
		UnknownTimezones int `json:"unknownTimezones"`
		// DroppedReminders is how many events carried a reminder longer than
		// the calendar stores; they were imported without it.
		DroppedReminders int `json:"droppedReminders"`
		// Unreadable says the body held nothing this parser recognised as
		// iCalendar. Without it, a file whose shape cannot be read answers
		// exactly like a calendar that genuinely had no events in it: every
		// counter zero, and no way to tell which happened.
		Unreadable bool `json:"unreadable"`
	}
}

// maxNotificationOffset is the longest reminder the calendar stores, in minutes.
const maxNotificationOffset = 10080

// importAlarm maps a parsed alarm onto the stored offset. An alarm outside the
// range the calendar holds is left off rather than snapped to a neighbouring
// value; the import result counts those it dropped.
func importAlarm(minutes *int32) sql.NullInt32 {
	if minutes == nil || *minutes < 0 || *minutes > maxNotificationOffset {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *minutes, Valid: true}
}

// importTransp reads TRANSP back. Only the free end of the axis is carried,
// because that is all the property can say; the finer values survive only in
// events this product wrote.
func importTransp(transp string) generated.CalendarEventsShowAs {
	if transp == "TRANSPARENT" {
		return generated.CalendarEventsShowAsFree
	}
	return generated.CalendarEventsShowAsBusy
}

// importVisibility reads a CLASS property back onto the visibility axis. An
// absent or unrecognised value means the calendar default, which is what a
// file without the property is saying.
func importVisibility(class string) generated.CalendarEventsVisibility {
	switch class {
	case "PUBLIC":
		return generated.CalendarEventsVisibilityPublic
	case "PRIVATE":
		return generated.CalendarEventsVisibilityPrivate
	case "CONFIDENTIAL":
		return generated.CalendarEventsVisibilityConfidential
	default:
		return generated.CalendarEventsVisibilityDefault
	}
}

func ImportEvents(deps Deps) func(context.Context, *ImportInputAlt) (*ImportOutput, error) {
	return func(ctx context.Context, in *ImportInputAlt) (*ImportOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		events := parseICS(in.Body.ICS)
		truncated := 0
		if len(events) > importMaxEvents {
			// Counted, not swallowed. A file that lost a thousand events must
			// not read as a clean import.
			truncated = len(events) - importMaxEvents
			events = events[:importMaxEvents]
		}

		converted := make([]calimport.Event, len(events))
		dropped := 0
		for i, e := range events {
			converted[i] = importEvent(e)
			if e.alarmMinutes != nil && !converted[i].NotificationOffset.Valid {
				dropped++
			}
		}
		res := calimport.Run(ctx, deps.DB, deps.WorkspaceID, cal, userID, converted, "ics-import")

		out := &ImportOutput{}
		out.Body.Imported = res.Imported
		out.Body.Skipped = res.Skipped
		out.Body.Failed = res.Failed
		out.Body.Rejected = res.Rejected
		out.Body.Duplicates = res.Duplicates
		out.Body.Truncated = truncated
		out.Body.UnknownTimezones = res.UnknownTimezones
		out.Body.DroppedReminders = dropped
		out.Body.Unreadable = res.Imported+res.Skipped+res.Failed+res.Duplicates+truncated == 0 &&
			!containsICSComponent(in.Body.ICS)
		return out, nil
	}
}

// importEvent maps a parsed VEVENT onto the shape the store side takes. The
// zone is left empty when the file named none, so a changed occurrence then
// inherits its series' zone.
func importEvent(e rawEvent) calimport.Event {
	tz := ""
	if e.tzid != "" {
		tz = importZone(e.tzid)
	}
	return calimport.Event{
		SourceUID:          e.uid,
		Title:              e.summary,
		Location:           e.location,
		Memo:               e.desc,
		URL:                e.url,
		Start:              e.start,
		End:                e.end,
		AllDay:             e.allDay,
		Timezone:           tz,
		TimezoneUnknown:    e.tzUnknown,
		Visibility:         importVisibility(e.class),
		ShowAs:             importTransp(e.transp),
		NotificationOffset: importAlarm(e.alarmMinutes),
		RRule:              e.rrule,
		Exdates:            e.exdates,
		TooManyExdates:     e.tooManyExdates,
		Duration:           e.duration,
		HasDuration:        e.hasDuration,
		BadDuration:        e.badDuration,
		RecurrenceID:       e.recurrenceID,
		HasRecurrenceID:    e.hasRecurrenceID,
	}
}

// importZone keeps the source zone so wall-clock semantics (recurrence,
// all-day rendering) survive the import. A name nothing can resolve falls back
// to UTC to match how the times were parsed.
func importZone(tzid string) string {
	if name, ok := resolveTZID(tzid); ok {
		return name
	}
	return "UTC"
}
