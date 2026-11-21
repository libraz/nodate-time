package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/eventstore"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// splitsSeries reports whether a write with this scope cuts the series at the
// occurrence its id names. At the first occurrence there is nothing before the
// cut, so the write is one to the whole series and takes that path.
func (w eventAccess) splitsSeries(scope string) bool {
	return scope == scopeFollowing && w.scope.occurrence != nil &&
		!w.scope.occurrence.StartAt.Equal(w.evt.StartAt.Time)
}

// requireRecurring is the check a following write makes on the series it
// addresses, after the task projection has been ruled out.
func requireRecurring(w eventAccess) error {
	if w.evt.TaskID.Valid {
		return apierrors.EventTaskOwned
	}
	if w.evt.RecurrenceRule == nil {
		return apierrors.EventNotRecurring
	}
	return nil
}

// seriesCut is where a series is divided: the rule as stored, the original
// start of the first occurrence that leaves it, and how the rule reads before
// that point.
type seriesCut struct {
	rule   *recurrence.Rule
	zone   string
	allDay bool
	at     time.Time
	// before counts the occurrences the rule generates before at, cancelled
	// ones included.
	before int
	// prev is the last of them.
	prev time.Time
}

// cutSeries measures master's rule against the occurrence starting at at.
func cutSeries(master generated.CalendarEvent, at time.Time) (seriesCut, error) {
	rule, err := recurrence.ParseRule(derefRaw(master.RecurrenceRule))
	if err != nil || rule == nil {
		return seriesCut{}, apierrors.EventNotRecurring
	}
	cut := seriesCut{
		rule: rule, zone: recurrence.ExpansionZone(master.AllDay, master.Timezone),
		allDay: master.AllDay, at: at,
	}
	start, end := master.StartAt.Time, master.EndAt.Time
	if rule.Count > 0 {
		// A count bounds the expansion by itself, so every occurrence can be read.
		occs := recurrence.ExpandInZone(rule, start, end, start.Add(-time.Millisecond), at, cut.zone)
		cut.before = len(occs)
		if len(occs) > 0 {
			cut.prev = occs[len(occs)-1].StartAt
		}
		return cut, nil
	}
	// Without a count, look back from the cut in growing windows: the series
	// may have run for longer than one expansion can hold.
	for span := 31 * 24 * time.Hour; ; span *= 4 {
		from := at.Add(-span)
		last := !from.After(start)
		if last {
			from = start.Add(-time.Millisecond)
		}
		if occs := recurrence.ExpandInZone(rule, start, end, from, at, cut.zone); len(occs) > 0 {
			cut.prev = occs[len(occs)-1].StartAt
			return cut, nil
		}
		if last {
			return cut, nil
		}
	}
}

// head is the rule the original series keeps: the occurrences before the cut.
func (c seriesCut) head() (*json.RawMessage, error) {
	r := *c.rule
	if r.Count > 0 {
		r.Count = c.before
	} else {
		until := c.prev.UTC().Format(time.RFC3339)
		if c.allDay {
			until = c.prev.UTC().Format(dateLayout)
		}
		r.Until = &until
	}
	return marshalRule(r)
}

func marshalRule(r recurrence.Rule) (*json.RawMessage, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	raw := json.RawMessage(body)
	return &raw, nil
}

// sameRule reports whether two stored rules say the same thing, however they
// are spelled.
func sameRule(a, b *json.RawMessage) bool {
	ra, errA := recurrence.ParseRule(derefRaw(a))
	rb, errB := recurrence.ParseRule(derefRaw(b))
	if errA != nil || errB != nil {
		return equalColumns(a, b)
	}
	return reflect.DeepEqual(ra, rb)
}

// tail is the rule of the series that starts at the cut, when the edit leaves
// the rule alone: the same grid from the cut on. A count keeps what remains of
// it, and a rule whose pattern is read off its start follows the cut's date
// (to) -- or, when the edit moved nothing, keeps the date the original grid is
// anchored on, so a month's clamped day does not become the new anchor. A move
// shifts every day of the month the rule names; one that leaves 1..31 needs
// the edit to send a rule.
func (c seriesCut) tail(master generated.CalendarEvent, from, to time.Time, toZone string) (recurrence.Rule, error) {
	r := *c.rule
	r.ByDay = append([]string(nil), r.ByDay...)
	r.ByMonthDay = append([]int(nil), r.ByMonthDay...)
	r.BySetPos = append([]int(nil), r.BySetPos...)
	if r.Count > 0 {
		r.Count -= c.before
	}
	f := from.In(recurrence.LoadLocation(c.zone))
	t := to.In(recurrence.LoadLocation(toZone))
	moved := f.Year() != t.Year() || f.Month() != t.Month() || f.Day() != t.Day()
	anchorDay := master.StartAt.Time.In(recurrence.LoadLocation(c.zone)).Day()
	switch r.Freq {
	case "weekly":
		if delta := (int(t.Weekday()) - int(f.Weekday()) + 7) % 7; delta != 0 {
			for i, code := range r.ByDay {
				r.ByDay[i] = weekdayCodes[(int(weekdayByCode[code])+delta)%7]
			}
		}
	case "monthly":
		switch {
		case len(r.ByDay) == 0 && len(r.ByMonthDay) == 0:
			day := anchorDay
			if moved {
				day = t.Day()
			}
			if day != t.Day() {
				r.ByMonthDay = []int{day}
			}
		case len(r.ByMonthDay) == 1 && moved:
			r.ByMonthDay = []int{t.Day()}
		case len(r.ByMonthDay) > 1 && moved:
			for i, d := range r.ByMonthDay {
				r.ByMonthDay[i] = d + t.Day() - f.Day()
			}
		case len(r.ByDay) == 1 && len(r.BySetPos) == 1 && moved:
			r.ByDay = []string{weekdayCodes[t.Weekday()]}
			if r.BySetPos[0] != -1 {
				r.BySetPos = []int{(t.Day()-1)/7 + 1}
			}
		}
	case "yearly":
		// A cut at a Feb 28 that stands in for Feb 29 keeps the leap day: the
		// same grid, spelled as every twelfth month on the 29th.
		if !moved && anchorDay != t.Day() {
			r.Freq, r.Interval, r.ByMonthDay = "monthly", max(r.Interval, 1)*12, []int{anchorDay}
		}
	}
	if r.Validate(t) != nil {
		return recurrence.Rule{}, apierrors.Validation
	}
	return r, nil
}

var (
	weekdayCodes  = [7]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}
	weekdayByCode = map[string]time.Weekday{
		"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
		"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
	}
)

// splitExceptions divides an exclusion list at the cut: the elements before it
// stay, those from it on are returned. An element that cannot be read stays.
func splitExceptions(list recurrence.ExceptionList, master generated.CalendarEvent, at time.Time) (kept, moved recurrence.ExceptionList) {
	for _, el := range list {
		if start, ok := elementStart(el, master); ok && !start.Before(at) {
			moved = append(moved, el)
			continue
		}
		kept = append(kept, el)
	}
	return kept, moved
}

// trimSeries cuts master down to the occurrences before the cut: the rule, the
// end boundary derived from it, the exclusions that fall before the cut.
// Overrides from the cut on are not touched; the caller hands them on or
// removes them, then calls reachOverrides.
func trimSeries(ctx context.Context, q *generated.Queries, master generated.CalendarEvent, cut seriesCut) (moved recurrence.ExceptionList, err error) {
	head, err := cut.head()
	if err != nil {
		return nil, err
	}
	parsed, _ := recurrence.ParseRule(*head)
	end := recurrence.ComputeEndInZone(parsed, master.StartAt.Time, master.EndAt.Time, cut.zone)
	if err := q.SetSeriesRule(ctx, generated.SetSeriesRuleParams{
		RecurrenceRule: head,
		RecurrenceEnd:  sql.NullTime{Time: end, Valid: true},
		ID:             master.ID,
	}); err != nil {
		return nil, err
	}
	stored := recurrence.ParseExceptionList(master.RecurrenceExceptions)
	kept, moved := splitExceptions(stored, master, cut.at)
	if len(moved) == 0 {
		return nil, nil
	}
	column, err := kept.MarshalColumn()
	if err != nil {
		return nil, err
	}
	return moved, q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
		RecurrenceExceptions: column,
		ID:                   master.ID,
	})
}

// reachOverrides widens a series' end boundary over the overrides it still
// holds, so one moved past the rule's last occurrence stays in range queries.
func reachOverrides(ctx context.Context, q *generated.Queries, seriesID uint32) error {
	rows, err := q.ListRecurrenceOverridesByParent(ctx, sql.NullInt32{Int32: int32(seriesID), Valid: true})
	if err != nil {
		return err
	}
	var last time.Time
	for _, o := range rows {
		if o.EndAt.Valid && o.EndAt.Time.After(last) {
			last = o.EndAt.Time
		}
	}
	if last.IsZero() {
		return nil
	}
	bound := sql.NullTime{Time: last, Valid: true}
	return q.ExtendRecurrenceEnd(ctx, generated.ExtendRecurrenceEndParams{RecurrenceEnd: bound, ID: seriesID, Boundary: bound})
}

// overridesFrom reads the override rows standing in for occurrences at or after at.
func overridesFrom(ctx context.Context, q *generated.Queries, seriesID uint32, at time.Time) ([]generated.CalendarEvent, error) {
	rows, err := q.ListRecurrenceOverridesByParent(ctx, sql.NullInt32{Int32: int32(seriesID), Valid: true})
	if err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, o := range rows {
		if o.RecurrenceOriginalStart.Valid && !o.RecurrenceOriginalStart.Time.Before(at) {
			out = append(out, o)
		}
	}
	return out, nil
}

func rowIDs(rows []generated.CalendarEvent) []uint32 {
	out := make([]uint32, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// splitSeriesEdit divides a series at the occurrence w names and applies the
// edit to the part from there on, which becomes a series of its own. The part
// before stays on the original. w must come from lockEventAccess in q's
// transaction with a scope that names an occurrence other than the first.
//
// The dates in the request are those of the named occurrence, measured as they
// are for an edit of the whole series.
func splitSeriesEdit(ctx context.Context, q *generated.Queries, deps Deps, e eventEdit, w eventAccess) (editedRows, error) {
	master := w.evt
	occ := *w.scope.occurrence
	f := mergeFields(e, master)
	body := &e.body.Body
	oldZone := recurrence.ExpansionZone(master.AllDay, master.Timezone)
	newZone := recurrence.ExpansionZone(body.AllDay, f.timezone)
	shownStart, shownEnd := w.scope.shown()
	newStart, newEnd := ontoSlot(occ, shownStart, shownEnd, e.startAt, e.endAt, f.timezone)

	cut, err := cutSeries(master, occ.StartAt)
	if err != nil {
		return editedRows{}, err
	}
	toggled := master.AllDay != body.AllDay
	ruleChanged := !sameRule(body.RecurrenceRule, master.RecurrenceRule)
	regrid := ruleChanged || toggled

	rule := body.RecurrenceRule
	if !ruleChanged {
		var err error
		tail, err := cut.tail(master, occ.StartAt, newStart, newZone)
		if err != nil {
			return editedRows{}, err
		}
		if rule, err = marshalRule(tail); err != nil {
			return editedRows{}, err
		}
		if toggled {
			rule = retargetUntil(rule, master, body.AllDay, newStart, f.timezone)
		}
	}
	if spec := checkRecurrenceRule(rule, newStart, newZone); spec != nil {
		return editedRows{}, spec
	}
	ruleData, recEnd := prepareRecurrence(rule, newStart, newEnd, newZone)

	pub, err := uuid.NewV7()
	if err != nil {
		return editedRows{}, err
	}
	result, err := q.CreateCalendarEvent(ctx, generated.CreateCalendarEventParams{
		PublicID:           pub[:],
		WorkspaceID:        deps.WorkspaceID,
		CalendarID:         e.cal.ID,
		Kind:               master.Kind,
		Visibility:         f.visibility,
		ShowAs:             f.showAs,
		Flexibility:        f.flexibility,
		Title:              body.Title,
		AllDay:             body.AllDay,
		StartAt:            sql.NullTime{Time: newStart, Valid: true},
		EndAt:              sql.NullTime{Time: newEnd, Valid: true},
		Timezone:           f.timezone,
		Location:           nullString(body.Location),
		Memo:               nullString(body.Memo),
		URL:                nullString(body.URL),
		OwnerUserID:        f.ownerID,
		CreatedByUserID:    master.CreatedByUserID,
		BlockLabel:         master.BlockLabel,
		NotificationOffset: ptrIntToNullInt32(body.NotificationOffset),
		RecurrenceRule:     ruleData,
		RecurrenceEnd:      recEnd,
	})
	if err != nil {
		return editedRows{}, err
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return editedRows{}, err
	}
	next, err := q.GetCalendarEventByID(ctx, uint32(newID))
	if err != nil {
		return editedRows{}, err
	}
	if err := copyParticipants(ctx, q, deps.WorkspaceID, master.ID, next.ID, e); err != nil {
		return editedRows{}, err
	}
	if err := copyChecklist(ctx, q, deps.WorkspaceID, master.ID, next.ID); err != nil {
		return editedRows{}, err
	}

	moved, err := trimSeries(ctx, q, master, cut)
	if err != nil {
		return editedRows{}, err
	}
	tail, err := overridesFrom(ctx, q, master.ID, cut.at)
	if err != nil {
		return editedRows{}, err
	}
	if regrid {
		// What the old grid keyed no longer names anything on the new one.
		if err := eventstore.DeleteOverrides(ctx, q, sql.NullInt32{Int32: int32(master.ID), Valid: true}, rowIDs(tail)); err != nil {
			return editedRows{}, err
		}
	} else {
		move := func(t time.Time) time.Time { return t }
		if shift := newWallShift(occ.StartAt, newStart, oldZone, newZone); !shift.identity() {
			move = shift.apply
		}
		if err := handOver(ctx, q, master, next, tail, moved, cut.at, move); err != nil {
			return editedRows{}, err
		}
	}
	if err := reachOverrides(ctx, q, master.ID); err != nil {
		return editedRows{}, err
	}
	if err := reachOverrides(ctx, q, next.ID); err != nil {
		return editedRows{}, err
	}

	extra := map[string]any{"scope": "following", "originalStart": occ.StartAt.Format(time.RFC3339)}
	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID, CalendarID: e.cal.ID, ActorUserID: e.actorID,
		Type: eventlog.TypeEventUpdated, Title: master.Title, Subject: master.PublicID, Extra: extra,
	}); err != nil {
		return editedRows{}, err
	}
	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID, CalendarID: e.cal.ID, ActorUserID: e.actorID,
		Type: eventlog.TypeEventCreated, Title: body.Title, Subject: pub[:], Extra: splitExtra(extra, master.PublicID),
	}); err != nil {
		return editedRows{}, err
	}
	created, err := q.GetCalendarEventByID(ctx, next.ID)
	if err != nil {
		return editedRows{}, err
	}
	return editedRows{series: created}, nil
}

// handOver gives the new series what hangs off the occurrences from the cut
// on, each keyed by the instant its occurrence has on the new grid: the
// exclusions, the override rows and the reminders already sent.
func handOver(ctx context.Context, q *generated.Queries, from, to generated.CalendarEvent, overrides []generated.CalendarEvent, exclusions recurrence.ExceptionList, at time.Time, move func(time.Time) time.Time) error {
	if len(exclusions) > 0 {
		list := remapExceptions(exclusions, from, to, move, nil)
		column, err := list.MarshalColumn()
		if err != nil {
			return err
		}
		if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
			RecurrenceExceptions: column,
			ID:                   to.ID,
		}); err != nil {
			return err
		}
	}
	parent := sql.NullInt32{Int32: int32(to.ID), Valid: true}
	for _, o := range overrides {
		params := generated.MoveRecurrenceOverrideParams{
			RecurrenceParentID:      parent,
			RecurrenceOriginalStart: sql.NullTime{Time: move(o.RecurrenceOriginalStart.Time), Valid: true},
			StartAt:                 o.StartAt,
			EndAt:                   o.EndAt,
			ID:                      o.ID,
		}
		if o.StartAt.Valid {
			params.StartAt.Time = move(o.StartAt.Time)
		}
		if o.EndAt.Valid {
			params.EndAt.Time = move(o.EndAt.Time)
		}
		if err := q.MoveRecurrenceOverride(ctx, params); err != nil {
			return err
		}
	}
	sent, err := q.ListEventReminderStarts(ctx, from.ID)
	if err != nil {
		return err
	}
	for _, r := range sent {
		if r.OccurrenceStart.Before(at) {
			continue
		}
		if err := q.MoveEventReminder(ctx, generated.MoveEventReminderParams{
			EventID:         to.ID,
			OccurrenceStart: move(r.OccurrenceStart),
			ID:              r.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// copyParticipants gives the new series the participants the edit names, each
// keeping the response and edit grant it holds on the original.
func copyParticipants(ctx context.Context, q *generated.Queries, workspaceID, fromID, toID uint32, e eventEdit) error {
	held, err := q.ListEventAttendees(ctx, sql.NullInt32{Int32: int32(fromID), Valid: true})
	if err != nil {
		return err
	}
	byUser := make(map[uint32]generated.ListEventAttendeesRow, len(held))
	for _, a := range held {
		byUser[a.UserID] = a
	}
	for _, p := range e.participants {
		pub, err := uuid.NewV7()
		if err != nil {
			return err
		}
		row, ok := byUser[p.UserID]
		if !ok {
			row.Rsvp = generated.CalendarEventAttendeesRsvpPending
		}
		if err := q.AddOverrideAttendee(ctx, generated.AddOverrideAttendeeParams{
			PublicID:    pub[:],
			WorkspaceID: workspaceID,
			EventID:     sql.NullInt32{Int32: int32(toID), Valid: true},
			UserID:      p.UserID,
			Rsvp:        row.Rsvp,
			CanEdit:     row.CanEdit,
		}); err != nil {
			return err
		}
	}
	return nil
}

// copyChecklist gives the new series a copy of the original's checklist.
func copyChecklist(ctx context.Context, q *generated.Queries, workspaceID, fromID, toID uint32) error {
	items, err := q.ListChecklistItems(ctx, generated.ListChecklistItemsParams{WorkspaceID: workspaceID, EventID: fromID})
	if err != nil {
		return err
	}
	for _, it := range items {
		pub, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if _, err := q.CreateChecklistItem(ctx, generated.CreateChecklistItemParams{
			PublicID:        pub[:],
			WorkspaceID:     workspaceID,
			EventID:         toID,
			CreatedByUserID: it.CreatedByUserID,
			Title:           it.Title,
			Done:            it.Done,
			SortWeight:      it.SortWeight,
		}); err != nil {
			return err
		}
	}
	return nil
}

// splitSeriesDelete removes the occurrences from the one w names on: the
// original series is cut short and the override rows from there on go with it.
func splitSeriesDelete(ctx context.Context, q *generated.Queries, deps Deps, actorID uint32, w eventAccess) error {
	master := w.evt
	occ := *w.scope.occurrence
	cut, err := cutSeries(master, occ.StartAt)
	if err != nil {
		return err
	}
	if _, err := trimSeries(ctx, q, master, cut); err != nil {
		return err
	}
	tail, err := overridesFrom(ctx, q, master.ID, cut.at)
	if err != nil {
		return err
	}
	if err := eventstore.DeleteOverrides(ctx, q, sql.NullInt32{Int32: int32(master.ID), Valid: true}, rowIDs(tail)); err != nil {
		return err
	}
	if err := reachOverrides(ctx, q, master.ID); err != nil {
		return err
	}
	return eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  w.cal.ID,
		ActorUserID: actorID,
		Type:        eventlog.TypeEventDeleted,
		Title:       master.Title,
		Subject:     master.PublicID,
		Extra: map[string]any{
			"scope":         "following",
			"originalStart": occ.StartAt.Format(time.RFC3339),
		},
	})
}

// splitExtra is the creation entry's payload for the series a cut starts: it
// names the series it was cut from, which a source sync follows to undo the cut.
func splitExtra(extra map[string]any, from []byte) map[string]any {
	out := make(map[string]any, len(extra)+1)
	for k, v := range extra {
		out[k] = v
	}
	out["splitFrom"] = eventlog.UUID(from)
	return out
}
