package calimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/libraz/nodate-time/apps/api/internal/attendees"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/eventstore"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// SyncPlan is what a Sync of one calendar would do, read without writing.
type SyncPlan struct {
	Create, Update, Unchanged, Kept int
	// Delete is the synced rows whose UID the source no longer has.
	Delete []uint32
	// DeleteUIDs is the UIDs of the Delete rows, in the same order.
	DeleteUIDs []string
	// Synced is the rows the source created here; Orphaned is the rows that
	// carry a UID but no creation entry naming the source.
	Synced, Orphaned int
}

// PlanSync reads what Sync would do to cal. present is every UID the source
// still has, events the ones among them that converted. force names UIDs whose
// owner or attendees do not exist yet, counted as changed without comparing.
func PlanSync(
	ctx context.Context, q *generated.Queries, cal generated.Calendar, actorUserID uint32,
	events []Event, present, force map[string]bool, source string,
) (SyncPlan, error) {
	var plan SyncPlan
	rows, err := q.ListSourcedCalendarEvents(ctx, generated.ListSourcedCalendarEventsParams{
		CalendarID: cal.ID, Source: json.RawMessage(source),
	})
	if err != nil {
		return SyncPlan{}, err
	}
	identified, err := q.CountCalendarEventsWithSourceUID(ctx, cal.ID)
	if err != nil {
		return SyncPlan{}, err
	}
	plan.Synced, plan.Orphaned = len(rows), int(identified)-len(rows)

	synced := make(map[string]generated.CalendarEvent, len(rows))
	for _, row := range rows {
		uid := row.SourceUidKey.String
		synced[uid] = row
		visible, err := visibleToImporter(ctx, q, cal, actorUserID, row)
		if err != nil {
			return SyncPlan{}, err
		}
		if !visible {
			continue
		}
		if !present[uid] {
			plan.Delete = append(plan.Delete, row.ID)
			plan.DeleteUIDs = append(plan.DeleteUIDs, uid)
		}
	}

	for _, e := range events {
		identity := sourceUID(e.SourceUID)
		if !identity.Valid {
			continue
		}
		h, outcome := buildHead(e, actorUserID)
		row, isSynced := synced[identity.String]
		if isSynced {
			if visible, err := visibleToImporter(ctx, q, cal, actorUserID, row); err != nil {
				return SyncPlan{}, err
			} else if !visible {
				plan.Kept++
				continue
			}
		}
		if !isSynced {
			_, err := q.FindCalendarEventBySourceUID(ctx,
				generated.FindCalendarEventBySourceUIDParams{CalendarID: cal.ID, SourceUid: identity})
			switch {
			case err == nil:
				plan.Kept++
			case !errors.Is(err, sql.ErrNoRows):
				return SyncPlan{}, err
			case outcome == importCreated:
				plan.Create++
			}
			continue
		}
		if outcome != importCreated {
			plan.Kept++
			continue
		}
		if force[e.SourceUID] {
			plan.Update++
			continue
		}
		changed, _, err := compare(ctx, q, row, h)
		if err != nil {
			return SyncPlan{}, err
		}
		if changed {
			plan.Update++
		} else {
			plan.Unchanged++
		}
	}

	kept, err := keptUnconverted(ctx, q, cal, events, present)
	if err != nil {
		return SyncPlan{}, err
	}
	plan.Kept += kept
	return plan, nil
}

// SyncResult counts what became of the events handed to Sync. Duplicates are
// synced events left unchanged.
type SyncResult struct {
	Result
	Updated, Deleted, Kept int
}

// Sync makes cal follow the source: it soft-deletes the rows of deleteIDs
// that still qualify, then inserts or rewrites events. present and source are
// as for PlanSync; each UID appears in events at most once.
func Sync(
	ctx context.Context, db *sql.DB, workspaceID uint32, cal generated.Calendar, actorUserID uint32,
	events []Event, present map[string]bool, deleteIDs []uint32, source string,
) SyncResult {
	s := session{db: db, workspaceID: workspaceID, source: source}
	var res SyncResult

	// Deletes go first, so a UID they free is free for the inserts after.
	written := chunked(ctx, s, cal.ID, actorUserID, deleteIDs,
		func(q *generated.Queries, id uint32, _ []deleteResult) (deleteResult, error) {
			return syncDelete(ctx, s, q, cal, actorUserID, id, present)
		},
		func(r deleteResult) {
			if r.failed {
				res.Failed++
			} else if r.deleted {
				res.Deleted++
			}
		},
		func(uint32, error) deleteResult { return deleteResult{failed: true} })
	res.Failed += len(deleteIDs) - written

	count := func(r importResult) {
		switch r.outcome {
		case importCreated:
			res.Imported++
			if r.e.TimezoneUnknown {
				res.UnknownTimezones++
			}
		case importSkipped:
			res.Skipped++
		case importRejected:
			res.Failed++
			res.Rejected++
		case importFailed:
			res.Failed++
		case importDuplicate:
			res.Duplicates++
		case importUpdated:
			res.Updated++
		case importKept:
			res.Kept++
		default:
			panic(fmt.Sprintf("calimport: unknown outcome %d", r.outcome))
		}
	}
	written = chunked(ctx, s, cal.ID, actorUserID, events,
		func(q *generated.Queries, e Event, _ []importResult) (importResult, error) {
			outcome, err := syncHead(ctx, s, q, cal, actorUserID, e)
			return importResult{e: e, outcome: outcome}, err
		},
		count, failedImport)
	res.Failed += len(events) - written

	kept, err := keptUnconverted(ctx, generated.New(db), cal, events, present)
	if err != nil {
		res.Failed++
	}
	res.Kept += kept
	return res
}

// deleteResult is what became of one row a sync was asked to delete. Neither
// flag is set for a row that no longer qualified.
type deleteResult struct{ deleted, failed bool }

// syncDelete soft-deletes one synced row under its row lock, if it is still
// live, still the source's and still absent from it.
func syncDelete(
	ctx context.Context, s session, q *generated.Queries, cal generated.Calendar, userID, id uint32,
	present map[string]bool,
) (deleteResult, error) {
	row, err := calresolve.LockEvent(ctx, q, cal.ID, id)
	if errors.Is(err, apierrors.EventNotFound) {
		return deleteResult{}, nil
	}
	if err != nil {
		return deleteResult{}, err
	}
	visible, err := visibleToImporter(ctx, q, cal, userID, row)
	if err != nil || !visible {
		return deleteResult{}, err
	}
	if row.TaskID.Valid || !row.SourceUidKey.Valid || present[row.SourceUidKey.String] {
		return deleteResult{}, nil
	}
	fromSource, err := isFromSource(ctx, s, q, row)
	if err != nil || !fromSource {
		return deleteResult{}, err
	}
	if err := eventstore.SoftDelete(ctx, q, s.workspaceID, cal.ID, userID, row,
		map[string]any{"source": s.source}); err != nil {
		return deleteResult{}, err
	}
	if err := dropSplitSeries(ctx, s, q, cal, userID, row.PublicID); err != nil {
		return deleteResult{}, err
	}
	return deleteResult{deleted: true}, nil
}

// syncHead inserts one event the calendar does not hold, or rewrites the
// synced row holding its UID when the source's version differs.
func syncHead(
	ctx context.Context, s session, q *generated.Queries, cal generated.Calendar, userID uint32, e Event,
) (importOutcome, error) {
	identity := sourceUID(e.SourceUID)
	if !identity.Valid {
		// Without an identity it would be inserted again on every sync.
		return importSkipped, nil
	}
	h, outcome := buildHead(e, userID)
	existing, err := q.FindCalendarEventBySourceUID(ctx,
		generated.FindCalendarEventBySourceUIDParams{CalendarID: cal.ID, SourceUid: identity})
	if errors.Is(err, sql.ErrNoRows) {
		if outcome != importCreated {
			return outcome, nil
		}
		pubID, err := uuid.NewV7()
		if err != nil {
			return importFailed, nil
		}
		if _, err := insertHead(ctx, s, q, cal, userID, h, pubID[:], identity); err != nil {
			return importFailed, err
		}
		return importCreated, nil
	}
	if err != nil {
		return importFailed, err
	}
	if outcome != importCreated {
		return importKept, nil
	}
	visible, err := visibleToImporter(ctx, q, cal, userID, existing)
	if err != nil {
		return importFailed, err
	}
	if !visible {
		return importKept, nil
	}

	row, err := calresolve.LockEvent(ctx, q, cal.ID, existing.ID)
	if errors.Is(err, apierrors.EventNotFound) {
		return importKept, nil
	}
	if err != nil {
		return importFailed, err
	}
	if row.TaskID.Valid {
		return importKept, nil
	}
	fromSource, err := isFromSource(ctx, s, q, row)
	if err != nil {
		return importFailed, err
	}
	if !fromSource {
		return importKept, nil
	}
	changed, overrides, err := compare(ctx, q, row, h)
	if err != nil {
		return importFailed, err
	}
	if !changed {
		return importDuplicate, nil
	}
	if err := updateHead(ctx, s, q, cal, userID, row, h, overrides); err != nil {
		return importFailed, err
	}
	return importUpdated, nil
}

// updateHead rewrites a synced row to h. The columns the source has no value
// for keep what the row holds; overrides are dropped, as the source has none.
func updateHead(
	ctx context.Context, s session, q *generated.Queries, cal generated.Calendar, userID uint32,
	row generated.CalendarEvent, h head, overrides []uint32,
) error {
	if err := q.UpdateCalendarEvent(ctx, generated.UpdateCalendarEventParams{
		Kind:               row.Kind,
		Visibility:         row.Visibility,
		ShowAs:             row.ShowAs,
		Flexibility:        row.Flexibility,
		Title:              h.title,
		AllDay:             h.allDay,
		StartAt:            sql.NullTime{Time: h.start, Valid: true},
		EndAt:              sql.NullTime{Time: h.end, Valid: true},
		Timezone:           h.tz,
		Location:           nullString(h.location),
		Memo:               nullString(h.memo),
		URL:                nullString(h.url),
		OwnerUserID:        h.owner,
		BlockLabel:         row.BlockLabel,
		NotificationOffset: row.NotificationOffset,
		RecurrenceRule:     h.rule,
		RecurrenceEnd:      h.recEnd,
		ID:                 row.ID,
	}); err != nil {
		return err
	}
	if err := q.SetCalendarEventCreator(ctx, generated.SetCalendarEventCreatorParams{
		CreatedByUserID: h.creator,
		ID:              row.ID,
	}); err != nil {
		return err
	}
	if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
		RecurrenceExceptions: h.exceptions,
		ID:                   row.ID,
	}); err != nil {
		return err
	}
	// Attachments are retired first, as deleting a whole event does.
	for _, id := range overrides {
		if err := q.SoftDeleteAttachmentsByEvent(ctx, sql.NullInt32{Int32: int32(id), Valid: true}); err != nil {
			return err
		}
	}
	if err := eventstore.DeleteOverrides(ctx, q, sql.NullInt32{Int32: int32(row.ID), Valid: true}, overrides); err != nil {
		return err
	}
	if err := dropSplitSeries(ctx, s, q, cal, userID, row.PublicID); err != nil {
		return err
	}
	extra := map[string]any{"source": s.source}
	if err := attendees.Replace(ctx, q, s.workspaceID, row.ID, h.attendees, attendees.Log{
		ActorUserID: userID, CalendarID: cal.ID, Subject: row.PublicID, Title: h.title, Extra: extra,
	}); err != nil {
		return err
	}
	return eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: s.workspaceID,
		CalendarID:  cal.ID,
		ActorUserID: userID,
		Type:        eventlog.TypeEventUpdated,
		Title:       h.title,
		Subject:     row.PublicID,
		Extra:       extra,
	})
}

// dropSplitSeries soft-deletes the series a following split cut from the one
// named by publicID, and those cut from them in turn: a cut is a local edit of
// the source's series, and the source's version replaces it as it does an override.
func dropSplitSeries(
	ctx context.Context, s session, q *generated.Queries, cal generated.Calendar, userID uint32, publicID []byte,
) error {
	ids, err := q.ListLiveSplitSeries(ctx, generated.ListLiveSplitSeriesParams{
		WorkspaceID: s.workspaceID,
		CalendarID:  sql.NullInt32{Int32: int32(cal.ID), Valid: true},
		SplitFrom:   json.RawMessage(eventlog.UUID(publicID)),
	})
	if err != nil {
		return err
	}
	for _, id := range ids {
		row, err := calresolve.LockEvent(ctx, q, cal.ID, id)
		if errors.Is(err, apierrors.EventNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		visible, err := visibleToImporter(ctx, q, cal, userID, row)
		if err != nil {
			return err
		}
		if !visible || row.TaskID.Valid {
			continue
		}
		if err := eventstore.SoftDelete(ctx, q, s.workspaceID, cal.ID, userID, row,
			map[string]any{"source": s.source}); err != nil {
			return err
		}
		if err := dropSplitSeries(ctx, s, q, cal, userID, row.PublicID); err != nil {
			return err
		}
	}
	return nil
}

// isFromSource reports whether row's creation entry names the session's source.
func isFromSource(ctx context.Context, s session, q *generated.Queries, row generated.CalendarEvent) (bool, error) {
	return q.IsCalendarEventFromSource(ctx, generated.IsCalendarEventFromSourceParams{
		WorkspaceID: row.WorkspaceID,
		CalendarID:  sql.NullInt32{Int32: int32(row.CalendarID), Valid: true},
		PublicID:    row.PublicID,
		Source:      json.RawMessage(s.source),
	})
}

// compare reads row's attendees and overrides and reports whether h differs
// from it, along with the override ids.
func compare(ctx context.Context, q *generated.Queries, row generated.CalendarEvent, h head) (bool, []uint32, error) {
	ref := sql.NullInt32{Int32: int32(row.ID), Valid: true}
	attendeeIDs, err := q.ListEnabledEventAttendeeIDs(ctx, ref)
	if err != nil {
		return false, nil, err
	}
	overrides, err := q.ListRecurrenceOverrideIDsByParent(ctx, ref)
	if err != nil {
		return false, nil, err
	}
	return diff(row, h, attendeeIDs, len(overrides)), overrides, nil
}

// keptUnconverted counts the UIDs the source still has but that did not
// convert, whose rows are left as they are.
func keptUnconverted(ctx context.Context, q *generated.Queries, cal generated.Calendar, events []Event, present map[string]bool) (int, error) {
	converted := make(map[string]bool, len(events))
	for _, e := range events {
		converted[e.SourceUID] = true
	}
	kept := 0
	for uid := range present {
		identity := sourceUID(uid)
		if converted[uid] || !identity.Valid {
			continue
		}
		_, err := q.FindCalendarEventBySourceUID(ctx,
			generated.FindCalendarEventBySourceUIDParams{CalendarID: cal.ID, SourceUid: identity})
		switch {
		case err == nil:
			kept++
		case !errors.Is(err, sql.ErrNoRows):
			return kept, err
		}
	}
	return kept, nil
}

// diff reports whether the stored row differs from h in anything a sync
// writes. attendeeIDs are the row's live attendees; overrides is how many
// override rows it has, any of which is a difference.
func diff(row generated.CalendarEvent, h head, attendeeIDs []uint32, overrides int) bool {
	if row.Title != h.title || row.AllDay != h.allDay ||
		row.Location.String != h.location || row.Memo.String != h.memo || row.URL.String != h.url ||
		row.Timezone != h.tz || row.OwnerUserID != h.owner || row.CreatedByUserID != h.creator {
		return true
	}
	if !sameInstant(row.StartAt, sql.NullTime{Time: h.start, Valid: true}) ||
		!sameInstant(row.EndAt, sql.NullTime{Time: h.end, Valid: true}) ||
		!sameInstant(row.RecurrenceEnd, h.recEnd) {
		return true
	}
	if !sameRule(row.RecurrenceRule, h.rule) || !sameExceptions(row.RecurrenceExceptions, h.exceptions) {
		return true
	}
	if overrides > 0 {
		return true
	}
	want := make(map[uint32]bool, len(h.attendees))
	for _, p := range h.attendees {
		want[p.UserID] = true
	}
	have := make(map[uint32]bool, len(attendeeIDs))
	for _, id := range attendeeIDs {
		have[id] = true
	}
	return !maps.Equal(want, have)
}

// sameInstant compares at the millisecond precision the columns hold.
func sameInstant(a, b sql.NullTime) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || a.Time.UTC().Truncate(time.Millisecond).Equal(b.Time.UTC().Truncate(time.Millisecond))
}

// sameRule compares decoded rules, so key order in the stored JSON is no
// difference.
func sameRule(a, b *json.RawMessage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ra, errA := recurrence.ParseRule(*a)
	rb, errB := recurrence.ParseRule(*b)
	if errA != nil || errB != nil {
		return false
	}
	return reflect.DeepEqual(ra, rb)
}

// sameExceptions compares the exclusion lists as sets of elements; NULL and an
// empty list are the same.
func sameExceptions(a, b *json.RawMessage) bool {
	set := func(raw *json.RawMessage) map[string]bool {
		out := map[string]bool{}
		for _, el := range recurrence.ParseExceptionList(raw) {
			out[string(el)] = true
		}
		return out
	}
	return maps.Equal(set(a), set(b))
}
