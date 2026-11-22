package dav

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	goical "github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/ical"
	"github.com/libraz/nodate-time/apps/api/internal/icsexport"
)

// maxResourceSize is the largest calendar object a client may store.
const maxResourceSize = 1 << 20

// backend answers one authenticated request on behalf of one user.
type backend struct {
	s        *server
	userID   uint32
	userUUID string
}

var _ caldav.SyncBackend = (*backend)(nil)

func httpError(status int) error { return webdav.NewHTTPError(status, nil) }

func (b *backend) viewer() eventacl.Reader {
	return eventacl.Reader{UserID: b.userID, IsMember: true}
}

// CurrentUserPrincipal implements webdav.UserPrincipalBackend.
func (b *backend) CurrentUserPrincipal(context.Context) (string, error) {
	return b.principalPath(), nil
}

// CalendarHomeSetPath implements caldav.Backend.
func (b *backend) CalendarHomeSetPath(context.Context) (string, error) {
	return b.homePath(), nil
}

// CreateCalendar implements caldav.Backend. Calendars are made in the app.
func (b *backend) CreateCalendar(context.Context, *caldav.Calendar) error {
	return httpError(http.StatusForbidden)
}

// ListCalendars implements caldav.Backend: every calendar the user is a
// member of, under the same membership conditions calresolve.Member applies.
func (b *backend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	rows, err := b.s.deps.Queries.ListCalendarsByUser(ctx, generated.ListCalendarsByUserParams{
		UserID:      b.userID,
		WorkspaceID: b.s.deps.WorkspaceID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]caldav.Calendar, 0, len(rows))
	for _, row := range rows {
		cal := generated.Calendar{
			ID: row.ID, PublicID: row.PublicID, WorkspaceID: row.WorkspaceID, Kind: row.Kind,
			Name: row.Name, Description: row.Description, Color: row.Color,
		}
		c, err := b.collection(ctx, cal, row.MemberRole)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// GetCalendar implements caldav.Backend.
func (b *backend) GetCalendar(ctx context.Context, p string) (*caldav.Calendar, error) {
	cal, member, _, err := b.calendarAt(ctx, p, levelCalendar)
	if err != nil {
		return nil, err
	}
	c, err := b.collection(ctx, cal, member.Role)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (b *backend) collection(ctx context.Context, cal generated.Calendar, role generated.CalendarMembersRole) (caldav.Calendar, error) {
	token, err := b.currentToken(ctx, cal, today())
	if err != nil {
		return caldav.Calendar{}, err
	}
	return caldav.Calendar{
		Path:                  b.calendarPath(publicUUID(cal.PublicID)),
		Name:                  cal.Name,
		Description:           cal.Description.String,
		Color:                 cal.Color,
		ReadOnly:              !calresolve.CanWrite(role) || cal.Kind == generated.CalendarsKindSystem,
		SupportedComponentSet: []string{goical.CompEvent},
		MaxResourceSize:       maxResourceSize,
		SyncToken:             token,
	}, nil
}

// calendarAt resolves the calendar a path at or below want names, through
// calresolve so the membership check comes with the lookup.
func (b *backend) calendarAt(ctx context.Context, p string, want level) (generated.Calendar, generated.CalendarMember, target, error) {
	t, status := b.parse(p)
	if status != 0 {
		return generated.Calendar{}, generated.CalendarMember{}, t, httpError(status)
	}
	if t.level != want {
		return generated.Calendar{}, generated.CalendarMember{}, t, httpError(http.StatusNotFound)
	}
	cal, member, err := calresolve.Member(ctx, b.s.deps.Queries, b.s.deps.WorkspaceID, t.calendar.String(), b.userID)
	if errors.Is(err, apierrors.CalendarNotFound) || errors.Is(err, apierrors.CalendarAccessDenied) {
		return generated.Calendar{}, generated.CalendarMember{}, t, httpError(http.StatusNotFound)
	}
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, t, err
	}
	return cal, member, t, nil
}

// GetCalendarObject implements caldav.Backend.
func (b *backend) GetCalendarObject(ctx context.Context, p string, _ *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	cal, _, t, err := b.calendarAt(ctx, p, levelObject)
	if err != nil {
		return nil, err
	}
	s, res, ok, err := b.lookup(ctx, cal, t.name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpError(http.StatusNotFound)
	}
	co, err := b.object(cal, *s, res)
	if err != nil {
		return nil, err
	}
	return &co, nil
}

// ListCalendarObjects implements caldav.Backend.
func (b *backend) ListCalendarObjects(ctx context.Context, p string, _ *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	cal, _, _, err := b.calendarAt(ctx, p, levelCalendar)
	if err != nil {
		return nil, err
	}
	return b.allObjects(ctx, cal, icsexport.LoadOptions{})
}

// QueryCalendarObjects implements caldav.Backend. The VEVENT time-range
// narrows the read; the filter then decides exactly.
func (b *backend) QueryCalendarObjects(ctx context.Context, p string, query *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	cal, _, _, err := b.calendarAt(ctx, p, levelCalendar)
	if err != nil {
		return nil, err
	}
	var opts icsexport.LoadOptions
	for _, c := range query.CompFilter.Comps {
		if c.Name == goical.CompEvent {
			opts.Start, opts.End = c.Start, c.End
		}
	}
	if !opts.Start.IsZero() && !opts.End.IsZero() && !opts.End.After(opts.Start) {
		return nil, nil
	}
	objs, err := b.allObjects(ctx, cal, opts)
	if err != nil {
		return nil, err
	}
	return caldav.Filter(query, objs)
}

func (b *backend) allObjects(ctx context.Context, cal generated.Calendar, opts icsexport.LoadOptions) ([]caldav.CalendarObject, error) {
	series, err := icsexport.LoadCalendar(ctx, b.s.deps.Queries, b.viewer(), cal, opts)
	if err != nil {
		return nil, loadError(ctx, cal, err)
	}
	names, err := b.names(ctx, cal)
	if err != nil {
		return nil, err
	}
	out := make([]caldav.CalendarObject, 0, len(series))
	for _, s := range series {
		res, ok := names[string(s.PublicID)]
		if !ok {
			res = defaultResource(s.PublicID)
		}
		co, err := b.object(cal, s, res)
		if err != nil {
			return nil, err
		}
		out = append(out, co)
	}
	return out, nil
}

// loadError answers a calendar too large to hold in one response as 507.
func loadError(ctx context.Context, cal generated.Calendar, err error) error {
	if errors.Is(err, icsexport.ErrTooLarge) {
		slog.WarnContext(ctx, "dav: calendar exceeds the entry bound",
			"calendar", calresolve.PublicIDString(cal.PublicID), "max", icsexport.MaxEntries)
		return webdav.NewHTTPError(http.StatusInsufficientStorage, err)
	}
	return err
}

// resource is what a series is called: its name in the collection and the
// UID its VEVENTs carry.
type resource struct {
	name, uid string
}

func defaultResource(publicID []byte) resource {
	return resource{name: defaultName(publicID), uid: ical.DefaultUID(publicID)}
}

// names maps series public IDs to the names clients gave them. Where two live
// series of the calendar share a name, the oldest keeps it and the others are
// listed under their default names.
func (b *backend) names(ctx context.Context, cal generated.Calendar) (map[string]resource, error) {
	q := b.s.deps.Queries
	rows, err := q.ListDavEventResourcesByCalendar(ctx, cal.ID)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	heads, err := q.ListCalendarEventsForExport(ctx, cal.ID)
	if err != nil {
		return nil, err
	}
	live := make(map[uint32][]byte, len(heads))
	for _, h := range heads {
		live[h.ID] = h.PublicID
	}
	out := make(map[string]resource, len(rows))
	taken := make(map[string]bool, len(rows))
	for _, r := range rows {
		pub, ok := live[r.EventID]
		if !ok {
			continue
		}
		res := resource{name: defaultName(pub), uid: r.Uid}
		if !taken[r.HrefName] {
			taken[r.HrefName] = true
			res.name = r.HrefName
		}
		out[string(pub)] = res
	}
	return out, nil
}

// resourceOf is what names gives a live series head of cal, read for one
// series; a head no longer live on cal keeps the name it was given.
func (b *backend) resourceOf(ctx context.Context, cal generated.Calendar, head generated.CalendarEvent) (resource, error) {
	q := b.s.deps.Queries
	row, err := q.GetDavEventResourceByEvent(ctx, head.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultResource(head.PublicID), nil
	}
	if err != nil {
		return resource{}, err
	}
	res := resource{name: defaultName(head.PublicID), uid: row.Uid}
	if !head.Enabled || head.CalendarID != cal.ID {
		// Gone from cal: the name it was known by there.
		res.name = row.HrefName
		return res, nil
	}
	rows, err := q.GetDavEventResourcesByHref(ctx, generated.GetDavEventResourcesByHrefParams{
		CalendarID: cal.ID, HrefName: row.HrefName,
	})
	if err != nil {
		return resource{}, err
	}
	for _, r := range rows {
		if r.EventID == head.ID {
			res.name = row.HrefName
			break
		}
		if _, err := q.GetCalendarEventByID(ctx, r.EventID); err == nil {
			break
		} else if !errors.Is(err, sql.ErrNoRows) {
			return resource{}, err
		}
	}
	return res, nil
}

// lookup resolves a resource name in cal to the series the reader sees
// under it. A default name resolves only a series no client named; any other
// name resolves the oldest live series carrying it. A deleted series is not
// found.
func (b *backend) lookup(ctx context.Context, cal generated.Calendar, name string) (*ical.Series, resource, bool, error) {
	q := b.s.deps.Queries
	ws := b.s.deps.WorkspaceID
	var head generated.CalendarEvent
	if pub, ok := defaultNameID(name); ok {
		h, err := q.GetCalendarEventByPublicID(ctx, generated.GetCalendarEventByPublicIDParams{WorkspaceID: ws, PublicID: pub})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, resource{}, false, nil
		}
		if err != nil {
			return nil, resource{}, false, err
		}
		head = h
	} else {
		rows, err := q.GetDavEventResourcesByHref(ctx, generated.GetDavEventResourcesByHrefParams{CalendarID: cal.ID, HrefName: name})
		if err != nil {
			return nil, resource{}, false, err
		}
		for _, r := range rows {
			h, err := q.GetCalendarEventByID(ctx, r.EventID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, resource{}, false, err
			}
			head = h
			break
		}
		if head.ID == 0 {
			return nil, resource{}, false, nil
		}
	}
	if head.CalendarID != cal.ID || head.RecurrenceParentID.Valid {
		return nil, resource{}, false, nil
	}
	res, err := b.resourceOf(ctx, cal, head)
	if err != nil {
		return nil, resource{}, false, err
	}
	if res.name != name {
		return nil, resource{}, false, nil
	}
	s, ok, err := icsexport.LoadSeries(ctx, q, b.viewer(), cal, head.PublicID, icsexport.LoadOptions{})
	if err != nil || !ok {
		return nil, resource{}, false, err
	}
	return s, res, true, nil
}

// object renders one series as the resource res names.
func (b *backend) object(cal generated.Calendar, s ical.Series, res resource) (caldav.CalendarObject, error) {
	now := time.Now()
	modified := lastUpdate(s)
	stamp := modified
	if stamp.IsZero() {
		stamp = now
	}
	opts := ical.RenderOptions{
		Now:          now,
		Stamp:        func(ical.Series) time.Time { return stamp },
		UID:          func(ical.Series) string { return res.uid },
		AppName:      b.s.deps.AppName,
		CalendarName: cal.Name,
	}
	data, err := goical.NewDecoder(bytes.NewReader(ical.Render([]ical.Series{s}, opts))).Decode()
	if err != nil {
		return caldav.CalendarObject{}, err
	}
	return caldav.CalendarObject{
		Path:    b.calendarPath(publicUUID(cal.PublicID)) + res.name,
		ModTime: modified,
		ETag:    ical.ContentHash(s, opts),
		Data:    data,
	}, nil
}

// lastUpdate is the latest change to any row the series renders.
func lastUpdate(s ical.Series) time.Time {
	var t time.Time
	if s.Master != nil {
		t = s.Master.UpdatedAt
	}
	for _, o := range s.Overrides {
		if o.UpdatedAt.After(t) {
			t = o.UpdatedAt
		}
	}
	return t
}
