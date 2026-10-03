package events

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// eventEdit is an update request after everything that can be checked without
// the stored row has been: dates parsed, owner and participants resolved.
type eventEdit struct {
	body         *UpdateEventInput
	actorID      uint32
	cal          generated.Calendar
	startAt      time.Time
	endAt        time.Time
	ownerID      uint32 // 0 keeps the current owner
	participants []eventParticipant
}

// editedFields are the columns whose omission keeps what the replaced row
// already has. base is that row: the override standing in for the occurrence
// when there is one, else the series.
type editedFields struct {
	visibility  generated.CalendarEventsVisibility
	showAs      generated.CalendarEventsShowAs
	flexibility generated.CalendarEventsFlexibility
	ownerID     uint32
	timezone    string
}

// mergeFields resolves the omittable fields of an edit against base.
//
// Treating an omission as "default" would quietly republish an event somebody
// had marked private, reset an out-of-office block to busy, or hand the event
// back to whoever is editing it -- and every client that does not know a field
// would do it on every save, including a drag that only means to move it.
func mergeFields(e eventEdit, base generated.CalendarEvent) editedFields {
	f := editedFields{
		visibility:  base.Visibility,
		showAs:      base.ShowAs,
		flexibility: base.Flexibility,
		ownerID:     base.OwnerUserID,
		timezone:    base.Timezone,
	}
	if e.body.Body.Visibility != "" {
		f.visibility = visibilityOrDefault(e.body.Body.Visibility)
	}
	if e.body.Body.ShowAs != "" {
		f.showAs = showAsOrDefault(e.body.Body.ShowAs)
	}
	if e.body.Body.Flexibility != "" {
		f.flexibility = flexibilityOrDefault(e.body.Body.Flexibility)
	}
	if e.ownerID != 0 {
		f.ownerID = e.ownerID
	}
	if e.body.Body.Timezone != "" {
		f.timezone = e.body.Body.Timezone
	}
	if f.timezone == "" {
		f.timezone = "UTC"
	}
	return f
}

// lockEvent is the first step of every write to an event: its own columns, its
// exclusion list, or a row hanging off it (an override, an attachment, a
// comment, a checklist item). It takes the calendar's shared lock, then reads
// the event under its exclusive lock and checks the caller's If-Match against
// that copy, so the revision compared is the one the write replaces and two
// writers holding the same tag cannot both pass.
//
// The order is the point. A row inserted under the event takes a shared lock
// on it for the foreign key, and two writers each holding that while waiting to
// upgrade it deadlock; a delete taking the rows under an event before the event
// deadlocks with a confirm going the other way. Calendar, then event, then
// what hangs off it, avoids both. TestConcurrentFirstEditsOfOccurrencesDoNotDeadlock
// and TestDeletingAnEventWhileAnUploadOnItConfirms are what notice.
func lockEvent(ctx context.Context, q *generated.Queries, calendarID, eventID uint32, ifMatch string) (generated.CalendarEvent, error) {
	current, err := calresolve.LockEvent(ctx, q, calendarID, eventID)
	if err != nil {
		return generated.CalendarEvent{}, err
	}
	if !matchesETag(ifMatch, current) {
		return generated.CalendarEvent{}, apierrors.EventStale
	}
	return current, nil
}

// checkRecurrenceRule rejects a rule the expander would not honour as written,
// or one with no occurrence at all. start is the first occurrence of the series
// the rule would govern, and tz the zone it is anchored in.
func checkRecurrenceRule(data *json.RawMessage, start time.Time, tz string) *apierrors.Spec {
	if data == nil || len(*data) == 0 || string(*data) == "null" {
		return nil
	}
	rule := recurrence.ParseRule(*data)
	if rule == nil || rule.Validate(start.In(recurrence.LoadLocation(tz))) != nil {
		return apierrors.BadRequest
	}
	return nil
}

func prepareRecurrence(ruleData *json.RawMessage, startAt, endAt time.Time, tz string) (*json.RawMessage, sql.NullTime) {
	if ruleData == nil || string(*ruleData) == "null" {
		return nil, sql.NullTime{}
	}
	rule := recurrence.ParseRule(*ruleData)
	if rule == nil {
		return nil, sql.NullTime{}
	}
	// Anchor the recurrence end in the event's timezone so DST does not shift
	// the boundary used by SQL range queries.
	end := recurrence.ComputeEndInZone(rule, startAt, endAt, tz)
	return ruleData, sql.NullTime{Time: end, Valid: true}
}

// editOccurrence writes an override row for one occurrence instead of
// mutating the whole series, inside one transaction.
func editOccurrence(ctx context.Context, deps Deps, e eventEdit, seriesID uint32, occurrenceDate string) (EventResponse, string, error) {
	var (
		child    generated.CalendarEvent
		master   generated.CalendarEvent
		original time.Time
	)
	err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		current, err := lockEvent(ctx, q, e.cal.ID, seriesID, e.body.IfMatch)
		if err != nil {
			return err
		}
		if current.RecurrenceRule == nil {
			return apierrors.EventNotFound
		}
		rule := recurrence.ParseRule(*current.RecurrenceRule)
		if rule == nil {
			return apierrors.EventNotFound
		}
		occ, err := occurrenceForDate(rule, current, occurrenceDate)
		if err != nil {
			return apierrors.EventNotFound
		}
		original = occ.StartAt
		// The body's rule does not apply to one occurrence, but a rule the
		// series could not carry is still a malformed request.
		if spec := checkRecurrenceRule(e.body.Body.RecurrenceRule, current.StartAt.Time, current.Timezone); spec != nil {
			return spec
		}

		parentRef := sql.NullInt32{Int32: int32(current.ID), Valid: true}
		originalRef := sql.NullTime{Time: original, Valid: true}
		base := current
		existing, err := q.GetRecurrenceOverride(ctx, generated.GetRecurrenceOverrideParams{
			RecurrenceParentID:      parentRef,
			RecurrenceOriginalStart: originalRef,
		})
		switch {
		case err == nil:
			base = existing
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		f := mergeFields(e, base)

		// The occurrence lives in its own row, so without this the series
		// would report the same revision as before the edit and a second
		// writer holding the older copy would be let through.
		if err := q.TouchCalendarEvent(ctx, current.ID); err != nil {
			return err
		}
		overridePubID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if _, err := q.UpsertRecurrenceOverride(ctx, generated.UpsertRecurrenceOverrideParams{
			PublicID:                overridePubID[:],
			WorkspaceID:             deps.WorkspaceID,
			CalendarID:              e.cal.ID,
			Kind:                    current.Kind,
			Visibility:              f.visibility,
			ShowAs:                  f.showAs,
			Flexibility:             f.flexibility,
			Title:                   e.body.Body.Title,
			AllDay:                  e.body.Body.AllDay,
			StartAt:                 sql.NullTime{Time: e.startAt, Valid: true},
			EndAt:                   sql.NullTime{Time: e.endAt, Valid: true},
			Timezone:                f.timezone,
			Location:                nullString(e.body.Body.Location),
			Memo:                    nullString(e.body.Body.Memo),
			URL:                     nullString(e.body.Body.URL),
			OwnerUserID:             f.ownerID,
			CreatedByUserID:         e.actorID,
			NotificationOffset:      ptrIntToNullInt32(e.body.Body.NotificationOffset),
			RecurrenceParentID:      parentRef,
			RecurrenceOriginalStart: originalRef,
		}); err != nil {
			return err
		}
		child, err = q.GetRecurrenceOverride(ctx, generated.GetRecurrenceOverrideParams{
			RecurrenceParentID:      parentRef,
			RecurrenceOriginalStart: originalRef,
		})
		if err != nil {
			return err
		}
		// Whether this request created the override or found one that was
		// already there.
		//
		// The upsert does not rewrite public_id on conflict -- it is
		// deliberately absent from that statement's update list -- so a row
		// carrying the id generated for this request is one this request
		// inserted. That row was written a statement ago and has no
		// attendees, and asking to remove the ones it does not have is not
		// free: it holds a gap lock to the end of the attendee index, which
		// the participant insert behind it then has to write into. Two first
		// edits on different series do that at the same moment and deadlock,
		// and whoever dragged an occurrence sees a 500.
		//
		// Edits of one series cannot reach this: they queue on the parent row
		// first. That is why it needs its own test, and why that test puts
		// every series on its own calendar.
		if bytes.Equal(child.PublicID, overridePubID[:]) {
			err = addOverrideParticipants(ctx, q, deps.WorkspaceID, current.ID, child.ID, e.participants)
		} else {
			err = replaceEventParticipants(ctx, q, deps.WorkspaceID, child.ID, e.participants)
		}
		if err != nil {
			return err
		}
		if err := reconcileOverrides(ctx, q, current, current); err != nil {
			return err
		}
		if master, err = q.GetCalendarEventByID(ctx, current.ID); err != nil {
			return err
		}
		return eventlog.Append(ctx, q, eventlog.Entry{
			WorkspaceID: deps.WorkspaceID,
			CalendarID:  e.cal.ID,
			ActorUserID: e.actorID,
			Type:        eventlog.TypeEventUpdated,
			Summary:     e.body.Body.Title,
			Subject:     current.PublicID,
			Extra: map[string]any{
				"scope":         "occurrence",
				"originalStart": original.Format(time.RFC3339),
			},
		})
	})
	if err != nil {
		return EventResponse{}, "", err
	}
	resp := mapOverrideInstance(master, child, e.cal.PublicID, original)
	if err := setAttendees(ctx, deps, &resp, child.ID); err != nil {
		return EventResponse{}, "", err
	}
	decorate(ctx, deps, &resp, child, e.cal.ID)
	// The tag names the series row, which the edit touched.
	return resp, eventETag(master), nil
}

// addOverrideParticipants adds the participants of an override created by
// this transaction, each with the answer and edit grant they had on the
// series. Until it was edited the occurrence showed the series' attendees;
// editing its title must not reset an answer already given or take back a
// grant.
func addOverrideParticipants(ctx context.Context, q *generated.Queries, workspaceID, seriesID, overrideID uint32, participants []eventParticipant) error {
	rows, err := q.ListEventAttendees(ctx, sql.NullInt32{Int32: int32(seriesID), Valid: true})
	if err != nil {
		return err
	}
	type state struct {
		rsvp    generated.CalendarEventAttendeesRsvp
		canEdit bool
	}
	onSeries := make(map[uint32]state, len(rows))
	for _, a := range rows {
		onSeries[a.UserID] = state{rsvp: a.Rsvp, canEdit: a.CanEdit}
	}
	for _, p := range participants {
		st, ok := onSeries[p.userID]
		if !ok {
			st = state{rsvp: generated.CalendarEventAttendeesRsvpPending}
		}
		pubID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := q.AddOverrideAttendee(ctx, generated.AddOverrideAttendeeParams{
			PublicID:    pubID[:],
			WorkspaceID: workspaceID,
			EventID:     sql.NullInt32{Int32: int32(overrideID), Valid: true},
			UserID:      p.userID,
			Rsvp:        st.rsvp,
			CanEdit:     st.canEdit,
		}); err != nil {
			return err
		}
	}
	return nil
}

// editSeries rewrites the series row itself, inside one transaction. A
// non-empty occurrenceDate says the dates in the request are those of that
// occurrence, edited with "apply to all".
func editSeries(ctx context.Context, deps Deps, e eventEdit, seriesID uint32, occurrenceDate string) (EventResponse, string, error) {
	var updated generated.CalendarEvent
	err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		current, err := lockEvent(ctx, q, e.cal.ID, seriesID, e.body.IfMatch)
		if err != nil {
			return err
		}
		f := mergeFields(e, current)
		startAt, endAt := e.startAt, e.endAt
		if occurrenceDate != "" && current.RecurrenceRule != nil {
			rule := recurrence.ParseRule(*current.RecurrenceRule)
			if rule == nil || !current.StartAt.Valid {
				return apierrors.EventNotFound
			}
			occ, err := occurrenceForDate(rule, current, occurrenceDate)
			if err != nil {
				return apierrors.EventNotFound
			}
			startAt, endAt = reanchor(current, occ, startAt, endAt, f.timezone)
		}
		if spec := checkRecurrenceRule(e.body.Body.RecurrenceRule, startAt, f.timezone); spec != nil {
			return spec
		}
		ruleData, recEnd := prepareRecurrence(e.body.Body.RecurrenceRule, startAt, endAt, f.timezone)

		if err := q.UpdateCalendarEvent(ctx, generated.UpdateCalendarEventParams{
			Kind:               current.Kind,
			Visibility:         f.visibility,
			ShowAs:             f.showAs,
			Flexibility:        f.flexibility,
			Title:              e.body.Body.Title,
			AllDay:             e.body.Body.AllDay,
			StartAt:            sql.NullTime{Time: startAt, Valid: true},
			EndAt:              sql.NullTime{Time: endAt, Valid: true},
			Timezone:           f.timezone,
			Location:           nullString(e.body.Body.Location),
			Memo:               nullString(e.body.Body.Memo),
			URL:                nullString(e.body.Body.URL),
			OwnerUserID:        f.ownerID,
			BlockLabel:         current.BlockLabel,
			NotificationOffset: ptrIntToNullInt32(e.body.Body.NotificationOffset),
			RecurrenceRule:     ruleData,
			RecurrenceEnd:      recEnd,
			ID:                 current.ID,
		}); err != nil {
			return err
		}
		if err := replaceEventParticipants(ctx, q, deps.WorkspaceID, current.ID, e.participants); err != nil {
			return err
		}
		after, err := q.GetCalendarEventByID(ctx, current.ID)
		if err != nil {
			return err
		}
		if err := reconcileOverrides(ctx, q, current, after); err != nil {
			return err
		}
		if updated, err = q.GetCalendarEventByID(ctx, current.ID); err != nil {
			return err
		}
		return eventlog.Append(ctx, q, eventlog.Entry{
			WorkspaceID: deps.WorkspaceID,
			CalendarID:  e.cal.ID,
			ActorUserID: e.actorID,
			Type:        eventlog.TypeEventUpdated,
			Summary:     e.body.Body.Title,
			Subject:     current.PublicID,
		})
	})
	if err != nil {
		return EventResponse{}, "", err
	}
	resp := mapEvent(updated, e.cal.PublicID)
	if err := setAttendees(ctx, deps, &resp, updated.ID); err != nil {
		return EventResponse{}, "", err
	}
	decorate(ctx, deps, &resp, updated, e.cal.ID)
	return resp, eventETag(updated), nil
}

// reanchor turns the dates a caller gave one occurrence into the series start
// that produces them. The move is applied in wall-clock terms in the event's
// zone -- calendar days plus the change in time of day -- because occurrences
// step in calendar units there: an absolute delta measured across a DST
// transition lands the whole series an hour off.
func reanchor(master generated.CalendarEvent, occ recurrence.Occurrence, newStart, newEnd time.Time, tz string) (time.Time, time.Time) {
	start := newWallShift(occ.StartAt, newStart, master.Timezone, tz).apply(master.StartAt.Time)
	loc := recurrence.LoadLocation(tz)
	localStart, localEnd := newStart.In(loc), newEnd.In(loc)
	// A midnight-to-midnight span is a number of days, not hours, the same
	// reading the expander gives it.
	if timeOfDay(localStart) == 0 && timeOfDay(localEnd) == 0 && localEnd.After(localStart) {
		days := newWallShift(newStart, newEnd, tz, tz).days
		return start, start.In(loc).AddDate(0, 0, days).UTC()
	}
	return start, start.Add(newEnd.Sub(newStart))
}

// reconcileOverrides brings a series' overrides and cancellations in line with
// the series row after any write to it. before is the row as locked at the
// start of the transaction, after as written.
//
//   - Recurrence removed: the series is a single event, and no override or
//     cancellation can name one of its occurrences again.
//   - Anchor moved: overrides and cancellations move with it in wall-clock
//     terms, so each keeps naming the occurrence it stood for.
//   - Rule, anchor or zone changed: anything the new grid does not produce is
//     dropped rather than left to surface beside the new schedule.
//   - Always: recurrence_end covers every override, or a moved occurrence
//     stops being selected for the window it now falls in and vanishes from
//     every view, export and share.
func reconcileOverrides(ctx context.Context, q *generated.Queries, before, after generated.CalendarEvent) error {
	parentRef := sql.NullInt32{Int32: int32(after.ID), Valid: true}
	if before.RecurrenceRule == nil && after.RecurrenceRule == nil {
		return nil
	}
	if after.RecurrenceRule == nil {
		ids, err := q.ListRecurrenceOverrideIDsByParent(ctx, parentRef)
		if err != nil {
			return err
		}
		if err := deleteOverrides(ctx, q, parentRef, ids); err != nil {
			return err
		}
		if after.RecurrenceExceptions == nil {
			return nil
		}
		return q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{ID: after.ID})
	}
	rule := recurrence.ParseRule(*after.RecurrenceRule)
	if rule == nil || !after.StartAt.Valid || !after.EndAt.Valid {
		return nil
	}

	overrides, err := q.ListRecurrenceOverridesByParent(ctx, parentRef)
	if err != nil {
		return err
	}
	exceptions := recurrence.ParseExceptions(after.RecurrenceExceptions)
	exceptionsChanged := false

	gridChanged := before.RecurrenceRule == nil ||
		!bytes.Equal(*before.RecurrenceRule, *after.RecurrenceRule) ||
		!before.StartAt.Time.Equal(after.StartAt.Time) ||
		before.Timezone != after.Timezone
	if before.RecurrenceRule != nil && before.StartAt.Valid {
		shift := newWallShift(before.StartAt.Time, after.StartAt.Time, before.Timezone, after.Timezone)
		if !shift.identity() {
			if err := shiftOverrides(ctx, q, overrides, shift); err != nil {
				return err
			}
			for i, ex := range exceptions {
				exceptions[i] = shift.apply(ex)
			}
			exceptionsChanged = len(exceptions) > 0
		}
	}

	if gridChanged {
		occurs := func(t time.Time) bool {
			return recurrence.OccursAt(rule, after.StartAt.Time, after.EndAt.Time, after.Timezone, t)
		}
		var orphans []uint32
		kept := overrides[:0]
		for _, o := range overrides {
			if o.RecurrenceOriginalStart.Valid && occurs(o.RecurrenceOriginalStart.Time) {
				kept = append(kept, o)
				continue
			}
			orphans = append(orphans, o.ID)
		}
		overrides = kept
		if err := deleteOverrides(ctx, q, parentRef, orphans); err != nil {
			return err
		}
		live := slices.DeleteFunc(slices.Clone(exceptions), func(t time.Time) bool { return !occurs(t) })
		exceptionsChanged = exceptionsChanged || len(live) != len(exceptions)
		exceptions = live
	}

	if exceptionsChanged {
		column, err := exceptions.MarshalColumn()
		if err != nil {
			return err
		}
		if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
			RecurrenceExceptions: column,
			ID:                   after.ID,
		}); err != nil {
			return err
		}
	}

	var lastEnd time.Time
	for _, o := range overrides {
		if o.EndAt.Valid && o.EndAt.Time.After(lastEnd) {
			lastEnd = o.EndAt.Time
		}
	}
	if lastEnd.IsZero() {
		return nil
	}
	bound := sql.NullTime{Time: lastEnd, Valid: true}
	return q.ExtendRecurrenceEnd(ctx, generated.ExtendRecurrenceEndParams{
		RecurrenceEnd: bound,
		ID:            after.ID,
		Boundary:      bound,
	})
}

// deleteOverrides hard-deletes override rows together with their attendee
// rows. The attendee foreign key would otherwise null the event out of each
// row, and the unique key over IFNULL(event_id, 0) refuses a second such row
// for the same person.
func deleteOverrides(ctx context.Context, q *generated.Queries, parentRef sql.NullInt32, ids []uint32) error {
	if len(ids) == 0 {
		return nil
	}
	refs := make([]sql.NullInt32, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, sql.NullInt32{Int32: int32(id), Valid: true})
	}
	if err := q.DeleteAttendeesOfEvents(ctx, refs); err != nil {
		return err
	}
	return q.DeleteRecurrenceOverrides(ctx, generated.DeleteRecurrenceOverridesParams{
		ParentID: parentRef,
		Ids:      ids,
	})
}

// shiftOverrides moves every override row of a series by the same wall-clock
// shift as the series itself, so each keeps naming the occurrence it replaces
// and keeps the time of day the user gave it. rows are updated in place.
//
// Rows are moved in the order that never lands one on an instant another still
// holds: a shift of exactly one period would otherwise collide with the unique
// key on (parent, original start) halfway through.
func shiftOverrides(ctx context.Context, q *generated.Queries, rows []generated.CalendarEvent, shift wallShift) error {
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		return rows[a].RecurrenceOriginalStart.Time.Compare(rows[b].RecurrenceOriginalStart.Time)
	})
	if len(rows) > 0 {
		probe := rows[order[0]].RecurrenceOriginalStart.Time
		if shift.apply(probe).After(probe) {
			slices.Reverse(order)
		}
	}
	for _, i := range order {
		row := &rows[i]
		if row.RecurrenceOriginalStart.Valid {
			row.RecurrenceOriginalStart.Time = shift.apply(row.RecurrenceOriginalStart.Time)
		}
		if row.StartAt.Valid {
			row.StartAt.Time = shift.apply(row.StartAt.Time)
		}
		if row.EndAt.Valid {
			row.EndAt.Time = shift.apply(row.EndAt.Time)
		}
		if err := q.RetimeRecurrenceOverride(ctx, generated.RetimeRecurrenceOverrideParams{
			RecurrenceOriginalStart: row.RecurrenceOriginalStart,
			StartAt:                 row.StartAt,
			EndAt:                   row.EndAt,
			ID:                      row.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// wallShift describes how a series-wide edit moved the grid: a number of
// calendar days, a change in the time of day, and the zones the two are read
// and written in.
//
// The unit matters. Occurrences step in calendar units in the event's own
// timezone, so a series whose anchor crosses a DST boundary moves by a
// different number of hours than of days. Shifting a cancellation or an
// override by the absolute difference between the two anchors therefore lands
// it an hour off the grid it is supposed to name: the cancelled occurrence
// comes back and the edited one is orphaned, showing up twice.
type wallShift struct {
	from *time.Location
	to   *time.Location
	days int
	// ofDay is the change in time of day, and may be negative or exceed a day.
	ofDay time.Duration
}

// newWallShift measures the move from oldStart in oldTZ to newStart in newTZ.
func newWallShift(oldStart, newStart time.Time, oldTZ, newTZ string) wallShift {
	from := recurrence.LoadLocation(oldTZ)
	to := recurrence.LoadLocation(newTZ)
	oldLocal := oldStart.In(from)
	newLocal := newStart.In(to)
	oldDay := time.Date(oldLocal.Year(), oldLocal.Month(), oldLocal.Day(), 0, 0, 0, 0, time.UTC)
	newDay := time.Date(newLocal.Year(), newLocal.Month(), newLocal.Day(), 0, 0, 0, 0, time.UTC)
	return wallShift{
		from:  from,
		to:    to,
		days:  int(newDay.Sub(oldDay) / (24 * time.Hour)),
		ofDay: timeOfDay(newLocal) - timeOfDay(oldLocal),
	}
}

// identity reports whether the shift leaves every instant where it is.
func (w wallShift) identity() bool {
	return w.days == 0 && w.ofDay == 0 && w.from.String() == w.to.String()
}

// apply moves one instant by the shift, staying in wall-clock terms
// throughout so a DST boundary crossed on the way does not add an hour.
func (w wallShift) apply(t time.Time) time.Time {
	local := t.In(w.from)
	const day = 24 * time.Hour
	wall := timeOfDay(local) + w.ofDay
	extraDays := int(wall / day)
	wall -= time.Duration(extraDays) * day
	if wall < 0 {
		wall += day
		extraDays--
	}
	target := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, w.days+extraDays)
	return time.Date(
		target.Year(), target.Month(), target.Day(),
		int(wall/time.Hour), int(wall/time.Minute)%60, int(wall/time.Second)%60,
		int(wall%time.Second), w.to,
	).UTC()
}

func timeOfDay(t time.Time) time.Duration {
	return time.Duration(t.Hour())*time.Hour +
		time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second +
		time.Duration(t.Nanosecond())
}
