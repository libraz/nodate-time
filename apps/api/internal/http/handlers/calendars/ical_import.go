package calendars

import (
	"context"
	"database/sql"
	"slices"

	"github.com/libraz/nodate-time/apps/api/internal/calimport"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/ical"
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

		body := []byte(in.Body.ICS)
		events, _ := ical.Parse(body, ical.ParseOptions{})
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
			if slices.ContainsFunc(e.Alarms, func(a ical.Alarm) bool { return a.HasOffset }) &&
				!converted[i].NotificationOffset.Valid {
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
			!ical.ContainsComponent(body)
		return out, nil
	}
}

// importEvent maps a parsed VEVENT onto the shape the store side takes. The
// zone is left empty when the file named none, so a changed occurrence then
// inherits its series' zone.
func importEvent(e ical.ParsedEvent) calimport.Event {
	return calimport.Event{
		SourceUID:          e.UID,
		Title:              e.Summary,
		Location:           e.Location,
		Memo:               e.Description,
		URL:                e.URL,
		Start:              e.Start,
		End:                e.End,
		AllDay:             e.AllDay,
		Timezone:           e.Zone(),
		TimezoneUnknown:    e.TZUnknown,
		Visibility:         importVisibility(e.Class),
		ShowAs:             importTransp(e.Transp),
		NotificationOffset: importAlarm(e.Alarms),
		RRule:              e.RRule,
		Exdates:            e.Exdates,
		TooManyExdates:     e.TooManyExdates,
		Duration:           e.Duration,
		HasDuration:        e.HasDuration,
		BadDuration:        e.BadDuration,
		RecurrenceID:       e.RecurrenceID,
		HasRecurrenceID:    e.HasRecurrenceID,
	}
}

// importAlarm is the reminder an event keeps. One the calendar cannot hold is
// left off rather than snapped to a neighbouring value; the result counts
// those it dropped.
func importAlarm(alarms []ical.Alarm) sql.NullInt32 {
	m, ok := ical.FirstExpressibleAlarm(alarms)
	return sql.NullInt32{Int32: m, Valid: ok}
}
