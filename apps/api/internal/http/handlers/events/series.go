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
	"github.com/libraz/nodate-time/apps/api/internal/attendees"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/eventstore"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
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
	participants []attendees.Person
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

// Scope values a write to an event may carry.
const (
	scopeThis      = "this"
	scopeAll       = "all"
	scopeFollowing = "following"
)

// writeScope is what an event id and a scope address on a series, as read
// under the series' row lock.
type writeScope struct {
	// occurrence is the occurrence a composite id named; nil for a plain id.
	occurrence *recurrence.Occurrence
	// override stands in for that occurrence once it has been edited.
	override *generated.CalendarEvent
	// confined says the write reaches that occurrence alone.
	confined bool
}

// shown returns the dates a reader was given for the occurrence: its override's
// once edited, else the ones the rule produces.
func (s writeScope) shown() (time.Time, time.Time) {
	if s.override != nil && s.override.StartAt.Valid && s.override.EndAt.Valid {
		return s.override.StartAt.Time, s.override.EndAt.Time
	}
	return s.occurrence.StartAt, s.occurrence.EndAt
}

// resolveScope is the one place a write's scope is mapped onto the rows it may
// touch, from the series row its transaction has locked.
//
// A composite id names an occurrence the way GET does: through
// eventexpand.ResolveOccurrence, so an id GET answers 404 for -- a series no
// longer recurring, a date the rule does not produce, a cancelled occurrence --
// is 404 here too, under either scope. scope=this is confined to that
// occurrence and never falls through to the series row; on a plain id it names
// nothing and is refused. Anything else is a write to the whole row.
func resolveScope(ctx context.Context, q *generated.Queries, series generated.CalendarEvent, occurrenceDate, scope string) (writeScope, error) {
	if occurrenceDate == "" {
		if scope == scopeThis {
			return writeScope{}, apierrors.BadRequest
		}
		return writeScope{}, nil
	}
	if series.RecurrenceRule == nil {
		return writeScope{}, apierrors.EventNotFound
	}
	rule, err := recurrence.ParseRule(*series.RecurrenceRule)
	if err != nil || rule == nil {
		return writeScope{}, apierrors.EventNotFound
	}
	occ, err := occurrenceForDate(rule, series, occurrenceDate)
	if err != nil {
		return writeScope{}, apierrors.EventNotFound
	}
	inst, err := eventexpand.ResolveOccurrence(ctx, q, series, occ)
	if errors.Is(err, eventexpand.ErrCancelled) {
		return writeScope{}, apierrors.EventNotFound
	}
	if err != nil {
		return writeScope{}, err
	}
	target := writeScope{occurrence: &occ, confined: scope == scopeThis}
	if inst.IsOverride {
		target.override = &inst.Event
	}
	return target, nil
}

// editedRows is what a committed edit is rendered from.
type editedRows struct {
	series generated.CalendarEvent
	// override is the occurrence's row after an edit confined to it.
	override *generated.CalendarEvent
	original time.Time
}

// render builds the response for a committed edit. It reads outside the edit's
// transaction, so it must only be called after that transaction committed.
func (r editedRows) render(ctx context.Context, deps Deps, cal generated.Calendar) (EventResponse, error) {
	if r.override != nil {
		resp := mapOverrideInstance(r.series, *r.override, cal.PublicID, r.original)
		if err := setOccurrenceAttendees(ctx, deps, &resp, r.series.ID, r.override.ID); err != nil {
			return EventResponse{}, err
		}
		decorate(ctx, deps, &resp, *r.override, cal.ID)
		return resp, nil
	}
	resp := mapEvent(r.series, cal.PublicID)
	if err := setAttendees(ctx, deps, &resp, r.series.ID); err != nil {
		return EventResponse{}, err
	}
	decorate(ctx, deps, &resp, r.series, cal.ID)
	return resp, nil
}

// checkRecurrenceRule rejects a rule the expander would not honour as written,
// or one with no occurrence at all. start is the first occurrence of the series
// the rule would govern, and tz the zone it is anchored in.
func checkRecurrenceRule(data *json.RawMessage, start time.Time, tz string) *apierrors.Spec {
	if data == nil || len(*data) == 0 || string(*data) == "null" {
		return nil
	}
	rule, err := recurrence.ParseRule(*data)
	if err != nil || rule == nil || rule.Validate(start.In(recurrence.LoadLocation(tz))) != nil {
		return apierrors.BadRequest
	}
	return nil
}

func prepareRecurrence(ruleData *json.RawMessage, startAt, endAt time.Time, tz string) (*json.RawMessage, sql.NullTime) {
	if ruleData == nil || string(*ruleData) == "null" {
		return nil, sql.NullTime{}
	}
	rule, err := recurrence.ParseRule(*ruleData)
	if err != nil || rule == nil {
		return nil, sql.NullTime{}
	}
	// Anchor the recurrence end in the event's timezone so DST does not shift
	// the boundary used by SQL range queries.
	end := recurrence.ComputeEndInZone(rule, startAt, endAt, tz)
	return ruleData, sql.NullTime{Time: end, Valid: true}
}

// editOccurrence writes an override row for one occurrence instead of
// mutating the whole series. w must come from lockEventAccess in q's
// transaction, with a scope confined to that occurrence.
func editOccurrence(ctx context.Context, q *generated.Queries, deps Deps, e eventEdit, w eventAccess) (editedRows, error) {
	current := w.evt
	original := w.scope.occurrence.StartAt
	// The body's rule does not apply to one occurrence, but a rule the
	// series could not carry is still a malformed request.
	if spec := checkRecurrenceRule(e.body.Body.RecurrenceRule, current.StartAt.Time, recurrence.ExpansionZone(current.AllDay, current.Timezone)); spec != nil {
		return editedRows{}, spec
	}

	parentRef := sql.NullInt32{Int32: int32(current.ID), Valid: true}
	originalRef := sql.NullTime{Time: original, Valid: true}
	f := mergeFields(e, w.row)

	// The occurrence lives in its own row, so without this the series
	// would report the same revision as before the edit and a second
	// writer holding the older copy would be let through.
	if err := q.TouchCalendarEvent(ctx, current.ID); err != nil {
		return editedRows{}, err
	}
	overridePubID, err := uuid.NewV7()
	if err != nil {
		return editedRows{}, err
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
		return editedRows{}, err
	}
	child, err := q.GetRecurrenceOverride(ctx, generated.GetRecurrenceOverrideParams{
		RecurrenceParentID:      parentRef,
		RecurrenceOriginalStart: originalRef,
	})
	if err != nil {
		return editedRows{}, err
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
	pl := attendees.Log{
		ActorUserID: e.actorID, CalendarID: e.cal.ID, Subject: current.PublicID,
		Title: e.body.Body.Title, Extra: participantLogExtra(w),
	}
	// An override with no attendee row inherits the series', so replacing
	// its list must write the rows that say who is left out.
	rowCount, err := q.CountEventAttendeeRows(ctx, sql.NullInt32{Int32: int32(child.ID), Valid: true})
	if err != nil {
		return editedRows{}, err
	}
	if bytes.Equal(child.PublicID, overridePubID[:]) || rowCount == 0 {
		err = addOverrideParticipants(ctx, q, deps.WorkspaceID, current.ID, child.ID, e.participants, pl)
	} else {
		err = attendees.Replace(ctx, q, deps.WorkspaceID, child.ID, e.participants, pl)
	}
	if err != nil {
		return editedRows{}, err
	}
	if err := reconcileOverrides(ctx, q, current, current); err != nil {
		return editedRows{}, err
	}
	master, err := q.GetCalendarEventByID(ctx, current.ID)
	if err != nil {
		return editedRows{}, err
	}
	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  e.cal.ID,
		ActorUserID: e.actorID,
		Type:        eventlog.TypeEventUpdated,
		Title:       e.body.Body.Title,
		Subject:     current.PublicID,
		Extra: map[string]any{
			"scope":         "occurrence",
			"originalStart": original.Format(time.RFC3339),
		},
	}); err != nil {
		return editedRows{}, err
	}
	return editedRows{series: master, override: &child, original: original}, nil
}

// addOverrideParticipants adds the participants of an override that has no
// attendee row yet, each with the answer and edit grant they had on the
// series. Until it was edited the occurrence showed the series' attendees;
// editing its title must not reset an answer already given or take back a
// grant. Everyone on the series who is not kept gets a disabled row: an
// override with any row replaces the series' attendees, so without those rows
// removing everyone would read as inheriting everyone.
func addOverrideParticipants(ctx context.Context, q *generated.Queries, workspaceID, seriesID, overrideID uint32, participants []attendees.Person, log attendees.Log) error {
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
		st, ok := onSeries[p.UserID]
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
			UserID:      p.UserID,
			Rsvp:        st.rsvp,
			CanEdit:     st.canEdit,
		}); err != nil {
			return err
		}
	}
	kept := make(map[uint32]struct{}, len(participants))
	for _, p := range participants {
		kept[p.UserID] = struct{}{}
	}
	live, err := q.ListEnabledEventAttendeeIDs(ctx, sql.NullInt32{Int32: int32(seriesID), Valid: true})
	if err != nil {
		return err
	}
	for _, userID := range live {
		if _, ok := kept[userID]; ok {
			continue
		}
		pubID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := q.AddDisabledOverrideAttendee(ctx, generated.AddDisabledOverrideAttendeeParams{
			PublicID:    pubID[:],
			WorkspaceID: workspaceID,
			EventID:     sql.NullInt32{Int32: int32(overrideID), Valid: true},
			UserID:      userID,
		}); err != nil {
			return err
		}
	}
	return attendees.LogDiff(ctx, q, workspaceID, log, rows, participants)
}

// editSeries rewrites the series row itself. w must come from
// lockEventAccess in q's transaction; when its scope names an occurrence, the
// dates in the request are those of that occurrence, edited with "apply to
// all".
func editSeries(ctx context.Context, q *generated.Queries, deps Deps, e eventEdit, w eventAccess) (editedRows, error) {
	current := w.evt
	if current.TaskID.Valid {
		return editProjected(ctx, q, deps, e, w)
	}
	f := mergeFields(e, current)
	startAt, endAt := e.startAt, e.endAt
	if w.scope.occurrence != nil {
		occ := *w.scope.occurrence
		shownStart, shownEnd := w.scope.shown()
		startAt, endAt = ontoSlot(occ, shownStart, shownEnd, startAt, endAt, f.timezone)
		startAt, endAt = reanchor(current, occ, startAt, endAt, f.timezone)
	}
	rule := e.body.Body.RecurrenceRule
	if current.AllDay != e.body.Body.AllDay {
		rule = retargetUntil(rule, current, e.body.Body.AllDay, startAt, f.timezone)
	}
	zone := recurrence.ExpansionZone(e.body.Body.AllDay, f.timezone)
	if spec := checkRecurrenceRule(rule, startAt, zone); spec != nil {
		return editedRows{}, spec
	}
	ruleData, recEnd := prepareRecurrence(rule, startAt, endAt, zone)

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
		return editedRows{}, err
	}
	pl := attendees.Log{
		ActorUserID: e.actorID, CalendarID: e.cal.ID, Subject: current.PublicID,
		Title: e.body.Body.Title, Extra: participantLogExtra(w),
	}
	if err := attendees.Replace(ctx, q, deps.WorkspaceID, current.ID, e.participants, pl); err != nil {
		return editedRows{}, err
	}
	after, err := q.GetCalendarEventByID(ctx, current.ID)
	if err != nil {
		return editedRows{}, err
	}
	if err := reconcileOverrides(ctx, q, current, after); err != nil {
		return editedRows{}, err
	}
	updated, err := q.GetCalendarEventByID(ctx, current.ID)
	if err != nil {
		return editedRows{}, err
	}
	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  e.cal.ID,
		ActorUserID: e.actorID,
		Type:        eventlog.TypeEventUpdated,
		Title:       e.body.Body.Title,
		Subject:     current.PublicID,
	}); err != nil {
		return editedRows{}, err
	}
	return editedRows{series: updated}, nil
}

// editProjected applies an edit to an event a task projects (PROTOCOL.md
// obligation 3). The title and dates mirror the task, and the all-day flag,
// zone and rule decide what those dates mean, so a change to any of them is
// refused; the columns with no task-side counterpart are written as usual.
func editProjected(ctx context.Context, q *generated.Queries, deps Deps, e eventEdit, w eventAccess) (editedRows, error) {
	current := w.evt
	f := mergeFields(e, current)
	rule := e.body.Body.RecurrenceRule
	if e.body.Body.Title != current.Title ||
		e.body.Body.AllDay != current.AllDay ||
		f.timezone != current.Timezone ||
		!current.StartAt.Valid || !e.startAt.Equal(current.StartAt.Time) ||
		!current.EndAt.Valid || !e.endAt.Equal(current.EndAt.Time) ||
		(rule != nil && string(*rule) != "null") {
		return editedRows{}, apierrors.EventTaskOwned
	}
	if err := q.UpdateProjectedEventDetails(ctx, generated.UpdateProjectedEventDetailsParams{
		Visibility:         f.visibility,
		ShowAs:             f.showAs,
		Flexibility:        f.flexibility,
		Location:           nullString(e.body.Body.Location),
		Memo:               nullString(e.body.Body.Memo),
		URL:                nullString(e.body.Body.URL),
		OwnerUserID:        f.ownerID,
		NotificationOffset: ptrIntToNullInt32(e.body.Body.NotificationOffset),
		ID:                 current.ID,
	}); err != nil {
		return editedRows{}, err
	}
	pl := attendees.Log{
		ActorUserID: e.actorID, CalendarID: e.cal.ID, Subject: current.PublicID,
		Title: current.Title, Extra: participantLogExtra(w),
	}
	if err := attendees.Replace(ctx, q, deps.WorkspaceID, current.ID, e.participants, pl); err != nil {
		return editedRows{}, err
	}
	updated, err := q.GetCalendarEventByID(ctx, current.ID)
	if err != nil {
		return editedRows{}, err
	}
	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  e.cal.ID,
		ActorUserID: e.actorID,
		Type:        eventlog.TypeEventUpdated,
		Title:       current.Title,
		Subject:     current.PublicID,
	}); err != nil {
		return editedRows{}, err
	}
	return editedRows{series: updated}, nil
}

// ontoSlot carries the change a caller made to the dates they were shown for an
// occurrence over onto the slot the rule gives it. An edited occurrence is shown
// at its override's dates, and reading those as the occurrence's own would turn
// the override's move into a move of the whole series. The start moves by the
// same wall-clock shift in tz, the end keeps the change in length.
func ontoSlot(occ recurrence.Occurrence, shownStart, shownEnd, newStart, newEnd time.Time, tz string) (time.Time, time.Time) {
	if shownStart.Equal(occ.StartAt) && shownEnd.Equal(occ.EndAt) {
		return newStart, newEnd
	}
	start := newWallShift(shownStart, newStart, tz, tz).apply(occ.StartAt)
	end := start.Add(occ.EndAt.Sub(occ.StartAt) + newEnd.Sub(newStart) - shownEnd.Sub(shownStart))
	return start, end
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
		if err := eventstore.DeleteOverrides(ctx, q, parentRef, ids); err != nil {
			return err
		}
		if after.RecurrenceExceptions == nil {
			return nil
		}
		return q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{ID: after.ID})
	}
	rule, err := recurrence.ParseRule(*after.RecurrenceRule)
	if err != nil || rule == nil || !after.StartAt.Valid || !after.EndAt.Valid {
		return nil
	}

	overrides, err := q.ListRecurrenceOverridesByParent(ctx, parentRef)
	if err != nil {
		return err
	}
	exceptions := recurrence.ParseExceptionList(after.RecurrenceExceptions)
	oldZone := recurrence.ExpansionZone(before.AllDay, before.Timezone)
	newZone := recurrence.ExpansionZone(after.AllDay, after.Timezone)
	toggled := before.AllDay != after.AllDay

	gridChanged := before.RecurrenceRule == nil ||
		!bytes.Equal(*before.RecurrenceRule, *after.RecurrenceRule) ||
		!before.StartAt.Time.Equal(after.StartAt.Time) ||
		oldZone != newZone || toggled
	// move maps an occurrence's instant on the grid the series had to the
	// same occurrence on the grid it has now; nil leaves every instant alone.
	var move func(time.Time) time.Time
	if toggled {
		move = allDayRetarget(before, after)
	} else if before.RecurrenceRule != nil && before.StartAt.Valid {
		shift := newWallShift(before.StartAt.Time, after.StartAt.Time, oldZone, newZone)
		if !shift.identity() {
			move = shift.apply
		}
	}
	if move != nil {
		if err := moveOverrides(ctx, q, overrides, move, !toggled); err != nil {
			return err
		}
		if toggled {
			if err := moveReminderKeys(ctx, q, after.ID, move); err != nil {
				return err
			}
		}
	}

	var occurs func(time.Time) bool
	if gridChanged {
		occurs = func(t time.Time) bool {
			return recurrence.OccursAt(rule, after.StartAt.Time, after.EndAt.Time, newZone, t)
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
		if err := eventstore.DeleteOverrides(ctx, q, parentRef, orphans); err != nil {
			return err
		}
	}

	if move != nil || occurs != nil {
		rewritten := remapExceptions(exceptions, before, after, move, occurs)
		stored, err := exceptions.MarshalColumn()
		if err != nil {
			return err
		}
		column, err := rewritten.MarshalColumn()
		if err != nil {
			return err
		}
		if !equalColumns(stored, column) {
			if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
				RecurrenceExceptions: column,
				ID:                   after.ID,
			}); err != nil {
				return err
			}
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

// moveOverrides re-keys every override row of a series with move, so each
// keeps naming the occurrence it replaces. A wall-clock shift also carries the
// override's own dates, so it keeps the time of day the user gave it; a change
// of all-day shape leaves them. rows are updated in place.
//
// Rows are moved in the order that never lands one on an instant another still
// holds: a shift of exactly one period would otherwise collide with the unique
// key on (parent, original start) halfway through.
func moveOverrides(ctx context.Context, q *generated.Queries, rows []generated.CalendarEvent, move func(time.Time) time.Time, withDates bool) error {
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		return rows[a].RecurrenceOriginalStart.Time.Compare(rows[b].RecurrenceOriginalStart.Time)
	})
	if len(rows) > 0 {
		probe := rows[order[0]].RecurrenceOriginalStart.Time
		if move(probe).After(probe) {
			slices.Reverse(order)
		}
	}
	for _, i := range order {
		row := &rows[i]
		if row.RecurrenceOriginalStart.Valid {
			row.RecurrenceOriginalStart.Time = move(row.RecurrenceOriginalStart.Time)
		}
		if withDates && row.StartAt.Valid {
			row.StartAt.Time = move(row.StartAt.Time)
		}
		if withDates && row.EndAt.Valid {
			row.EndAt.Time = move(row.EndAt.Time)
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

// allDayRetarget maps an occurrence's instant when a series changes between
// timed and all-day. A timed occurrence becomes the UTC midnight of its date
// in the series' zone; an all-day one becomes that date's wall-clock start,
// the series' new time of day, read in the series' new zone.
func allDayRetarget(before, after generated.CalendarEvent) func(time.Time) time.Time {
	if after.AllDay {
		loc := recurrence.LoadLocation(before.Timezone)
		return func(t time.Time) time.Time {
			y, m, d := t.In(loc).Date()
			return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		}
	}
	loc := recurrence.LoadLocation(after.Timezone)
	tod := timeOfDay(after.StartAt.Time.In(loc))
	return func(t time.Time) time.Time {
		return atWall(t.UTC(), tod, loc).UTC()
	}
}

// atWall is the wall time tod on day's calendar date in loc.
func atWall(day time.Time, tod time.Duration, loc *time.Location) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, int(tod/time.Hour), int(tod/time.Minute)%60, int(tod/time.Second)%60, int(tod%time.Second), loc)
}

// retargetUntil rewrites a rule's until into the form the series' new shape
// reads: a date for all-day, an instant for timed. An until already in that
// form, and a rule without one, are returned as they are.
func retargetUntil(rule *json.RawMessage, current generated.CalendarEvent, toAllDay bool, start time.Time, tz string) *json.RawMessage {
	parsed, err := recurrence.ParseRule(derefRaw(rule))
	if err != nil || parsed == nil || parsed.Until == nil {
		return rule
	}
	var out string
	if toAllDay {
		t, err := time.Parse(time.RFC3339, *parsed.Until)
		if err != nil {
			return rule
		}
		out = t.In(recurrence.LoadLocation(current.Timezone)).Format("2006-01-02")
	} else {
		day, err := time.Parse("2006-01-02", *parsed.Until)
		if err != nil {
			return rule
		}
		loc := recurrence.LoadLocation(tz)
		out = atWall(day, timeOfDay(start.In(loc)), loc).UTC().Format(time.RFC3339)
	}
	parsed.Until = &out
	body, err := json.Marshal(parsed)
	if err != nil {
		return rule
	}
	msg := json.RawMessage(body)
	return &msg
}

func derefRaw(r *json.RawMessage) []byte {
	if r == nil {
		return nil
	}
	return *r
}

// elementStart reads one stored exclusion as the occurrence start it names in
// a series shaped like ev. An element that cannot be read, or that names a
// date on a timed series, is reported as not found and left alone.
func elementStart(el json.RawMessage, ev generated.CalendarEvent) (time.Time, bool) {
	var str string
	if err := json.Unmarshal(el, &str); err != nil {
		return time.Time{}, false
	}
	loc := recurrence.LoadLocation(recurrence.ExpansionZone(ev.AllDay, ev.Timezone))
	utcMidnight := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	if t, err := time.Parse("2006-01-02", str); err == nil {
		if ev.AllDay {
			return t, true
		}
		return atWall(t, timeOfDay(ev.StartAt.Time.In(loc)), loc).UTC(), true
	}
	if t, err := time.Parse(time.RFC3339Nano, str); err == nil {
		if ev.AllDay {
			return utcMidnight(t.UTC()), true
		}
		return t, true
	}
	if t, err := time.Parse("2006-01-02T15:04:05", str); err == nil {
		if ev.AllDay {
			return utcMidnight(t), true
		}
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc).UTC(), true
	}
	return time.Time{}, false
}

// remapExceptions carries an exclusion list onto the series as it now is.
// Each readable element is moved by move (nil keeps its instant) and dropped
// when live (nil keeps everything) says the new grid has no occurrence there;
// one that stays where it was keeps its spelling. Unreadable elements are kept
// as they are.
func remapExceptions(list recurrence.ExceptionList, before, after generated.CalendarEvent, move func(time.Time) time.Time, live func(time.Time) bool) recurrence.ExceptionList {
	out := make(recurrence.ExceptionList, 0, len(list))
	for _, el := range list {
		at, ok := elementStart(el, before)
		if !ok {
			out = append(out, el)
			continue
		}
		moved := at
		if move != nil {
			moved = move(at)
		}
		if live != nil && !live(moved) {
			continue
		}
		if moved.Equal(at) && before.AllDay == after.AllDay {
			out = append(out, el)
			continue
		}
		out = out.With(moved, after.AllDay, after.Timezone)
	}
	return out
}

func equalColumns(a, b *json.RawMessage) bool {
	return bytes.Equal(derefRaw(a), derefRaw(b))
}

// moveReminderKeys re-keys the send records of a series with move, so a
// reminder already sent for an occurrence is not sent again under its new
// instant.
func moveReminderKeys(ctx context.Context, q *generated.Queries, eventID uint32, move func(time.Time) time.Time) error {
	rows, err := q.ListEventReminderStarts(ctx, eventID)
	if err != nil {
		return err
	}
	if len(rows) > 0 && move(rows[0].OccurrenceStart).After(rows[0].OccurrenceStart) {
		slices.Reverse(rows)
	}
	for _, r := range rows {
		moved := move(r.OccurrenceStart)
		if moved.Equal(r.OccurrenceStart) {
			continue
		}
		if err := q.SetEventReminderStart(ctx, generated.SetEventReminderStartParams{OccurrenceStart: moved, ID: r.ID}); err != nil {
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
