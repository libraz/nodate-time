package dav

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/emersion/go-webdav/caldav"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

// anyVersion is the If-Match every REST call carries: the DAV ETag has
// already been compared, and the REST tags differ row by row.
const anyVersion = precondition.IfMatch("*")

const (
	scopeAll  = "all"
	scopeThis = "this"
)

// calendarLockWait is how long a create waits for another create on the same
// calendar before giving up.
const calendarLockWait = 5

// bodySchemas are the REST request body schemas, so a body built here is held
// to the same tags a body sent to the REST API is.
var bodySchemas = newBodySchemas()

type schemas struct {
	registry      huma.Registry
	create, patch *huma.Schema
}

func newBodySchemas() schemas {
	r := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	return schemas{
		registry: r,
		create:   huma.SchemaFromType(r, reflect.TypeOf(events.CreateEventInput{}.Body)),
		patch:    huma.SchemaFromType(r, reflect.TypeOf(events.PatchEventInput{}.Body)),
	}
}

// validate holds body to schema s as huma holds a request body.
func validate(s *huma.Schema, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	res := &huma.ValidateResult{}
	huma.Validate(bodySchemas.registry, s, huma.NewPathBuffer(make([]byte, 0, 128), 0), huma.ModeWriteToServer, v, res)
	if len(res.Errors) > 0 {
		return invalidData(res.Errors[0].Error())
	}
	return nil
}

// restOps calls the REST event handlers for one calendar, as the request's
// actor.
type restOps struct {
	deps     events.Deps
	calendar string
}

// restError maps a REST refusal onto CalDAV: a refusal of the actor's right
// is 403 with no body, one of the data is 409 with a precondition.
func restError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var se huma.StatusError
	if !errors.As(err, &se) {
		return err
	}
	var he *apierrors.HumaError
	code := ""
	if errors.As(err, &he) {
		code = he.Code
	}
	switch status := se.GetStatus(); {
	case status == http.StatusForbidden, code == apierrors.EventTaskOwned.Code:
		return httpError(http.StatusForbidden)
	case status == http.StatusNotFound, status == http.StatusPreconditionFailed:
		return httpError(status)
	case status == http.StatusBadRequest, status == http.StatusUnprocessableEntity, status == http.StatusConflict:
		return refuse(ctx, invalidData(code))
	case status >= http.StatusInternalServerError:
		return err
	default:
		return httpError(status)
	}
}

// refuse logs why a body was refused and answers with its precondition.
func refuse(ctx context.Context, err error) error {
	var r *refusal
	if !errors.As(err, &r) {
		return err
	}
	slog.InfoContext(ctx, "dav put refused", "dav.precondition", string(r.precondition), "dav.reason", r.reason)
	return caldav.NewPreconditionError(r.precondition)
}

// createBody is the REST create of v.
func createBody(v vevent) events.CreateEventInput {
	var in events.CreateEventInput
	b := &in.Body
	b.Title, b.AllDay = v.title, v.allDay
	if v.allDay {
		b.StartDate, b.EndDate = day(v.start), day(v.end)
	} else {
		b.StartAt, b.EndAt = v.start.Format(time.RFC3339), v.end.Format(time.RFC3339)
	}
	b.Timezone = v.zone
	b.Location, b.Memo, b.URL = v.location, v.description, v.url
	if s, ok := showAsOf(v.transp); ok {
		b.ShowAs = s
	}
	if vis, ok := visibilityOf(v.class); ok {
		b.Visibility = vis
	}
	if v.alarm.minutes != nil {
		m := int(*v.alarm.minutes)
		b.NotificationOffset = &m
	}
	b.RecurrenceRule = v.rule
	return in
}

// patchBody is the REST patch c sends.
func patchBody(c change) events.PatchEventInput {
	var in events.PatchEventInput
	b := &in.Body
	if c.title != nil {
		b.Title = *c.title
	}
	field := func(s *string) patch.Field[string] {
		if s == nil {
			return patch.Field[string]{}
		}
		return patch.Of(*s)
	}
	b.Location, b.Memo, b.URL = field(c.location), field(c.memo), field(c.url)
	b.Visibility, b.ShowAs = c.visibility, c.showAs
	if c.alarmSet {
		if c.alarm == nil {
			b.NotificationOffset = patch.Null[int]()
		} else {
			b.NotificationOffset = patch.Of(int(*c.alarm))
		}
	}
	if d := c.dates; d != nil {
		allDay := d.allDay
		b.AllDay = &allDay
		if d.allDay {
			b.StartDate, b.EndDate = day(d.start), day(d.end)
		} else {
			b.StartAt, b.EndAt = d.start.Format(time.RFC3339), d.end.Format(time.RFC3339)
			b.Timezone = d.zone
		}
	}
	if c.ruleSet {
		if c.rule == nil {
			b.RecurrenceRule = patch.Null[json.RawMessage]()
		} else {
			b.RecurrenceRule = patch.Of(*c.rule)
		}
	}
	return in
}

// everything is a change sending every member of v, which validates what any
// edit drawn from v could send.
func everything(v vevent) change {
	c := change{
		title: &v.title, location: &v.location, memo: &v.description, url: &v.url,
		alarmSet: v.alarm.minutes != nil, alarm: v.alarm.minutes,
		dates: &dates{allDay: v.allDay, start: v.start, end: v.end, zone: v.zone},
	}
	if vis, ok := visibilityOf(v.class); ok {
		c.visibility = vis
	}
	if s, ok := showAsOf(v.transp); ok {
		c.showAs = s
	}
	return c
}

func (o restOps) create(ctx context.Context, in events.CreateEventInput) (string, error) {
	in.CalendarID = o.calendar
	out, err := events.CreateEvent(o.deps)(ctx, &in)
	if err != nil {
		return "", restError(ctx, err)
	}
	return out.Body.ID, nil
}

func (o restOps) update(ctx context.Context, eventID, scope string, c change) error {
	in := patchBody(c)
	in.CalendarID, in.EventID, in.Scope, in.IfMatch = o.calendar, eventID, scope, anyVersion
	_, err := events.UpdateEvent(o.deps)(ctx, &in)
	return restError(ctx, err)
}

func (o restOps) delete(ctx context.Context, eventID, scope string) error {
	in := events.DeleteEventInput{CalendarID: o.calendar, EventID: eventID, Scope: scope, IfMatch: anyVersion}
	_, err := events.DeleteEvent(o.deps)(ctx, &in)
	return restError(ctx, err)
}

func (o restOps) undo(ctx context.Context, eventID, key string) error {
	in := events.UndoExceptionInput{CalendarID: o.calendar, EventID: eventID, OccurrenceDate: occurrenceDate(key), IfMatch: anyVersion}
	_, err := events.UndoException(o.deps)(ctx, &in)
	return restError(ctx, err)
}

// withCalendarLock runs fn holding the calendar's named lock, on a connection
// of its own so the lock outlives the transactions fn runs. Two creates of
// the same new resource then cannot both find it absent.
func withCalendarLock(ctx context.Context, db *sql.DB, calendar string, fn func() error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	name := "dav:" + calendar
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", name, calendarLockWait).Scan(&got); err != nil {
		return err
	}
	if !got.Valid || got.Int64 != 1 {
		return httpError(http.StatusServiceUnavailable)
	}
	defer func() {
		var released sql.NullInt64
		if err := conn.QueryRowContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK(?)", name).Scan(&released); err != nil {
			slog.WarnContext(ctx, "dav: release calendar lock", "error", err)
		}
	}()
	return fn()
}
