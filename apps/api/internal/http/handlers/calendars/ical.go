package calendars

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/http/daterange"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/ical"
	"github.com/libraz/nodate-time/apps/api/internal/icsexport"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

// --- Export ---

type ExportInput struct {
	CalendarID string `path:"calendarId"`
	Format     string `query:"format" enum:"ics,csv" default:"ics"`
	From       string `query:"from" doc:"first day to export (YYYY-MM-DD)" required:"false"`
	To         string `query:"to" doc:"last day to export, inclusive (YYYY-MM-DD)" required:"false"`
	// TZ names the zone from and to are days in, read the way the events
	// listing reads it: the requested zone, else the caller's own, else UTC.
	TZ string `query:"tz" required:"false" doc:"IANA timezone the dates are read in"`
}

type ExportOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	// ExportWindow states the range the body actually covers, so a caller can
	// tell a calendar with nothing in a period from an export that stopped
	// short of it: "full" for the whole calendar, else "from/to" with an open
	// end left empty.
	ExportWindow string `header:"X-Export-Window"`
	Body         []byte
}

// csvWindow bounds the CSV export, which lists one row per occurrence and so
// cannot represent an unbounded series. The iCalendar export carries the rule
// itself and needs no bound.
const (
	csvWindowPast   = -5 * 365 * 24 * time.Hour
	csvWindowFuture = 10 * 365 * 24 * time.Hour
	// csvMaxWindow is the widest CSV window accepted. The expander steps at
	// most 10000 candidates per series, which a daily series exhausts in
	// about 27 years; a wider window would end each one short without saying.
	csvMaxWindow = csvWindowFuture - csvWindowPast
)

// exportWindow is the range an export covers. A zero bound means unbounded.
// Both bounds are in the zone the dates were read in.
type exportWindow struct {
	start time.Time
	end   time.Time
	loc   *time.Location
}

func (w exportWindow) header() string {
	if w.start.IsZero() && w.end.IsZero() {
		return "full"
	}
	from, to := "", ""
	if !w.start.IsZero() {
		from = w.start.In(w.loc).Format("2006-01-02")
	}
	if !w.end.IsZero() {
		to = w.end.In(w.loc).Format("2006-01-02")
	}
	return from + "/" + to
}

// parseExportWindow reads the optional from/to parameters as calendar days in
// loc. An absent bound stays zero, which means "no bound" rather than a
// default the caller cannot see.
func parseExportWindow(from, to string, loc *time.Location) (exportWindow, error) {
	w := exportWindow{loc: loc}
	if from != "" {
		t, err := time.ParseInLocation("2006-01-02", from, loc)
		if err != nil {
			return w, err
		}
		w.start = t
	}
	if to != "" {
		t, err := time.ParseInLocation("2006-01-02", to, loc)
		if err != nil {
			return w, err
		}
		// Inclusive: a caller asking for the 5th means through the end of it.
		w.end = t.AddDate(0, 0, 1)
	}
	if !w.start.IsZero() && !w.end.IsZero() && !w.end.After(w.start) {
		return w, fmt.Errorf("empty window")
	}
	return w, nil
}

// actorTimezone is the zone the caller reads their calendar in, used when a
// request names dates without saying which zone they are days in.
func actorTimezone(ctx context.Context, deps Deps, userID uint32) string {
	u, err := deps.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return ""
	}
	return u.Timezone
}

func ExportEvents(deps Deps) func(context.Context, *ExportInput) (*ExportOutput, error) {
	return func(ctx context.Context, in *ExportInput) (*ExportOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		loc := daterange.Location(in.TZ, actorTimezone(ctx, deps, userID))
		window, err := parseExportWindow(in.From, in.To, loc)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		viewer := eventacl.Reader{UserID: userID, IsMember: true}
		if in.Format == "csv" {
			return exportCSV(ctx, deps, cal, viewer, window)
		}
		return exportICS(ctx, deps, cal, viewer, window)
	}
}

// exportICS writes the calendar as iCalendar, series as series.
func exportICS(ctx context.Context, deps Deps, cal generated.Calendar, viewer eventacl.Reader, window exportWindow) (*ExportOutput, error) {
	series, err := icsexport.LoadCalendar(ctx, deps.Queries, viewer, cal,
		icsexport.LoadOptions{Start: window.start, End: window.end})
	if err != nil {
		return nil, exportLoadError(ctx, err)
	}
	return &ExportOutput{
		ContentType:        "text/calendar; charset=utf-8",
		ContentDisposition: exportDisposition(cal.Name, "ics"),
		ExportWindow:       window.header(),
		Body: ical.Render(series, ical.RenderOptions{
			Now: time.Now(), AppName: deps.AppName, CalendarName: cal.Name,
		}),
	}, nil
}

// exportCSV writes one row per occurrence, which means a series has to be
// expanded and so bounded. The bound is whatever the caller asked for, and the
// response says what it was.
func exportCSV(ctx context.Context, deps Deps, cal generated.Calendar, viewer eventacl.Reader, window exportWindow) (*ExportOutput, error) {
	// One reading of the clock, so the default window is exactly csvMaxWindow wide.
	now := time.Now().In(window.loc)
	if window.start.IsZero() {
		window.start = now.Add(csvWindowPast)
	}
	if window.end.IsZero() {
		window.end = now.Add(csvWindowFuture)
	}
	if window.end.Sub(window.start) > csvMaxWindow {
		return nil, apierrors.ToHuma(apierrors.BadRequest)
	}

	series, err := icsexport.LoadCalendar(ctx, deps.Queries, viewer, cal,
		icsexport.LoadOptions{Start: window.start, End: window.end, Expand: true})
	if err != nil {
		return nil, exportLoadError(ctx, err)
	}
	return &ExportOutput{
		ContentType:        "text/csv; charset=utf-8",
		ContentDisposition: exportDisposition(cal.Name, "csv"),
		ExportWindow:       window.header(),
		Body:               icsexport.RenderCSV(series),
	}, nil
}

// exportDisposition names the download after the calendar, in the header
// shape every download here uses: an ASCII fallback plus the percent-encoded
// UTF-8 name.
func exportDisposition(calName, ext string) string {
	name := sanitizeFilename(calName)
	if name == "" {
		name = "calendar"
	}
	return storage.ContentDisposition(name + "." + ext)
}

// sanitizeFilename keeps a calendar name from reading as a path or breaking
// out of the header: separators, quotes, spaces and control characters become
// underscores.
func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == '"' || r == ' ':
			return '_'
		case r < 0x20 || r == 0x7f:
			return '_'
		}
		return r
	}, strings.ToValidUTF8(s, "_"))
}

// exportLoadError answers a failed load, a range holding more than the export
// bound as the client's to narrow.
func exportLoadError(ctx context.Context, err error) error {
	if errors.Is(err, icsexport.ErrTooLarge) {
		return apierrors.ToHuma(apierrors.ExportRangeTooLarge)
	}
	return apierrors.Respond(ctx, err)
}
