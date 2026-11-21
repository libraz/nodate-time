// Package calimport writes parsed events onto a calendar. The iCal upload and
// the batch importers share it so that duplicate recognition, chunked
// transactions, write locking and event-log entries live in one place.
package calimport

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/attendees"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// MaxExdates bounds the cancellations one imported series may carry.
// Every listing and export of the series reads the whole list, so a file
// naming hundreds of thousands of them is refused rather than stored.
const MaxExdates = 1000

// MaxMemoRunes is the memo length the events API accepts. A longer
// description is cut to it rather than stored as a memo no edit could save.
const MaxMemoRunes = 16000

// Event is one event to import. Changed occurrences (HasRecurrenceID) are
// matched to their series by SourceUID.
type Event struct {
	SourceUID                  string
	Title, Location, Memo, URL string
	// End zero means the default length: a day when AllDay, an hour otherwise.
	Start, End time.Time
	AllDay     bool
	// Timezone is an IANA name resolved by the caller; "" means UTC.
	Timezone           string
	TimezoneUnknown    bool
	Visibility         generated.CalendarEventsVisibility
	ShowAs             generated.CalendarEventsShowAs
	NotificationOffset sql.NullInt32
	// RRule is an RFC 5545 RRULE value without the "RRULE:" prefix.
	RRule string
	// Exdates are distinct, UTC and at most MaxExdates; TooManyExdates records
	// that the source named more.
	Exdates                  []time.Time
	TooManyExdates           bool
	Duration                 time.Duration
	HasDuration, BadDuration bool
	RecurrenceID             time.Time
	HasRecurrenceID          bool
	// OwnerUserID 0 means the actor; otherwise the caller guarantees the
	// user is a member of the calendar.
	OwnerUserID uint32
	// Attendees are the event's participants, distinct and each a member of
	// the calendar; the caller guarantees both.
	Attendees []attendees.Person
}

// Result counts what became of the events handed to Run. Rejected is the part
// of Failed the data itself caused; every event lands in exactly one of
// Imported, Skipped, Failed or Duplicates.
type Result struct {
	Imported, Skipped, Failed, Rejected, Duplicates, UnknownTimezones int
}

// Run writes events to cal as actorUserID. source labels the event-log entries.
func Run(
	ctx context.Context, db *sql.DB, workspaceID uint32, cal generated.Calendar,
	actorUserID uint32, events []Event, source string,
) Result {
	s := session{db: db, workspaceID: workspaceID, source: source}
	var res Result
	// count records one event's outcome. A rejection is a failure as well:
	// the event did not land either way, and the second counter only says
	// whose fault that was.
	count := func(outcome importOutcome) {
		switch outcome {
		case importCreated:
			res.Imported++
		case importSkipped:
			res.Skipped++
		case importRejected:
			res.Failed++
			res.Rejected++
		case importFailed:
			res.Failed++
		case importDuplicate:
			res.Duplicates++
		default:
			panic(fmt.Sprintf("calimport: outcome %d is not one Run produces", outcome))
		}
	}

	// Series heads are written first: a changed occurrence names the one
	// it belongs to by UID, and the file may put it either side of it.
	seriesByUID := map[string]importedSeries{}
	var heads, changed []Event
	for _, e := range events {
		if e.HasRecurrenceID {
			changed = append(changed, e)
		} else {
			heads = append(heads, e)
		}
	}

	record := func(r importResult) {
		count(r.outcome)
		if r.outcome == importCreated && r.e.TimezoneUnknown {
			res.UnknownTimezones++
		}
	}

	written := chunked(ctx, s, cal.ID, actorUserID, heads,
		func(q *generated.Queries, e Event, earlier []importResult) (importResult, error) {
			series, outcome, err := importSeriesHead(ctx, s, q, cal, actorUserID, e,
				importIdentity(e.SourceUID, seriesByUID, earlier))
			return importResult{e: e, series: series, outcome: outcome}, err
		},
		func(r importResult) {
			record(r)
			// A recognised series is registered like a written one: its
			// changed occurrences have to find it, and find out that it
			// was left alone.
			if r.registers() {
				seriesByUID[r.e.SourceUID] = r.series
			}
		},
		failedImport)
	res.Failed += len(heads) - written

	var overrides []Event
	for _, e := range changed {
		parent, ok := seriesByUID[e.SourceUID]
		if !ok || parent.rule == nil {
			// Nothing in the file for it to be a departure from. Importing
			// it as a free-standing event would put a duplicate next to
			// whichever occurrence it was meant to replace.
			res.Skipped++
			continue
		}
		if parent.recognised {
			// The series is already on the calendar, so its departures from
			// it are already here too. Writing this one anyway would apply
			// it over the occurrence the earlier import left, which is half
			// the file landing -- worse than taking all of it or none.
			res.Duplicates++
			continue
		}
		overrides = append(overrides, e)
	}

	written = chunked(ctx, s, cal.ID, actorUserID, overrides,
		func(q *generated.Queries, e Event, _ []importResult) (importResult, error) {
			outcome, err := importChangedOccurrence(ctx, s, q, cal, actorUserID, e, seriesByUID[e.SourceUID])
			return importResult{e: e, outcome: outcome}, err
		},
		record, failedImport)
	res.Failed += len(overrides) - written
	return res
}

// session is what every write of one Run shares.
type session struct {
	db          *sql.DB
	workspaceID uint32
	source      string
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
