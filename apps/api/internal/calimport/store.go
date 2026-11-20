package calimport

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

	"github.com/libraz/nodate-time/apps/api/internal/attendees"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/eventstore"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

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
		if r.registers() && r.e.SourceUID == uid {
			return sql.NullString{}
		}
	}
	return sourceUID(uid)
}

// importResult is what became of one imported event, held until the
// transaction that wrote it commits.
type importResult struct {
	e       Event
	series  importedSeries
	outcome importOutcome
}

// registers reports whether a series head now answers for its UID: one this
// import wrote, or one the calendar already held.
func (r importResult) registers() bool {
	return r.e.SourceUID != "" && (r.outcome == importCreated || r.outcome == importDuplicate)
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
	ctx context.Context, s session, calendarID, userID uint32, events []Event,
	write func(q *generated.Queries, e Event, earlier []importResult) (importResult, error),
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
		err := dbtx.Run(ctx, s.db, func(q *generated.Queries) error {
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
				err := dbtx.Run(ctx, s.db, func(q *generated.Queries) error {
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

// importEndAt is DTEND, else DTSTART plus DURATION, else a default length.
// ok is false for a DURATION the event cannot take -- unreadable, or not whole
// days on an all-day event -- which is refused rather than replaced by a
// length the file did not say.
func importEndAt(e Event) (time.Time, bool) {
	if !e.End.IsZero() {
		return e.End, true
	}
	if e.HasDuration {
		if e.BadDuration {
			return time.Time{}, false
		}
		if e.AllDay {
			if e.Duration%(24*time.Hour) != 0 {
				return time.Time{}, false
			}
			// Calendar days, so a DST change inside the span does not move
			// the end off midnight.
			return e.Start.AddDate(0, 0, int(e.Duration/(24*time.Hour))), true
		}
		return e.Start.Add(e.Duration), true
	}
	if e.AllDay {
		return e.Start.AddDate(0, 0, 1), true
	}
	return e.Start.Add(time.Hour), true
}

// validateImported decides whether one VEVENT, series head or changed
// occurrence, is something the calendar can hold, and returns its end. A
// file's event without a title or a start has nothing to import; one whose
// values the calendar cannot store is rejected.
func validateImported(e Event) (time.Time, importOutcome, bool) {
	if e.Title == "" || e.Start.IsZero() {
		return time.Time{}, importSkipped, false
	}
	endAt, ok := importEndAt(e)
	if !ok || e.TooManyExdates || !storableInstant(e.Start) || !storableInstant(endAt) ||
		(e.HasRecurrenceID && !storableInstant(e.RecurrenceID)) {
		return time.Time{}, importRejected, false
	}
	return endAt, importCreated, true
}

// convertRRule maps an RFC 5545 RRULE value onto the stored rule JSON, or
// reports false for a rule the expander would expand to other dates, so the
// caller skips the event instead of importing a different series. start is
// DTSTART in the event's zone.
func convertRRule(value string, start time.Time) (*json.RawMessage, bool) {
	rule, ok := recurrence.ParseRRule(value, start)
	if !ok {
		return nil, false
	}
	data, err := json.Marshal(rule)
	if err != nil {
		return nil, false
	}
	raw := json.RawMessage(data)
	return &raw, true
}

// importSeriesHead writes one VEVENT that is not a changed occurrence.
//
// It runs in the transaction importChunked hands it, which keeps one bad row
// from discarding the rest of the file. Each event carries its own log entry:
// an import is a state change like any other, and a feed that skipped it would
// be missing however many events landed.
//
// The event is owned by the actor unless it names an owner. A .ics file has no
// notion of one, and filing the events under whoever ran the import is the
// honest answer -- they are who put them there.
//
// identity is what the event will be known by, and an event this calendar
// already holds under it is left exactly as it is. An import that overwrote
// what it recognised would undo whatever was changed here since, which is not
// what uploading the same file again asks for.
func importSeriesHead(
	ctx context.Context, s session, q *generated.Queries, cal generated.Calendar, userID uint32,
	e Event, identity sql.NullString,
) (importedSeries, importOutcome, error) {
	endAt, outcome, ok := validateImported(e)
	if !ok {
		return importedSeries{}, outcome, nil
	}
	tz := cmp.Or(e.Timezone, "UTC")

	var ruleData *json.RawMessage
	var exceptions *json.RawMessage
	recEnd := sql.NullTime{}
	if e.RRule != "" {
		converted, ok := convertRRule(e.RRule, e.Start.In(recurrence.LoadLocation(tz)))
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
			Time:  recurrence.ComputeEndInZone(rule, e.Start, endAt, tz),
			Valid: true,
		}
		// EXDATE says which occurrences the series does not have. Dropping it
		// hands back every occurrence its author cancelled.
		// The parser already made them distinct.
		column, err := recurrence.Exceptions(e.Exdates).MarshalColumn()
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

	created, err := eventstore.Insert(ctx, q, generated.CreateCalendarEventParams{
		PublicID:           pubID[:],
		WorkspaceID:        s.workspaceID,
		CalendarID:         cal.ID,
		Kind:               generated.CalendarEventsKindEvent,
		Visibility:         e.Visibility,
		ShowAs:             e.ShowAs,
		Flexibility:        generated.CalendarEventsFlexibilityFixed,
		Title:              e.Title,
		AllDay:             e.AllDay,
		StartAt:            sql.NullTime{Time: e.Start, Valid: true},
		EndAt:              sql.NullTime{Time: endAt, Valid: true},
		Timezone:           tz,
		Location:           nullString(e.Location),
		Memo:               nullString(e.Memo),
		URL:                nullString(e.URL),
		OwnerUserID:        cmp.Or(e.OwnerUserID, userID),
		CreatedByUserID:    userID,
		NotificationOffset: e.NotificationOffset,
		RecurrenceRule:     ruleData,
		RecurrenceEnd:      recEnd,
		SourceUid:          identity,
	}, e.AttendeeUserIDs, map[string]any{"source": s.source})
	if err != nil {
		return importedSeries{}, importFailed, err
	}
	newID := created.ID
	if exceptions != nil {
		if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
			RecurrenceExceptions: exceptions,
			ID:                   newID,
		}); err != nil {
			return importedSeries{}, importFailed, err
		}
	}
	return importedSeries{
		id: newID, publicID: pubID[:], title: e.Title, start: e.Start, end: endAt,
		timezone: tz, rule: ruleData, exceptions: e.Exdates,
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
	ctx context.Context, s session, q *generated.Queries, cal generated.Calendar, userID uint32,
	e Event, parent importedSeries,
) (importOutcome, error) {
	// An override inherits what it does not set, the title included.
	e.Title = cmp.Or(e.Title, parent.title)
	endAt, outcome, ok := validateImported(e)
	if !ok {
		return outcome, nil
	}
	parentRule, err := recurrence.ParseRule(*parent.rule)
	if err != nil {
		return importFailed, nil
	}
	if parent.exceptions.Contains(e.RecurrenceID) ||
		!recurrence.OccursAt(parentRule, parent.start, parent.end,
			parent.timezone, e.RecurrenceID) {
		return importSkipped, nil
	}
	overridePubID, err := uuid.NewV7()
	if err != nil {
		return importFailed, nil
	}
	tz := cmp.Or(e.Timezone, parent.timezone)
	parentRef := sql.NullInt32{Int32: int32(parent.id), Valid: true}
	originalRef := sql.NullTime{Time: e.RecurrenceID, Valid: true}

	if _, err := calresolve.LockEvent(ctx, q, cal.ID, parent.id); err != nil {
		return importFailed, err
	}
	res, err := q.UpsertRecurrenceOverride(ctx, generated.UpsertRecurrenceOverrideParams{
		PublicID:                overridePubID[:],
		WorkspaceID:             s.workspaceID,
		CalendarID:              cal.ID,
		Kind:                    generated.CalendarEventsKindEvent,
		Visibility:              e.Visibility,
		ShowAs:                  e.ShowAs,
		Flexibility:             generated.CalendarEventsFlexibilityFixed,
		Title:                   e.Title,
		AllDay:                  e.AllDay,
		StartAt:                 sql.NullTime{Time: e.Start, Valid: true},
		EndAt:                   sql.NullTime{Time: endAt, Valid: true},
		Timezone:                tz,
		Location:                nullString(e.Location),
		Memo:                    nullString(e.Memo),
		URL:                     nullString(e.URL),
		OwnerUserID:             cmp.Or(e.OwnerUserID, userID),
		CreatedByUserID:         userID,
		NotificationOffset:      e.NotificationOffset,
		RecurrenceParentID:      parentRef,
		RecurrenceOriginalStart: originalRef,
	})
	if err != nil {
		return importFailed, err
	}
	// One row affected is a new override; two means the same occurrence came
	// twice in this import and the row, with its participants, already exists.
	if affected, err := res.RowsAffected(); err != nil {
		return importFailed, err
	} else if affected == 1 {
		id, err := res.LastInsertId()
		if err != nil {
			return importFailed, err
		}
		if err := attendees.AddToNew(ctx, q, s.workspaceID, uint32(id), e.AttendeeUserIDs); err != nil {
			return importFailed, err
		}
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
		WorkspaceID: s.workspaceID,
		CalendarID:  cal.ID,
		ActorUserID: userID,
		Type:        eventlog.TypeEventUpdated,
		Title:       e.Title,
		Subject:     parent.publicID,
		Extra: map[string]any{
			"scope":         "occurrence",
			"originalStart": e.RecurrenceID.UTC().Format(time.RFC3339),
			"source":        s.source,
		},
	}); err != nil {
		return importFailed, err
	}
	return importCreated, nil
}
