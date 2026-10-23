package calendars

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/daterange"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
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
	// Truncated says the body stopped at exportMaxEntries, so the window is
	// not fully covered.
	Truncated string `header:"X-Result-Truncated" required:"false"`
	Body      []byte
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

// exportMaxEntries bounds one export: VEVENTs for iCalendar, rows for CSV. A
// CSV series expands per occurrence and nothing bounds how many series a
// calendar holds, so the bound is on the output.
const exportMaxEntries = 20000

// exportEvent is a single row to render as one VEVENT.
//
// A recurring series is one entry carrying its rule and its cancellations,
// not one entry per occurrence: expanding it would turn a daily series into
// thousands of unrelated single events, which is what an importing client
// would then have — a pile it can no longer edit as a series.
type exportEvent struct {
	event   generated.CalendarEvent
	startAt time.Time
	endAt   time.Time
	// rule and exceptions are set for a series head.
	rule       *recurrence.Rule
	exceptions recurrence.Exceptions
	// originalStart is set for a changed occurrence, which shares the series'
	// UID and names the occurrence it replaces. RECURRENCE-ID has to take the
	// series' value type and zone rather than the override's own, so those
	// are carried alongside.
	originalStart  time.Time
	isOverride     bool
	parentAllDay   bool
	parentTimezone string
}

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

// queryBounds is the window as the range queries take it, an open end
// standing at the first or last instant the DATETIME columns hold. The floor
// is a second past year 1's first instant because the driver writes that
// instant itself as a zero date.
func (w exportWindow) queryBounds() (start, end sql.NullTime) {
	start = sql.NullTime{Time: time.Date(1, 1, 1, 0, 0, 1, 0, time.UTC), Valid: true}
	end = sql.NullTime{Time: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), Valid: true}
	if !w.start.IsZero() {
		start.Time = w.start
	}
	if !w.end.IsZero() {
		end.Time = w.end
	}
	return start, end
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
			return nil, toAPIError(err)
		}

		loc := daterange.Location(in.TZ, actorTimezone(ctx, deps, userID))
		window, err := parseExportWindow(in.From, in.To, loc)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		if in.Format == "csv" {
			return exportCSV(ctx, deps, cal, window)
		}
		return exportICS(ctx, deps, cal, window)
	}
}

// loadExportEvents reads what an export renders. Every series' changed
// occurrences come from one query, and the output is capped once at
// exportMaxEntries, with truncated reporting whether the cap was hit.
//
// With expand set each series becomes its occurrences inside the window (CSV);
// otherwise it stays one entry carrying its rule, followed by its changed
// occurrences (iCalendar).
func loadExportEvents(
	ctx context.Context, deps Deps, cal generated.Calendar, window exportWindow, expand bool,
) (entries []exportEvent, truncated bool, err error) {
	var heads []generated.CalendarEvent
	if expand || !window.start.IsZero() || !window.end.IsZero() {
		// The same selection the listing makes, so a series whose changed
		// occurrence sits in the window is exported with it.
		rangeStart, rangeEnd := window.queryBounds()
		plain, err := deps.Queries.ListCalendarEventsByCalendarAndRange(ctx, generated.ListCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   rangeEnd,
			RangeStart: rangeStart,
		})
		if err != nil {
			return nil, false, err
		}
		series, err := deps.Queries.ListRecurringCalendarEventsByCalendarAndRange(ctx, generated.ListRecurringCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   rangeEnd,
			RangeStart: rangeStart,
		})
		if err != nil {
			return nil, false, err
		}
		heads = append(plain, series...)
	} else {
		rows, err := deps.Queries.ListCalendarEventsForExport(ctx, cal.ID)
		if err != nil {
			return nil, false, err
		}
		for _, e := range rows {
			// An undated row has nothing to render as DTSTART. The shared
			// schema allows one; this product does not create them, but the
			// export must not invent a zero time if another writer has.
			if e.StartAt.Valid && e.EndAt.Valid {
				heads = append(heads, e)
			}
		}
	}

	var seriesKeys []sql.NullInt32
	for _, e := range heads {
		if e.RecurrenceRule != nil {
			seriesKeys = append(seriesKeys, sql.NullInt32{Int32: int32(e.ID), Valid: true})
		}
	}
	overrides := eventexpand.Overrides{}
	if len(seriesKeys) > 0 {
		rows, err := deps.Queries.ListRecurrenceOverridesByParents(ctx, seriesKeys)
		if err != nil {
			return nil, false, err
		}
		for _, r := range rows {
			parent := uint32(r.RecurrenceParentID.Int32)
			overrides[parent] = append(overrides[parent], r)
		}
	}

	add := func(x exportEvent) bool {
		if len(entries) >= exportMaxEntries {
			truncated = true
			return false
		}
		entries = append(entries, x)
		return true
	}
	for _, e := range heads {
		if !e.StartAt.Valid || !e.EndAt.Valid {
			continue
		}
		if expand && e.RecurrenceRule != nil {
			// Expansion goes through eventexpand so both departures from the
			// rule are honored: cancelled occurrences stay out and changed ones
			// carry their override's values, matching what the app displays.
			for _, inst := range eventexpand.ExpandWithOverrides(e, overrides[e.ID], window.start, window.end) {
				x := exportEvent{event: e, startAt: inst.Occurrence.StartAt, endAt: inst.Occurrence.EndAt}
				if inst.IsOverride {
					if !inst.Event.StartAt.Valid || !inst.Event.EndAt.Valid {
						continue
					}
					x = exportEvent{event: inst.Event, startAt: inst.Event.StartAt.Time, endAt: inst.Event.EndAt.Time}
				}
				if !add(x) {
					return entries, truncated, nil
				}
			}
			continue
		}

		entry := exportEvent{event: e, startAt: e.StartAt.Time, endAt: e.EndAt.Time}
		if !expand && e.RecurrenceRule != nil {
			entry.rule = recurrence.ParseRule(*e.RecurrenceRule)
			entry.exceptions = recurrence.ParseExceptions(e.RecurrenceExceptions)
		}
		if !add(entry) {
			return entries, truncated, nil
		}
		if entry.rule == nil {
			continue
		}
		// A changed occurrence is its own VEVENT under the same UID, so an
		// importing client can tell it apart from the series and put it back
		// where it belongs.
		for _, child := range overrides[e.ID] {
			if !child.StartAt.Valid || !child.EndAt.Valid || !child.RecurrenceOriginalStart.Valid {
				continue
			}
			if entry.exceptions.Contains(child.RecurrenceOriginalStart.Time) {
				continue
			}
			x := exportEvent{
				event:          child,
				startAt:        child.StartAt.Time,
				endAt:          child.EndAt.Time,
				originalStart:  child.RecurrenceOriginalStart.Time,
				isOverride:     true,
				parentAllDay:   e.AllDay,
				parentTimezone: e.Timezone,
			}
			// The series' identity is what ties the occurrence to it.
			x.event.PublicID = e.PublicID
			if !add(x) {
				return entries, truncated, nil
			}
		}
	}
	return entries, truncated, nil
}

// truncatedHeader renders the X-Result-Truncated value.
func truncatedHeader(truncated bool) string {
	if truncated {
		return "true"
	}
	return ""
}

// exportICS writes the calendar as iCalendar, series as series.
func exportICS(ctx context.Context, deps Deps, cal generated.Calendar, window exportWindow) (*ExportOutput, error) {
	exports, truncated, err := loadExportEvents(ctx, deps, cal, window, false)
	if err != nil {
		return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
	}
	return &ExportOutput{
		ContentType:        "text/calendar; charset=utf-8",
		ContentDisposition: exportDisposition(cal.Name, "ics"),
		ExportWindow:       window.header(),
		Truncated:          truncatedHeader(truncated),
		Body:               []byte(buildICS(deps.AppName, cal.Name, exports)),
	}, nil
}

// exportCSV writes one row per occurrence, which means a series has to be
// expanded and so bounded. The bound is whatever the caller asked for, and the
// response says what it was.
func exportCSV(ctx context.Context, deps Deps, cal generated.Calendar, window exportWindow) (*ExportOutput, error) {
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

	exports, truncated, err := loadExportEvents(ctx, deps, cal, window, true)
	if err != nil {
		return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
	}
	return &ExportOutput{
		ContentType:        "text/csv; charset=utf-8",
		ContentDisposition: exportDisposition(cal.Name, "csv"),
		ExportWindow:       window.header(),
		Truncated:          truncatedHeader(truncated),
		Body:               buildCSV(exports),
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

// forbiddenControl reports whether r is a character RFC 5545 does not allow
// inside a value: every C0 control but HTAB, and DEL.
//
// A line break is left out of the set because a value may legally contain one
// -- it is written as an escape rather than as a raw byte, and the escaping is
// what keeps it inside the value. The rest are not text at all: a terminal
// executes an escape sequence rather than showing it, so a title carrying one
// runs a command in the shell of whoever reads the file.
func forbiddenControl(r rune) bool {
	return r == 0x7f || (r < 0x20 && r != '\t' && r != '\n')
}

// dropForbiddenControl removes those characters.
//
// They are dropped rather than costing the value or the event they arrived on:
// nothing in a calendar means anything by them, and the same rule has to hold
// on the way out, where the row is already stored and refusing it is not on
// offer. One rule in both directions is also what keeps an import and the
// export after it agreeing on what the event says.
func dropForbiddenControl(s string) string {
	if !strings.ContainsFunc(s, forbiddenControl) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if forbiddenControl(r) {
			return -1
		}
		return r
	}, s)
}

func icsEscape(s string) string {
	// Normalize all newline variants to a single \n before escaping so bare CR
	// and CRLF do not leak raw control characters into the output.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = dropForbiddenControl(s)
	r := strings.NewReplacer(
		"\\", `\\`,
		";", `\;`,
		",", `\,`,
		"\n", `\n`,
	)
	return r.Replace(s)
}

// icsURI sanitizes a URI value for a property whose value type is URI (not TEXT).
// URI values must not be TEXT-escaped, so commas and semicolons are kept as-is;
// only line breaks are stripped to avoid injecting extra content lines.
func icsURI(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return dropForbiddenControl(s)
}

func icsTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// icsDate renders an all-day date in the event's own timezone so the calendar
// day does not shift for non-UTC users. Falls back to UTC on an unknown zone.
func icsDate(t time.Time, tz string) string {
	loc := loadLocationOrUTC(tz)
	return t.In(loc).Format("20060102")
}

// loadLocationOrUTC resolves an IANA timezone name, falling back to UTC.
func loadLocationOrUTC(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// writeFolded writes a content line, folding at 75 octets per RFC 5545 §3.1.
// Continuation lines start with a single space, which counts against the same
// limit and so leaves 74 octets for the text.
//
// §3.1 asks for two things at once: the limit is counted in octets, and a
// multi-octet character is never split across the fold. Counting characters
// instead would satisfy the second and break the first, since one character
// can be four octets; cutting at the octet count without regard for where a
// character begins breaks the second, and the file then carries a byte
// sequence that is not a character. Anything reading it -- another calendar
// application, this product's own import, a JSON encoder carrying the file --
// substitutes or drops it, so the corruption reaches the copy the owner kept.
func writeFolded(b *strings.Builder, line string) {
	const limit = 75
	const cont = limit - 1

	n := foldPoint(line, limit)
	b.WriteString(line[:n])
	b.WriteString("\r\n")

	rest := line[n:]
	for rest != "" {
		n = foldPoint(rest, cont)
		b.WriteByte(' ')
		b.WriteString(rest[:n])
		b.WriteString("\r\n")
		rest = rest[n:]
	}
}

// foldPoint returns how many octets of s fit in limit without ending inside a
// character. It backs up off the continuation octets of a split sequence,
// which costs at most three octets of a line.
func foldPoint(s string, limit int) int {
	if len(s) <= limit {
		return len(s)
	}
	n := limit
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	if n == 0 {
		// Only reachable if the text is not valid UTF-8 to begin with. Writing
		// the full run is what keeps this from folding nothing forever; the
		// bytes are already not characters, and the limit still holds.
		return limit
	}
	return n
}

// icsZone is the TZID a timed value is written in, or "" when its zone is
// UTC -- or unknown, which the expander also reads as UTC -- and the Z form
// already says so.
//
// A rule expands in the zone of its DTSTART. Written as a UTC instant, a
// Monday 08:00 Tokyo series starts on a Sunday, and BYDAY=MO lands a day late
// in every client; a New York one keeps its UTC time across DST and moves an
// hour on the wall clock. A single event carries its zone the same way, so a
// re-import keeps it rather than reading the event as UTC.
func icsZone(tz string) string {
	loc := loadLocationOrUTC(tz)
	if loc == time.UTC {
		return ""
	}
	return loc.String()
}

// icsTimeProp renders a timed property as a wall-clock value in zone, or as a
// UTC instant when zone is "".
func icsTimeProp(name string, t time.Time, zone string) string {
	if zone == "" {
		return name + ":" + icsTime(t)
	}
	return name + ";TZID=" + zone + ":" + t.In(loadLocationOrUTC(zone)).Format("20060102T150405")
}

// icsExdate renders the series' cancellations. They are stored as the instants
// the occurrences would have started at, which is exactly what EXDATE names,
// written in the value type and zone DTSTART uses.
func icsExdate(ex recurrence.Exceptions, allDay bool, tz string) string {
	if len(ex) == 0 {
		return ""
	}
	zone := icsZone(tz)
	values := make([]string, 0, len(ex))
	for _, t := range ex {
		switch {
		case allDay:
			values = append(values, icsDate(t, tz))
		case zone != "":
			values = append(values, t.In(loadLocationOrUTC(zone)).Format("20060102T150405"))
		default:
			values = append(values, icsTime(t))
		}
	}
	switch {
	case allDay:
		return "EXDATE;VALUE=DATE:" + strings.Join(values, ",")
	case zone != "":
		return "EXDATE;TZID=" + zone + ":" + strings.Join(values, ",")
	}
	return "EXDATE:" + strings.Join(values, ",")
}

// icsRecurrenceID names the occurrence an override replaces. RFC 5545 requires
// it to match the series' DTSTART in value type and zone, whatever the
// override itself has become.
func icsRecurrenceID(x exportEvent) string {
	if x.parentAllDay {
		return "RECURRENCE-ID;VALUE=DATE:" + icsDate(x.originalStart, x.parentTimezone)
	}
	return icsTimeProp("RECURRENCE-ID", x.originalStart, icsZone(x.parentTimezone))
}

// icsZonesUsed returns each TZID the rows reference with the earliest instant
// written in it, which is where its VTIMEZONE has to start.
func icsZonesUsed(rows []exportEvent) map[string]time.Time {
	zones := map[string]time.Time{}
	note := func(zone string, t time.Time) {
		if zone == "" {
			return
		}
		if first, ok := zones[zone]; !ok || t.Before(first) {
			zones[zone] = t
		}
	}
	for _, x := range rows {
		if !x.event.AllDay {
			note(icsZone(x.event.Timezone), x.startAt)
		}
		if x.isOverride && !x.parentAllDay {
			note(icsZone(x.parentTimezone), x.originalStart)
		}
	}
	return zones
}

func buildICS(appName, calName string, rows []exportEvent) string {
	var b strings.Builder
	writeFolded(&b, "BEGIN:VCALENDAR")
	writeFolded(&b, "VERSION:2.0")
	// Escaped like any other TEXT value: the name comes from configuration,
	// and an unescaped separator in it would end the property early and leave
	// the rest of the line to be read as something else.
	writeFolded(&b, "PRODID:-//"+icsEscape(appName)+"//EN")
	writeFolded(&b, "CALSCALE:GREGORIAN")
	writeFolded(&b, "X-WR-CALNAME:"+icsEscape(calName))
	now := time.Now()
	zones := icsZonesUsed(rows)
	zoneNames := make([]string, 0, len(zones))
	for z := range zones {
		zoneNames = append(zoneNames, z)
	}
	sort.Strings(zoneNames)
	for _, z := range zoneNames {
		writeVTimezone(&b, z, zones[z], now)
	}
	stamp := icsTime(now)
	for _, x := range rows {
		e := x.event
		writeFolded(&b, "BEGIN:VEVENT")
		// A changed occurrence shares the series' UID and is told apart by its
		// RECURRENCE-ID. Giving it one of its own would make it a separate
		// event, so a re-import would show it alongside the occurrence it is
		// supposed to replace.
		writeFolded(&b, "UID:"+hex.EncodeToString(e.PublicID)+"@nodate-time")
		writeFolded(&b, "DTSTAMP:"+stamp)
		if e.AllDay {
			writeFolded(&b, "DTSTART;VALUE=DATE:"+icsDate(x.startAt, e.Timezone))
			writeFolded(&b, "DTEND;VALUE=DATE:"+icsDate(x.endAt, e.Timezone))
		} else {
			// Written in the event's own zone, which the VTIMEZONE above
			// defines, so a rule expands where the server expands it.
			zone := icsZone(e.Timezone)
			writeFolded(&b, icsTimeProp("DTSTART", x.startAt, zone))
			writeFolded(&b, icsTimeProp("DTEND", x.endAt, zone))
		}
		if x.isOverride {
			writeFolded(&b, icsRecurrenceID(x))
		}
		if rrule := recurrence.FormatRRule(x.rule, x.startAt.In(loadLocationOrUTC(e.Timezone)), e.AllDay); rrule != "" {
			writeFolded(&b, "RRULE:"+rrule)
			if exdate := icsExdate(x.exceptions, e.AllDay, e.Timezone); exdate != "" {
				writeFolded(&b, exdate)
			}
		}
		writeFolded(&b, "SUMMARY:"+icsEscape(e.Title))
		if e.Location.Valid && e.Location.String != "" {
			writeFolded(&b, "LOCATION:"+icsEscape(e.Location.String))
		}
		if e.Memo.Valid && e.Memo.String != "" {
			writeFolded(&b, "DESCRIPTION:"+icsEscape(e.Memo.String))
		}
		if e.URL.Valid && e.URL.String != "" {
			writeFolded(&b, "URL:"+icsURI(e.URL.String))
		}
		if class := icsClass(e.Visibility); class != "" {
			writeFolded(&b, "CLASS:"+class)
		}
		writeFolded(&b, "TRANSP:"+icsTransp(e.ShowAs))
		if e.NotificationOffset.Valid {
			// The reminder is delivered by whatever client holds the calendar,
			// which is the only thing that can raise one when the app is not
			// open. Leaving it out of the file is what makes a reminder set here
			// silently never arrive.
			writeFolded(&b, "BEGIN:VALARM")
			writeFolded(&b, "ACTION:DISPLAY")
			// DISPLAY alarms require a DESCRIPTION; the event's own title is
			// what the reminder should say.
			writeFolded(&b, "DESCRIPTION:"+icsEscape(e.Title))
			writeFolded(&b, "TRIGGER:"+icsTrigger(e.NotificationOffset.Int32))
			writeFolded(&b, "END:VALARM")
		}
		writeFolded(&b, "END:VEVENT")
	}
	writeFolded(&b, "END:VCALENDAR")
	return b.String()
}

// icsTrigger renders a reminder as a duration relative to the start, which is
// the form every receiving client understands. Minutes are used throughout
// rather than the largest fitting unit: the value is stored in minutes, and a
// unit conversion is one more place for a day to become an hour.
func icsTrigger(minutes int32) string {
	if minutes <= 0 {
		return "PT0S"
	}
	return "-PT" + strconv.Itoa(int(minutes)) + "M"
}

// parseTriggerMinutes reads a TRIGGER value as minutes before the start.
//
// Only a duration relative to the start is usable: an absolute trigger names
// an instant rather than an offset, and one relative to the end has no
// equivalent here. Both are reported as unusable rather than approximated,
// because a reminder that fires at the wrong time is worse than one the
// import says it did not take.
func parseTriggerMinutes(value string, params []string) (int32, bool) {
	for _, p := range params {
		if strings.EqualFold(p, "VALUE=DATE-TIME") || strings.EqualFold(p, "RELATED=END") {
			return 0, false
		}
	}
	value = strings.TrimSpace(strings.ToUpper(value))
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimLeft(value, "+-")
	d, ok := parseICSDuration(value)
	if !ok {
		return 0, false
	}
	if d == 0 {
		return 0, true
	}
	// A trigger after the start would be a reminder for a commitment already
	// under way, which this product has no way to express.
	if !negative {
		return 0, false
	}
	if d%time.Minute != 0 {
		return 0, false
	}
	return int32(d / time.Minute), true
}

// The units a duration may name, in the order the grammar puts them. Weeks
// stand alone; the rest run from days down to seconds, each at most once.
const (
	durNoUnit = iota
	durWeek
	durDay
	durHour
	durMinute
	durSecond
)

// maxICSDuration is the longest span a Duration can hold, which is what bounds
// the arithmetic below.
const maxICSDuration = time.Duration(1<<63 - 1)

// parseICSDuration parses the unsigned part of an RFC 5545 duration.
//
// The grammar is followed rather than approximated: each unit appears at most
// once, in order, weeks combine with nothing, and a value naming no unit at
// all is not a duration. Adding up whatever units a malformed value happens to
// carry would answer a file that asked for no reminder with one -- and a
// reminder at the wrong time is worse than one the import says it did not
// take.
func parseICSDuration(s string) (time.Duration, bool) {
	if !strings.HasPrefix(s, "P") {
		return 0, false
	}
	s = s[1:]
	var total time.Duration
	inTime := false
	num := ""
	last := durNoUnit
	for _, r := range s {
		if r >= '0' && r <= '9' {
			num += string(r)
			continue
		}
		if r == 'T' {
			if num != "" || inTime {
				return 0, false
			}
			inTime = true
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			return 0, false
		}
		num = ""
		var unit int
		var size time.Duration
		switch {
		case r == 'W' && !inTime:
			unit, size = durWeek, 7*24*time.Hour
		case r == 'D' && !inTime:
			unit, size = durDay, 24*time.Hour
		case r == 'H' && inTime:
			unit, size = durHour, time.Hour
		case r == 'M' && inTime:
			unit, size = durMinute, time.Minute
		case r == 'S' && inTime:
			unit, size = durSecond, time.Second
		default:
			return 0, false
		}
		if unit <= last || last == durWeek {
			return 0, false
		}
		last = unit
		// More than a duration can hold is refused rather than allowed to wrap
		// round, which would land on some unrelated offset and read as an
		// interval someone chose.
		if time.Duration(n) > maxICSDuration/size {
			return 0, false
		}
		part := time.Duration(n) * size
		if total > maxICSDuration-part {
			return 0, false
		}
		total += part
	}
	if num != "" || last == durNoUnit {
		return 0, false
	}
	return total, true
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

// icsTransp renders the availability axis. iCalendar only distinguishes taken
// from free, so the two shades in between -- tentative and out of office --
// both read as taken, which is the answer that does not advertise time its
// owner has not offered.
func icsTransp(s generated.CalendarEventsShowAs) string {
	if s == generated.CalendarEventsShowAsFree {
		return "TRANSPARENT"
	}
	return "OPAQUE"
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

// icsClass renders the visibility axis as the property receiving clients
// already understand. Nothing is written for the calendar default, which is
// what an absent CLASS means; an event marked as not for everyone carries the
// marking into the file, so the export does not quietly strip it.
func icsClass(v generated.CalendarEventsVisibility) string {
	switch v {
	case generated.CalendarEventsVisibilityPublic:
		return "PUBLIC"
	case generated.CalendarEventsVisibilityPrivate:
		return "PRIVATE"
	case generated.CalendarEventsVisibilityConfidential:
		return "CONFIDENTIAL"
	default:
		return ""
	}
}

// csvEscape renders one CSV text field.
//
// A spreadsheet reads a cell starting with = + - @ (or a tab or CR ahead of
// one) as a formula, so an event title becomes something that runs on the
// machine of whoever opens the export. Such a field is prefixed with a quote,
// which spreadsheets show as text. Control characters go the way they go in
// the iCalendar export.
func csvEscape(s string) string {
	s = dropForbiddenControl(s)
	if s != "" && strings.IndexByte("=+-@\t\r", s[0]) >= 0 {
		s = "'" + s
	}
	if strings.ContainsAny(s, ",\"\r\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func buildCSV(rows []exportEvent) []byte {
	var b bytes.Buffer
	b.WriteString("Subject,Start Date,Start Time,End Date,End Time,All Day,Location,Description,URL\r\n")
	for _, x := range rows {
		e := x.event
		// Render dates/times in the event's own timezone so all-day events do
		// not shift a day for non-UTC users.
		loc := loadLocationOrUTC(e.Timezone)
		start := x.startAt.In(loc)
		end := x.endAt.In(loc)
		// All-day end is stored exclusively (midnight after the last day); show the
		// inclusive last day so a one-day event reads as the same start/end date and
		// round-trips correctly into spreadsheet/calendar importers.
		endDate := end.Format("2006-01-02")
		if e.AllDay && end.After(start) {
			endDate = end.AddDate(0, 0, -1).Format("2006-01-02")
		}
		fields := []string{
			csvEscape(e.Title),
			start.Format("2006-01-02"),
			start.Format("15:04:05"),
			endDate,
			end.Format("15:04:05"),
			fmt.Sprintf("%t", e.AllDay),
			csvEscape(nullStringValue(e.Location)),
			csvEscape(nullStringValue(e.Memo)),
			csvEscape(nullStringValue(e.URL)),
		}
		b.WriteString(strings.Join(fields, ","))
		b.WriteString("\r\n")
	}
	return b.Bytes()
}

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

type rawEvent struct {
	uid      string
	summary  string
	location string
	desc     string
	url      string
	// class is the CLASS property, which carries the same axis as an event's
	// visibility. An unrecognised value imports as the calendar default.
	class string
	// transp is the TRANSP property, the free/busy half of the availability
	// axis.
	transp string
	start  time.Time
	end    time.Time
	allDay bool
	tzid   string
	// tzUnknown records that a dated property named a zone nothing here could
	// resolve, so its wall clock was read as UTC. The event still imports --
	// there is nothing better to do with it -- but the import says how many it
	// placed that way, because the times are the only thing wrong and nobody
	// checks those.
	tzUnknown bool
	rrule     string
	// exdates are the occurrences the series cancels, distinct and at most
	// importMaxExdates of them; tooManyExdates records that the file named more.
	exdates        []time.Time
	tooManyExdates bool
	// duration is the DURATION property, the alternative to DTEND.
	// badDuration records one that is present but not a duration.
	duration    time.Duration
	hasDuration bool
	badDuration bool
	// recurrenceID names the occurrence this entry replaces; when set the
	// entry is a changed occurrence of the series sharing its uid, not an
	// event of its own.
	recurrenceID    time.Time
	hasRecurrenceID bool
	// alarmMinutes is how long before the start the event's reminder fires,
	// taken from the first VALARM the event can express.
	alarmMinutes *int32
}

// noteUnknownZone records a TZID this server could not resolve. A property
// with no TZID at all is a floating time rather than a zone that failed, so
// there is nothing to report about it.
func (e *rawEvent) noteUnknownZone(tzid string) {
	if tzid == "" {
		return
	}
	if _, ok := resolveTZID(tzid); !ok {
		e.tzUnknown = true
	}
}

// resolveTZID maps a TZID from a file onto an IANA zone name, which is the
// only kind this server can look up. A Windows name -- what Outlook and
// everything speaking to Exchange write -- is mapped onto its IANA equivalent
// rather than being read as UTC.
//
// ok is false when the name means nothing here. The caller then reads the
// value as UTC, which is the only fallback available and the wrong instant for
// every zone that is not UTC, so the import counts how often it had to.
func resolveTZID(tzid string) (string, bool) {
	name := strings.Trim(strings.TrimSpace(tzid), `"`)
	if name == "" {
		return "", false
	}
	// "Local" resolves against whichever machine is running the server, which
	// has nothing to do with the calendar the file came from.
	if strings.EqualFold(name, "Local") {
		return "", false
	}
	if _, err := time.LoadLocation(name); err == nil {
		return name, true
	}
	if iana, ok := windowsZones[name]; ok {
		return iana, true
	}
	return "", false
}

// parseICSTime parses a DTSTART/DTEND value. Wall-clock values carrying a TZID
// are anchored in that zone; UTC values end in Z; floating values (no TZID, no
// Z) are treated as UTC. A TZID nothing can resolve falls back to UTC.
func parseICSTime(value, tzid string, allDay bool) (time.Time, error) {
	// The T and Z separators are read in either case, like the names around
	// them.
	value = strings.ToUpper(strings.TrimSpace(value))
	name, _ := resolveTZID(tzid)
	loc := loadLocationOrUTC(name)
	if allDay {
		return time.ParseInLocation("20060102", value, loc)
	}
	if strings.HasSuffix(value, "Z") {
		return time.Parse("20060102T150405Z", value)
	}
	return time.ParseInLocation("20060102T150405", value, loc)
}

func unfoldICS(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	// A file separated by bare CR is one long line otherwise, so no component
	// in it is ever seen and the whole thing imports as nothing at all.
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\n ", "")
	text = strings.ReplaceAll(text, "\n\t", "")
	return text
}

// containsICSComponent reports whether the body holds a line this parser would
// recognise as opening a component. A body with none imported nothing because
// there was nothing in it to read, which is worth saying: every counter
// reading zero is also what a calendar containing no events answers with.
func containsICSComponent(text string) bool {
	upper := strings.ToUpper(text)
	return strings.Contains(upper, "BEGIN:VCALENDAR") || strings.Contains(upper, "BEGIN:VEVENT")
}

// splitContentLine splits a content line into its name, parameters and value
// per RFC 5545 §3.1: the value starts at the first colon outside a quoted
// parameter value, and parameters split on semicolons outside quotes. ok is
// false for a line with no value at all.
func splitContentLine(line string) (name string, params []string, value string, ok bool) {
	inQuote := false
	segStart := 0
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '"':
			inQuote = !inQuote
		case inQuote:
		case c == ';' || c == ':':
			seg := line[segStart:i]
			if segStart == 0 {
				name = seg
			} else {
				params = append(params, seg)
			}
			segStart = i + 1
			if c == ':' {
				return name, params, line[i+1:], true
			}
		}
	}
	return "", nil, "", false
}

// importMaxExdates bounds the cancellations one imported series may carry.
// Every listing and export of the series reads the whole list, so a file
// naming hundreds of thousands of them is refused rather than stored.
const importMaxExdates = 1000

func parseICS(text string) []rawEvent {
	text = unfoldICS(text)
	lines := strings.Split(text, "\n")
	var events []rawEvent
	var cur *rawEvent
	// exdateSeen dedupes the current event's EXDATE values as they are read,
	// so a repeated value costs nothing and the count is of distinct ones.
	var exdateSeen map[int64]bool
	// A VALARM is a component nested inside the event, and it carries property
	// names the event itself uses. Read flat, its DESCRIPTION would land on the
	// event as its memo, replacing whatever the file said the event was about.
	inAlarm := false
	for _, line := range lines {
		// A byte order mark belongs at the head of the file, but some exporters
		// write one against a component's first line, where it carries no text
		// and keeps the line from matching anything.
		line = strings.TrimPrefix(line, "\ufeff")
		// Component names are case-insensitive like every other name.
		switch {
		case strings.EqualFold(line, "BEGIN:VEVENT"):
			cur = &rawEvent{}
			exdateSeen = map[int64]bool{}
			inAlarm = false
		case strings.EqualFold(line, "END:VEVENT"):
			if cur != nil {
				events = append(events, *cur)
				cur = nil
			}
			inAlarm = false
		case cur != nil && strings.EqualFold(line, "BEGIN:VALARM"):
			inAlarm = true
		case cur != nil && strings.EqualFold(line, "END:VALARM"):
			inAlarm = false
		case cur != nil && inAlarm:
			name, params, value, ok := splitContentLine(line)
			if !ok || !strings.EqualFold(name, "TRIGGER") || cur.alarmMinutes != nil {
				// The first usable alarm wins. An event may carry several, and
				// there is one reminder to keep.
				continue
			}
			if m, ok := parseTriggerMinutes(value, params); ok {
				cur.alarmMinutes = &m
			}
		case cur != nil:
			name, params, value, ok := splitContentLine(line)
			if !ok {
				continue
			}
			// Whatever the file put in the value, what leaves this parser is
			// text. A control character is not: it survives storage and the
			// export writes it back out, handing the next reader a file whose
			// title runs a command in their terminal.
			val := dropForbiddenControl(value)
			key := strings.ToUpper(name)
			isDate := false
			tzid := ""
			for _, p := range params {
				if strings.EqualFold(p, "VALUE=DATE") {
					isDate = true
				}
				if len(p) > 5 && strings.EqualFold(p[:5], "TZID=") {
					tzid = strings.Trim(p[5:], `"`)
				}
			}
			switch key {
			case "UID":
				cur.uid = val
			case "SUMMARY":
				cur.summary = unescapeICS(val)
			case "LOCATION":
				cur.location = unescapeICS(val)
			case "DESCRIPTION":
				cur.desc = truncateRunes(unescapeICS(val), importMaxMemoRunes)
			case "URL":
				cur.url = val
			case "CLASS":
				cur.class = strings.ToUpper(strings.TrimSpace(val))
			case "TRANSP":
				cur.transp = strings.ToUpper(strings.TrimSpace(val))
			case "RRULE":
				cur.rrule = val
			case "DTSTART":
				cur.allDay = isDate
				if tzid != "" {
					cur.tzid = tzid
				}
				cur.noteUnknownZone(tzid)
				if t, err := parseICSTime(val, tzid, isDate); err == nil {
					cur.start = t
				}
			case "DTEND":
				if cur.tzid == "" && tzid != "" {
					cur.tzid = tzid
				}
				cur.noteUnknownZone(tzid)
				if t, err := parseICSTime(val, tzid, isDate); err == nil {
					cur.end = t
				}
			case "DURATION":
				d, ok := parseICSDuration(strings.TrimPrefix(strings.TrimSpace(strings.ToUpper(val)), "+"))
				cur.duration = d
				cur.hasDuration = true
				cur.badDuration = !ok
			case "EXDATE":
				// EXDATE is multi-valued and may appear more than once. Every
				// value names an occurrence the series does not have, so
				// dropping them resurrects occurrences the author cancelled.
				for _, v := range strings.Split(val, ",") {
					if cur.tooManyExdates {
						break
					}
					t, err := parseICSTime(v, tzid, isDate)
					if err != nil {
						continue
					}
					key := t.UnixMilli()
					if exdateSeen[key] {
						continue
					}
					if len(cur.exdates) >= importMaxExdates {
						cur.tooManyExdates = true
						break
					}
					exdateSeen[key] = true
					cur.exdates = append(cur.exdates, t.UTC())
				}
			case "RECURRENCE-ID":
				if t, err := parseICSTime(val, tzid, isDate); err == nil {
					cur.recurrenceID = t.UTC()
					cur.hasRecurrenceID = true
				}
			}
		}
	}
	return events
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

// importMaxMemoRunes is the memo length the events API accepts. A longer
// description is cut to it rather than stored as a memo no edit could save.
const importMaxMemoRunes = 16000

// truncateRunes cuts s to at most n characters, never inside one.
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

func unescapeICS(s string) string {
	r := strings.NewReplacer(
		`\\`, "\\",
		`\;`, ";",
		`\,`, ",",
		`\n`, "\n",
		`\N`, "\n",
	)
	return r.Replace(s)
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
		var changed []rawEvent

		for _, e := range events {
			if e.hasRecurrenceID {
				changed = append(changed, e)
				continue
			}
			series, outcome := importSeriesHead(ctx, deps, cal, userID, e,
				importIdentity(e.uid, seriesByUID))
			count(outcome)
			if outcome != importCreated && outcome != importDuplicate {
				continue
			}
			if outcome == importCreated && e.tzUnknown {
				unknownZones++
			}
			// A recognised series is registered like a written one: its changed
			// occurrences have to find it, and find out that it was left alone.
			if e.uid != "" {
				seriesByUID[e.uid] = series
			}
		}

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
			outcome := importChangedOccurrence(ctx, deps, cal, userID, e, parent)
			count(outcome)
			if outcome == importCreated && e.tzUnknown {
				unknownZones++
			}
		}

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
func importIdentity(uid string, seen map[string]importedSeries) sql.NullString {
	if _, taken := seen[uid]; taken {
		return sql.NullString{}
	}
	return sourceUID(uid)
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
// Each event is its own transaction so one bad row does not discard the whole
// file, and each carries its own log entry: an import is a state change like
// any other, and a feed that skipped it would be missing however many events
// landed.
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
	ctx context.Context, deps Deps, cal generated.Calendar, userID uint32, e rawEvent,
	identity sql.NullString,
) (importedSeries, importOutcome) {
	endAt, outcome, ok := validateImported(e)
	if !ok {
		return importedSeries{}, outcome
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
			return importedSeries{}, importSkipped
		}
		ruleData = converted
		recEnd = sql.NullTime{
			Time:  recurrence.ComputeEndInZone(recurrence.ParseRule(*ruleData), e.start, endAt, tz),
			Valid: true,
		}
		// EXDATE says which occurrences the series does not have. Dropping it
		// hands back every occurrence its author cancelled.
		// The parser already made them distinct.
		column, err := recurrence.Exceptions(e.exdates).MarshalColumn()
		if err != nil {
			return importedSeries{}, importFailed
		}
		exceptions = column
	}
	if recEnd.Valid && !storableInstant(recEnd.Time) {
		return importedSeries{}, importRejected
	}

	// Asked after the event is known to be one this import would write, so an
	// event it was going to skip is reported as skipped rather than as one the
	// calendar already has.
	if identity.Valid {
		existing, err := deps.Queries.FindCalendarEventBySourceUID(ctx,
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
			}, importDuplicate
		case !errors.Is(err, sql.ErrNoRows):
			return importedSeries{}, importFailed
		}
	}

	pubID, err := uuid.NewV7()
	if err != nil {
		return importedSeries{}, importFailed
	}

	var newID uint32
	err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		if err := calresolve.LockShared(ctx, q, cal.ID); err != nil {
			return err
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
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		newID = uint32(id)
		if exceptions != nil {
			if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
				RecurrenceExceptions: exceptions,
				ID:                   newID,
			}); err != nil {
				return err
			}
		}
		return eventlog.Append(ctx, q, eventlog.Entry{
			WorkspaceID: deps.WorkspaceID,
			CalendarID:  cal.ID,
			ActorUserID: userID,
			Type:        eventlog.TypeEventCreated,
			Summary:     e.summary,
			Subject:     pubID[:],
			Extra:       map[string]any{"source": "ics-import"},
		})
	})
	if err != nil {
		return importedSeries{}, storeOutcome(err)
	}
	return importedSeries{
		id: newID, publicID: pubID[:], title: e.summary, start: e.start, end: endAt,
		timezone: tz, rule: ruleData, exceptions: e.exdates,
	}, importCreated
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
	ctx context.Context, deps Deps, cal generated.Calendar, userID uint32,
	e rawEvent, parent importedSeries,
) importOutcome {
	// An override inherits what it does not set, the title included.
	e.summary = cmp.Or(e.summary, parent.title)
	endAt, outcome, ok := validateImported(e)
	if !ok {
		return outcome
	}
	if parent.exceptions.Contains(e.recurrenceID) ||
		!recurrence.OccursAt(recurrence.ParseRule(*parent.rule), parent.start, parent.end,
			parent.timezone, e.recurrenceID) {
		return importSkipped
	}
	overridePubID, err := uuid.NewV7()
	if err != nil {
		return importFailed
	}
	tz := parent.timezone
	if e.tzid != "" {
		tz = importZone(e.tzid)
	}
	parentRef := sql.NullInt32{Int32: int32(parent.id), Valid: true}
	originalRef := sql.NullTime{Time: e.recurrenceID, Valid: true}

	err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		if _, err := calresolve.LockEvent(ctx, q, cal.ID, parent.id); err != nil {
			return err
		}
		_, err := q.UpsertRecurrenceOverride(ctx, generated.UpsertRecurrenceOverrideParams{
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
		})
		if err != nil {
			return err
		}
		// An occurrence moved past the series' end would otherwise fall out
		// of every range query, which selects series by that column.
		if err := q.ExtendRecurrenceEnd(ctx, generated.ExtendRecurrenceEndParams{
			RecurrenceEnd: sql.NullTime{Time: endAt, Valid: true},
			ID:            parent.id,
			Boundary:      sql.NullTime{Time: endAt, Valid: true},
		}); err != nil {
			return err
		}
		// Logged the way a changed occurrence made through the API is: against
		// the series, naming the occurrence.
		return eventlog.Append(ctx, q, eventlog.Entry{
			WorkspaceID: deps.WorkspaceID,
			CalendarID:  cal.ID,
			ActorUserID: userID,
			Type:        eventlog.TypeEventUpdated,
			Summary:     e.summary,
			Subject:     parent.publicID,
			Extra: map[string]any{
				"scope":         "occurrence",
				"originalStart": e.recurrenceID.UTC().Format(time.RFC3339),
				"source":        "ics-import",
			},
		})
	})
	if err != nil {
		return storeOutcome(err)
	}
	return importCreated
}
