package calendars

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
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
		// Unreadable says the body held nothing this parser recognised as
		// iCalendar. Without it, a file whose shape cannot be read answers
		// exactly like a calendar that genuinely had no events in it: every
		// counter zero, and no way to tell which happened.
		Unreadable bool `json:"unreadable"`
	}
}

// importAlarm maps a parsed alarm onto the stored offset, accepting exactly
// the reminders the events API does. An alarm outside them would be invisible
// in the picker, and the next edit through it would clear it without anyone
// choosing to, so it is left off rather than snapped to a neighbouring value;
// dropping it here at least happens where the import result can report it.
func importAlarm(minutes *int32) sql.NullInt32 {
	if minutes == nil || !events.SupportedNotificationOffset(int(*minutes)) {
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
			return nil, toAPIError(err)
		}

		events := parseICS(in.Body.ICS)
		truncated := 0
		if len(events) > importMaxEvents {
			// Counted, not swallowed. A file that lost a thousand events must
			// not read as a clean import.
			truncated = len(events) - importMaxEvents
			events = events[:importMaxEvents]
		}

		var imported, skipped, failed, rejected, duplicates, unknownZones int
		// count records one event's outcome. A rejection is a failure as well:
		// the event did not land either way, and the second counter only says
		// whose fault that was.
		count := func(outcome importOutcome) {
			switch outcome {
			case importSkipped:
				skipped++
			case importRejected:
				failed++
				rejected++
			case importFailed:
				failed++
			case importDuplicate:
				duplicates++
			default:
				imported++
			}
		}

		// Series heads are written first: a changed occurrence names the one
		// it belongs to by UID, and the file may put it either side of it.
		seriesByUID := map[string]importedSeries{}
		var heads, changed []rawEvent
		for _, e := range events {
			if e.hasRecurrenceID {
				changed = append(changed, e)
			} else {
				heads = append(heads, e)
			}
		}

		record := func(r importResult) {
			count(r.outcome)
			if r.outcome == importCreated && r.e.tzUnknown {
				unknownZones++
			}
		}

		written := importChunked(ctx, deps, cal.ID, userID, heads,
			func(q *generated.Queries, e rawEvent, earlier []importResult) (importResult, error) {
				series, outcome, err := importSeriesHead(ctx, deps, q, cal, userID, e,
					importIdentity(e.uid, seriesByUID, earlier))
				return importResult{e: e, series: series, outcome: outcome}, err
			},
			func(r importResult) {
				record(r)
				// A recognised series is registered like a written one: its
				// changed occurrences have to find it, and find out that it
				// was left alone.
				if r.registers() {
					seriesByUID[r.e.uid] = r.series
				}
			})
		failed += len(heads) - written

		var overrides []rawEvent
		for _, e := range changed {
			parent, ok := seriesByUID[e.uid]
			if !ok || parent.rule == nil {
				// Nothing in the file for it to be a departure from. Importing
				// it as a free-standing event would put a duplicate next to
				// whichever occurrence it was meant to replace.
				skipped++
				continue
			}
			if parent.recognised {
				// The series is already on the calendar, so its departures from
				// it are already here too. Writing this one anyway would apply
				// it over the occurrence the earlier import left, which is half
				// the file landing -- worse than taking all of it or none.
				duplicates++
				continue
			}
			overrides = append(overrides, e)
		}

		written = importChunked(ctx, deps, cal.ID, userID, overrides,
			func(q *generated.Queries, e rawEvent, _ []importResult) (importResult, error) {
				outcome, err := importChangedOccurrence(ctx, deps, q, cal, userID, e, seriesByUID[e.uid])
				return importResult{e: e, outcome: outcome}, err
			},
			record)
		failed += len(overrides) - written

		out := &ImportOutput{}
		out.Body.Imported = imported
		out.Body.Skipped = skipped
		out.Body.Failed = failed
		out.Body.Rejected = rejected
		out.Body.Duplicates = duplicates
		out.Body.Truncated = truncated
		out.Body.UnknownTimezones = unknownZones
		out.Body.Unreadable = imported+skipped+failed+duplicates+truncated == 0 &&
			!containsICSComponent(in.Body.ICS)
		return out, nil
	}
}

// importedSeries is what a changed occurrence needs to attach itself to the
// series it belongs to, and to check that it names an occurrence the series
// has.
type importedSeries struct {
	id         uint32
	publicID   []byte
	title      string
	start      time.Time
	end        time.Time
	timezone   string
	rule       *json.RawMessage
	exceptions recurrence.Exceptions
	// recognised marks a series the calendar already held, which this import
	// left alone. Its changed occurrences have to be left alone with it: the
	// override write applies itself in place, so taking those while the series
	// keeps the values it already had would land half the file.
	recognised bool
}

// importIdentity decides what an event will be known by on this calendar.
//
// A UID answers for one event per import, whether that event was written or
// recognised. A file is free to repeat a UID however badly, and the second
// event under it is a second event: matching it against the same row would
// leave one of them off the calendar, and once the other is deleted no
// re-upload could put it back. Written without an identity it simply imports
// again next time -- the half of the choice that can be undone.
//
// earlier is the series heads written before this one in the same transaction,
// which seen does not hold until it commits.
func importIdentity(uid string, seen map[string]importedSeries, earlier []importResult) sql.NullString {
	if _, taken := seen[uid]; taken {
		return sql.NullString{}
	}
	for _, r := range earlier {
		if r.registers() && r.e.uid == uid {
			return sql.NullString{}
		}
	}
	return sourceUID(uid)
}

// importResult is what became of one imported event, held until the
// transaction that wrote it commits.
type importResult struct {
	e       rawEvent
	series  importedSeries
	outcome importOutcome
}

// registers reports whether a series head now answers for its UID: one this
// import wrote, or one the calendar already held.
func (r importResult) registers() bool {
	return r.e.uid != "" && (r.outcome == importCreated || r.outcome == importDuplicate)
}

// importChunkSize is how many events share one transaction. A commit and a
// lock round trip per event is what put a file at the event cap past the
// server's write timeout.
const importChunkSize = 100

// importLockError is a transaction that could not take the importer's write
// lock: the calendar is gone, or they may no longer write to it.
type importLockError struct{ err error }

func (e *importLockError) Error() string { return e.err.Error() }
func (e *importLockError) Unwrap() error { return e.err }

// importChunked writes events importChunkSize to a transaction, each under the
// importer's write lock. A chunk any write fails in is rolled back and written
// again one event per transaction, so a row the store refuses costs only
// itself.
//
// write is handed the results earlier in its chunk, which are exactly what its
// transaction will keep; keep is handed each result, in order, once it has
// committed. It returns how many events were dealt with, which falls short of
// len(events) only when the write lock was lost and the rest were left alone.
func importChunked(
	ctx context.Context, deps Deps, calendarID, userID uint32, events []rawEvent,
	write func(q *generated.Queries, e rawEvent, earlier []importResult) (importResult, error),
	keep func(importResult),
) int {
	lock := func(q *generated.Queries) error {
		if _, err := calresolve.LockWrite(ctx, q, calendarID, userID); err != nil {
			return &importLockError{err}
		}
		return nil
	}
	var lockErr *importLockError
	for lo := 0; lo < len(events); lo += importChunkSize {
		chunk := events[lo:min(lo+importChunkSize, len(events))]
		var batch []importResult
		err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			batch = batch[:0]
			if err := lock(q); err != nil {
				return err
			}
			for _, e := range chunk {
				r, err := write(q, e, batch)
				if err != nil {
					return err
				}
				batch = append(batch, r)
			}
			return nil
		})
		if errors.As(err, &lockErr) {
			return lo
		}
		if err != nil {
			batch = batch[:0]
			for _, e := range chunk {
				var r importResult
				err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
					if err := lock(q); err != nil {
						return err
					}
					var err error
					r, err = write(q, e, batch)
					return err
				})
				if errors.As(err, &lockErr) {
					for _, r := range batch {
						keep(r)
					}
					return lo + len(batch)
				}
				if err != nil {
					r = importResult{e: e, outcome: storeOutcome(err)}
				}
				batch = append(batch, r)
			}
		}
		for _, r := range batch {
			keep(r)
		}
	}
	return len(events)
}

type importOutcome int

const (
	importCreated importOutcome = iota
	importSkipped
	importFailed
	// importRejected is a failure the file caused: a value the calendar cannot
	// hold, whatever the caller does with it. It is told apart from a plain
	// failure because "failed" invites a retry, and a retry of these can only
	// end the same way.
	importRejected
	// importDuplicate is an event this calendar already holds under the name
	// the file gave it. Nothing was written and nothing was wrong.
	importDuplicate
)

// mysqlDataErrors are the codes MySQL answers with when the value is what it
// objects to: too long for its column, outside what the type can name, or
// against a constraint the table declares.
var mysqlDataErrors = map[uint16]bool{
	1264: true, // out of range value
	1265: true, // data truncated for column
	1292: true, // incorrect value for a date or time column
	1366: true, // incorrect string value for the column's character set
	1406: true, // data too long for column
	3819: true, // check constraint violated
}

// storableInstant reports whether an instant is one the store can hold. The
// columns are DATETIMEs, which name the years 1 through 9999, and the driver
// refuses anything outside that before it ever reaches MySQL -- so the check
// belongs here, where the event can be counted as one the file put outside the
// calendar rather than as a failure with nothing to say about it.
func storableInstant(t time.Time) bool {
	return t.Year() >= 1 && t.Year() <= 9999
}

// isDataRejection reports whether the store refused a row over the data in it
// rather than over something that went wrong on this side.
func isDataRejection(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlDataErrors[mysqlErr.Number]
	}
	return false
}

// storeOutcome names what happened to one event the store would not take.
//
// A duplicate key on the source UID is the calendar saying it already holds
// this event: the lookup before the insert answers that question for anything
// already committed, and this covers the two it cannot see -- a file naming
// the same event twice, and a second import running alongside this one.
func storeOutcome(err error) importOutcome {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlDuplicateKey {
		return importDuplicate
	}
	if isDataRejection(err) {
		return importRejected
	}
	return importFailed
}

// mysqlDuplicateKey is the code for a write refused by a unique constraint.
const mysqlDuplicateKey = 1062

// sourceUID is the identity an imported event keeps, so a second upload of the
// same file can recognise it rather than writing another copy.
//
// A UID longer than the column is left off instead of cut down: two different
// UIDs sharing a prefix would merge two unrelated events, and an event that
// merely imports twice can be deleted while one that was never written cannot
// be got back.
func sourceUID(uid string) sql.NullString {
	if uid == "" || utf8.RuneCountInString(uid) > sourceUIDMaxRunes {
		return sql.NullString{}
	}
	return sql.NullString{String: uid, Valid: true}
}

// sourceUIDMaxRunes is what calendar_events.source_uid holds.
const sourceUIDMaxRunes = 255

// importZone keeps the source zone so wall-clock semantics (recurrence,
// all-day rendering) survive the import. A name nothing can resolve falls back
// to UTC to match how the times were parsed.
func importZone(tzid string) string {
	if name, ok := resolveTZID(tzid); ok {
		return name
	}
	return "UTC"
}

// importEndAt is DTEND, else DTSTART plus DURATION, else a default length.
// ok is false for a DURATION the event cannot take -- unreadable, or not whole
// days on an all-day event -- which is refused rather than replaced by a
// length the file did not say.
func importEndAt(e rawEvent) (time.Time, bool) {
	if !e.end.IsZero() {
		return e.end, true
	}
	if e.hasDuration {
		if e.badDuration {
			return time.Time{}, false
		}
		if e.allDay {
			if e.duration%(24*time.Hour) != 0 {
				return time.Time{}, false
			}
			// Calendar days, so a DST change inside the span does not move
			// the end off midnight.
			return e.start.AddDate(0, 0, int(e.duration/(24*time.Hour))), true
		}
		return e.start.Add(e.duration), true
	}
	if e.allDay {
		return e.start.AddDate(0, 0, 1), true
	}
	return e.start.Add(time.Hour), true
}

// validateImported decides whether one VEVENT, series head or changed
// occurrence, is something the calendar can hold, and returns its end. A
// file's event without a title or a start has nothing to import; one whose
// values the calendar cannot store is rejected.
func validateImported(e rawEvent) (time.Time, importOutcome, bool) {
	if e.summary == "" || e.start.IsZero() {
		return time.Time{}, importSkipped, false
	}
	endAt, ok := importEndAt(e)
	if !ok || e.tooManyExdates || !storableInstant(e.start) || !storableInstant(endAt) ||
		(e.hasRecurrenceID && !storableInstant(e.recurrenceID)) {
		return time.Time{}, importRejected, false
	}
	return endAt, importCreated, true
}

// importSeriesHead writes one VEVENT that is not a changed occurrence.
//
// It runs in the transaction importChunked hands it, which keeps one bad row
// from discarding the rest of the file. Each event carries its own log entry:
// an import is a state change like any other, and a feed that skipped it would
// be missing however many events landed.
//
// The importing user is recorded as the owner. A .ics file has no notion of
// one, and filing the events under whoever ran the import is the honest
// answer -- they are who put them there.
//
// identity is what the event will be known by, and an event this calendar
// already holds under it is left exactly as it is. An import that overwrote
// what it recognised would undo whatever was changed here since, which is not
// what uploading the same file again asks for.
func importSeriesHead(
	ctx context.Context, deps Deps, q *generated.Queries, cal generated.Calendar, userID uint32,
	e rawEvent, identity sql.NullString,
) (importedSeries, importOutcome, error) {
	endAt, outcome, ok := validateImported(e)
	if !ok {
		return importedSeries{}, outcome, nil
	}
	tz := importZone(e.tzid)

	var ruleData *json.RawMessage
	var exceptions *json.RawMessage
	recEnd := sql.NullTime{}
	if e.rrule != "" {
		converted, ok := convertRRule(e.rrule, e.start.In(loadLocationOrUTC(tz)))
		if !ok {
			// An unsupported RRULE must not silently collapse a recurring
			// event into a single occurrence.
			return importedSeries{}, importSkipped, nil
		}
		ruleData = converted
		rule, err := recurrence.ParseRule(*ruleData)
		if err != nil {
			return importedSeries{}, importFailed, nil
		}
		recEnd = sql.NullTime{
			Time:  recurrence.ComputeEndInZone(rule, e.start, endAt, tz),
			Valid: true,
		}
		// EXDATE says which occurrences the series does not have. Dropping it
		// hands back every occurrence its author cancelled.
		// The parser already made them distinct.
		column, err := recurrence.Exceptions(e.exdates).MarshalColumn()
		if err != nil {
			return importedSeries{}, importFailed, nil
		}
		exceptions = column
	}
	if recEnd.Valid && !storableInstant(recEnd.Time) {
		return importedSeries{}, importRejected, nil
	}

	// Asked after the event is known to be one this import would write, so an
	// event it was going to skip is reported as skipped rather than as one the
	// calendar already has.
	if identity.Valid {
		existing, err := q.FindCalendarEventBySourceUID(ctx,
			generated.FindCalendarEventBySourceUIDParams{CalendarID: cal.ID, SourceUid: identity})
		switch {
		case err == nil:
			return importedSeries{
				id:         existing.ID,
				publicID:   existing.PublicID,
				title:      existing.Title,
				start:      existing.StartAt.Time,
				end:        existing.EndAt.Time,
				timezone:   existing.Timezone,
				rule:       existing.RecurrenceRule,
				exceptions: recurrence.ParseExceptions(existing.RecurrenceExceptions),
				recognised: true,
			}, importDuplicate, nil
		case !errors.Is(err, sql.ErrNoRows):
			return importedSeries{}, importFailed, err
		}
	}

	pubID, err := uuid.NewV7()
	if err != nil {
		return importedSeries{}, importFailed, nil
	}

	res, err := q.CreateCalendarEvent(ctx, generated.CreateCalendarEventParams{
		PublicID:           pubID[:],
		WorkspaceID:        deps.WorkspaceID,
		CalendarID:         cal.ID,
		Kind:               generated.CalendarEventsKindEvent,
		Visibility:         importVisibility(e.class),
		ShowAs:             importTransp(e.transp),
		Flexibility:        generated.CalendarEventsFlexibilityFixed,
		Title:              e.summary,
		AllDay:             e.allDay,
		StartAt:            sql.NullTime{Time: e.start, Valid: true},
		EndAt:              sql.NullTime{Time: endAt, Valid: true},
		Timezone:           tz,
		Location:           nullString(e.location),
		Memo:               nullString(e.desc),
		URL:                nullString(e.url),
		OwnerUserID:        userID,
		CreatedByUserID:    userID,
		NotificationOffset: importAlarm(e.alarmMinutes),
		RecurrenceRule:     ruleData,
		RecurrenceEnd:      recEnd,
		SourceUid:          identity,
	})
	if err != nil {
		return importedSeries{}, importFailed, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return importedSeries{}, importFailed, err
	}
	newID := uint32(id)
	if exceptions != nil {
		if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
			RecurrenceExceptions: exceptions,
			ID:                   newID,
		}); err != nil {
			return importedSeries{}, importFailed, err
		}
	}
	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  cal.ID,
		ActorUserID: userID,
		Type:        eventlog.TypeEventCreated,
		Title:       e.summary,
		Subject:     pubID[:],
		Extra:       map[string]any{"source": "ics-import"},
	}); err != nil {
		return importedSeries{}, importFailed, err
	}
	return importedSeries{
		id: newID, publicID: pubID[:], title: e.summary, start: e.start, end: endAt,
		timezone: tz, rule: ruleData, exceptions: e.exdates,
	}, importCreated, nil
}

// importChangedOccurrence writes a VEVENT carrying a RECURRENCE-ID as an
// override of the series it names, which is how the app stores a changed
// occurrence. Written as a free-standing event it would show up beside the
// occurrence it was meant to replace.
//
// It is held to the checks a series head is, and to one more: the occurrence
// it names has to be one the series has. A row standing in for an occurrence
// the rule does not produce, or one the file cancels, is never listed, so it
// is skipped rather than stored and counted.
func importChangedOccurrence(
	ctx context.Context, deps Deps, q *generated.Queries, cal generated.Calendar, userID uint32,
	e rawEvent, parent importedSeries,
) (importOutcome, error) {
	// An override inherits what it does not set, the title included.
	e.summary = cmp.Or(e.summary, parent.title)
	endAt, outcome, ok := validateImported(e)
	if !ok {
		return outcome, nil
	}
	parentRule, err := recurrence.ParseRule(*parent.rule)
	if err != nil {
		return importFailed, nil
	}
	if parent.exceptions.Contains(e.recurrenceID) ||
		!recurrence.OccursAt(parentRule, parent.start, parent.end,
			parent.timezone, e.recurrenceID) {
		return importSkipped, nil
	}
	overridePubID, err := uuid.NewV7()
	if err != nil {
		return importFailed, nil
	}
	tz := parent.timezone
	if e.tzid != "" {
		tz = importZone(e.tzid)
	}
	parentRef := sql.NullInt32{Int32: int32(parent.id), Valid: true}
	originalRef := sql.NullTime{Time: e.recurrenceID, Valid: true}

	if _, err := calresolve.LockEvent(ctx, q, cal.ID, parent.id); err != nil {
		return importFailed, err
	}
	if _, err := q.UpsertRecurrenceOverride(ctx, generated.UpsertRecurrenceOverrideParams{
		PublicID:                overridePubID[:],
		WorkspaceID:             deps.WorkspaceID,
		CalendarID:              cal.ID,
		Kind:                    generated.CalendarEventsKindEvent,
		Visibility:              importVisibility(e.class),
		ShowAs:                  importTransp(e.transp),
		Flexibility:             generated.CalendarEventsFlexibilityFixed,
		Title:                   e.summary,
		AllDay:                  e.allDay,
		StartAt:                 sql.NullTime{Time: e.start, Valid: true},
		EndAt:                   sql.NullTime{Time: endAt, Valid: true},
		Timezone:                tz,
		Location:                nullString(e.location),
		Memo:                    nullString(e.desc),
		URL:                     nullString(e.url),
		OwnerUserID:             userID,
		CreatedByUserID:         userID,
		NotificationOffset:      importAlarm(e.alarmMinutes),
		RecurrenceParentID:      parentRef,
		RecurrenceOriginalStart: originalRef,
	}); err != nil {
		return importFailed, err
	}
	// An occurrence moved past the series' end would otherwise fall out
	// of every range query, which selects series by that column.
	if err := q.ExtendRecurrenceEnd(ctx, generated.ExtendRecurrenceEndParams{
		RecurrenceEnd: sql.NullTime{Time: endAt, Valid: true},
		ID:            parent.id,
		Boundary:      sql.NullTime{Time: endAt, Valid: true},
	}); err != nil {
		return importFailed, err
	}
	// Logged the way a changed occurrence made through the API is: against
	// the series, naming the occurrence.
	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  cal.ID,
		ActorUserID: userID,
		Type:        eventlog.TypeEventUpdated,
		Title:       e.summary,
		Subject:     parent.publicID,
		Extra: map[string]any{
			"scope":         "occurrence",
			"originalStart": e.recurrenceID.UTC().Format(time.RFC3339),
			"source":        "ics-import",
		},
	}); err != nil {
		return importFailed, err
	}
	return importCreated, nil
}
