package dav

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	goical "github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/ical"
	"github.com/libraz/nodate-time/apps/api/internal/icsexport"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// A PUT is not stored as sent. The body and the server's own drawing of the
// resource are read by the same strict parser, and what differs is applied
// through the REST event handlers, which decide every right and every rule
// exactly as they do for the app. What iCalendar cannot say (labels, owner,
// participants, checklists, attachments) is therefore never touched.

// writable reports whether role may change what cal holds, as the
// collection's read-only flag advertises.
func writable(cal generated.Calendar, role generated.CalendarMembersRole) bool {
	return calresolve.CanWrite(role) && cal.Kind != generated.CalendarsKindSystem
}

// matches compares a conditional header with an ETag strongly: "*", or
// exactly that one tag.
func matches(cond webdav.ConditionalMatch, etag string) bool {
	v := strings.TrimSpace(string(cond))
	return v == "*" || v == `"`+etag+`"`
}

// PutCalendarObject implements caldav.Backend.
func (b *backend) PutCalendarObject(ctx context.Context, p string, data *goical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	cal, member, t, err := b.calendarAt(ctx, p, levelObject)
	if err != nil {
		return nil, err
	}
	if !writable(cal, member.Role) {
		return nil, httpError(http.StatusForbidden)
	}
	text := putBodyFrom(ctx)
	if text == nil {
		var buf bytes.Buffer
		if err := goical.NewEncoder(&buf).Encode(data); err != nil {
			return nil, refuse(ctx, invalidData(err.Error()))
		}
		text = buf.Bytes()
	}
	in, err := readIncoming(data, text)
	if err != nil {
		return nil, refuse(ctx, err)
	}
	w := b.writer(cal)
	out := &caldav.CalendarObject{Path: b.calendarPath(publicUUID(cal.PublicID)) + t.name}

	s, res, found, err := b.lookup(ctx, cal, t.name)
	if err != nil {
		return nil, err
	}
	if !found {
		if opts.IfMatch.IsSet() {
			return nil, httpError(http.StatusPreconditionFailed)
		}
		// Default names and UIDs are refused whether or not a series the
		// reader cannot see carries them, so the answer says nothing of it.
		if strings.HasPrefix(t.name, defaultNamePrefix) || strings.HasSuffix(in.uid, defaultUIDSuffix) {
			return nil, httpError(http.StatusForbidden)
		}
		created := false
		err := withCalendarLock(ctx, b.s.deps.Events.DB, publicUUID(cal.PublicID).String(), func() error {
			// A create racing this one may have taken the name meanwhile.
			var err error
			if s, res, found, err = b.lookup(ctx, cal, t.name); err != nil || found {
				return err
			}
			created = true
			return w.create(ctx, t.name, in)
		})
		if err != nil {
			return nil, refuse(ctx, err)
		}
		if created {
			return out, nil
		}
	}

	if opts.IfNoneMatch.IsSet() || opts.IfMatch.IsSet() {
		co, err := b.object(cal, *s, res)
		if err != nil {
			return nil, err
		}
		if opts.IfNoneMatch.IsSet() && matches(opts.IfNoneMatch, co.ETag) {
			return nil, httpError(http.StatusPreconditionFailed)
		}
		if opts.IfMatch.IsSet() && !matches(opts.IfMatch, co.ETag) {
			return nil, httpError(http.StatusPreconditionFailed)
		}
	}
	if in.uid != res.uid {
		return nil, refuse(ctx, &refusal{precondition: caldav.PreconditionNoUIDConflict, reason: "UID differs from the resource's"})
	}
	if err := w.update(ctx, *s, in); err != nil {
		return nil, refuse(ctx, err)
	}
	// No ETag: what is stored is not the body sent, so the client reads it back.
	return out, nil
}

// DeleteCalendarObject implements caldav.Backend. It deletes the whole
// series; the resource's name stays recorded, so a sync can report it gone.
func (b *backend) DeleteCalendarObject(ctx context.Context, p string) error {
	cal, member, t, err := b.calendarAt(ctx, p, levelObject)
	if err != nil {
		return err
	}
	if !writable(cal, member.Role) {
		return httpError(http.StatusForbidden)
	}
	s, res, found, err := b.lookup(ctx, cal, t.name)
	if err != nil {
		return err
	}
	if !found {
		return httpError(http.StatusNotFound)
	}
	if cond := ifMatchFrom(ctx); cond.IsSet() {
		co, err := b.object(cal, *s, res)
		if err != nil {
			return err
		}
		if !matches(cond, co.ETag) {
			return httpError(http.StatusPreconditionFailed)
		}
	}
	w := b.writer(cal)
	return w.ops.delete(ctx, publicUUID(s.PublicID).String(), scopeAll)
}

// writer applies PUT bodies to one calendar.
type writer struct {
	b   *backend
	cal generated.Calendar
	ops restOps
}

func (b *backend) writer(cal generated.Calendar) writer {
	return writer{b: b, cal: cal, ops: restOps{deps: b.s.deps.Events, calendar: publicUUID(cal.PublicID).String()}}
}

// storedGrid is what generates head's occurrences as stored.
func storedGrid(head generated.CalendarEvent) grid {
	g := grid{start: head.StartAt.Time, end: head.EndAt.Time, form: seriesForm{allDay: head.AllDay, zone: head.Timezone}}
	if head.RecurrenceRule != nil {
		g.rule, _ = recurrence.ParseRule(*head.RecurrenceRule)
	}
	return g
}

// bodyGrid is what generates the occurrences once in is applied: its master's
// when it carries one, else the stored series'.
func bodyGrid(in incoming, stored grid) grid {
	if in.master == nil {
		return stored
	}
	m := in.master
	return grid{rule: m.parsedRule(), start: m.start, end: m.end, form: seriesForm{allDay: m.allDay, zone: m.zone}}
}

// prevalidate holds every edit a body could produce to the REST schemas
// before any is applied, so a body refused for its data changes nothing.
func prevalidate(master change, in incoming) error {
	if !master.empty() {
		if err := validate(bodySchemas.patch, patchBody(master).Body); err != nil {
			return err
		}
	}
	for _, o := range in.overrides {
		if err := validate(bodySchemas.patch, patchBody(everything(o)).Body); err != nil {
			return err
		}
	}
	return nil
}

// state is a series as the reader sees it now, with its stored head.
type state struct {
	series ical.Series
	head   generated.CalendarEvent
	view   view
}

// load reads the series publicID names as the reader sees it. ok is false
// once it is gone from the reader's view.
func (w writer) load(ctx context.Context, publicID []byte) (state, bool, error) {
	q := w.b.s.deps.Queries
	s, ok, err := icsexport.LoadSeries(ctx, q, w.b.viewer(), w.cal, publicID, icsexport.LoadOptions{})
	if err != nil || !ok {
		return state{}, false, err
	}
	head, err := q.GetCalendarEventByPublicID(ctx, generated.GetCalendarEventByPublicIDParams{
		WorkspaceID: w.b.s.deps.WorkspaceID, PublicID: publicID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return state{}, false, nil
	}
	if err != nil {
		return state{}, false, err
	}
	drawn, err := rendered(*s)
	if err != nil {
		return state{}, false, err
	}
	return state{series: *s, head: head, view: currentView(drawn, seriesForm{allDay: s.AllDay, zone: s.Timezone})}, true, nil
}

// update applies a body to an existing series: the master first, then, from
// a fresh drawing of what that left, the occurrences.
func (w writer) update(ctx context.Context, s ical.Series, in incoming) error {
	cur, ok, err := w.load(ctx, s.PublicID)
	if err != nil {
		return err
	}
	if !ok {
		return httpError(http.StatusNotFound)
	}
	if cur.view.master == nil && in.master != nil {
		// The reader is shown changed occurrences of a series they may not
		// see; a master here would be a write to it.
		return httpError(http.StatusForbidden)
	}
	inView, err := incomingView(in, bodyGrid(in, storedGrid(cur.head)))
	if err != nil {
		return err
	}
	var master change
	if cur.view.master != nil && in.master != nil {
		master = diffEvent(*cur.view.master, *in.master, true)
	}
	if err := prevalidate(master, in); err != nil {
		return err
	}
	id := publicUUID(s.PublicID).String()
	if !master.empty() {
		if err := w.ops.update(ctx, id, scopeAll, master); err != nil {
			return err
		}
		// The REST edit moved changed and cancelled occurrences with the
		// series; the body is compared with where they are now.
		if cur, ok, err = w.load(ctx, s.PublicID); err != nil || !ok {
			return err
		}
	}
	return w.applyOccurrences(ctx, cur, inView)
}

// create makes a new series from a body under name, holding the calendar's
// lock.
func (w writer) create(ctx context.Context, name string, in incoming) error {
	if in.master == nil {
		return invalidData("no master to create")
	}
	if err := w.uidTaken(ctx, in.uid); err != nil {
		return err
	}
	inView, err := incomingView(in, bodyGrid(in, grid{}))
	if err != nil {
		return err
	}
	body := createBody(*in.master)
	if err := validate(bodySchemas.create, body.Body); err != nil {
		return err
	}
	if err := prevalidate(change{}, in); err != nil {
		return err
	}
	id, err := w.ops.create(ctx, body)
	if err != nil {
		return err
	}
	pub, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	if err := w.claimName(ctx, pub[:], name, in.uid); err != nil {
		if derr := w.ops.delete(ctx, id, scopeAll); derr != nil {
			err = errors.Join(err, derr)
		}
		return fmt.Errorf("dav: record resource name: %w", err)
	}
	if len(inView.exdates) == 0 && len(inView.overrides) == 0 {
		return nil
	}
	cur, ok, err := w.load(ctx, pub[:])
	if err != nil || !ok {
		return err
	}
	return w.applyOccurrences(ctx, cur, inView)
}

// uidTaken refuses a UID a series the reader sees on the calendar already
// carries. One they cannot see does not count: refusing would tell them it
// exists.
func (w writer) uidTaken(ctx context.Context, uid string) error {
	q := w.b.s.deps.Queries
	rows, err := q.GetDavEventResourcesByUID(ctx, generated.GetDavEventResourcesByUIDParams{CalendarID: w.cal.ID, Uid: uid})
	if err != nil {
		return err
	}
	for _, r := range rows {
		head, err := q.GetCalendarEventByID(ctx, r.EventID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		_, seen, err := icsexport.LoadSeries(ctx, q, w.b.viewer(), w.cal, head.PublicID, icsexport.LoadOptions{})
		if err != nil {
			return err
		}
		if seen {
			return &refusal{precondition: caldav.PreconditionNoUIDConflict, reason: "UID in use by " + r.HrefName}
		}
	}
	return nil
}

// claimName records the name and UID a client gave a new series. A name last
// held by a deleted series with the same UID is handed over rather than
// recorded twice.
func (w writer) claimName(ctx context.Context, publicID []byte, name, uid string) error {
	q := w.b.s.deps.Queries
	head, err := q.GetCalendarEventByPublicID(ctx, generated.GetCalendarEventByPublicIDParams{
		WorkspaceID: w.b.s.deps.WorkspaceID, PublicID: publicID,
	})
	if err != nil {
		return err
	}
	rows, err := q.GetDavEventResourcesByHref(ctx, generated.GetDavEventResourcesByHrefParams{CalendarID: w.cal.ID, HrefName: name})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.Uid != uid {
			continue
		}
		_, err := q.GetCalendarEventByID(ctx, r.EventID)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = q.ReassignDavEventResource(ctx, generated.ReassignDavEventResourceParams{NewEventID: head.ID, OldEventID: r.EventID})
		return err
	}
	return q.InsertDavEventResource(ctx, generated.InsertDavEventResourceParams{EventID: head.ID, HrefName: name, Uid: uid})
}

// applyOccurrences brings cur's occurrences to in's: cancellations undone,
// occurrences cancelled, then occurrences changed.
func (w writer) applyOccurrences(ctx context.Context, cur state, in view) error {
	stored := storedGrid(cur.head)
	hidden := map[string]bool{}
	if cur.head.RecurrenceRule != nil {
		rows, err := w.b.s.deps.Queries.ListRecurrenceOverridesByParent(ctx, sql.NullInt32{Int32: int32(cur.head.ID), Valid: true})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !r.RecurrenceOriginalStart.Valid {
				continue
			}
			if k := stored.form.key(r.RecurrenceOriginalStart.Time); !hasKey(cur.view.overrides, k) {
				hidden[k] = true
			}
		}
	}
	base := func(k string) (vevent, bool) {
		if cur.view.master == nil || hidden[k] {
			return vevent{}, false
		}
		occ, ok := stored.occurrence(k)
		if !ok {
			return vevent{}, false
		}
		return asGenerated(*cur.view.master, occ), true
	}
	undo, cancel, edits := occurrencePlan(cur.view, in, base)
	id := publicUUID(cur.series.PublicID).String()
	for _, k := range undo {
		if err := w.ops.undo(ctx, id, k); err != nil {
			return err
		}
	}
	for _, k := range cancel {
		if err := w.ops.delete(ctx, id+"_"+k, scopeThis); err != nil {
			return err
		}
	}
	for _, e := range edits {
		if err := w.ops.update(ctx, id+"_"+e.key, scopeThis, e.change); err != nil {
			return err
		}
	}
	return nil
}

func hasKey(m map[string]vevent, k string) bool {
	_, ok := m[k]
	return ok
}
