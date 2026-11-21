package calendars

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/http/daterange"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
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

// exportMaxEntries bounds one export: VEVENTs for iCalendar, rows for CSV. A
// CSV series expands per occurrence and nothing bounds how many series a
// calendar holds, so the bound is on the output. A range holding more is
// refused rather than cut short.
const exportMaxEntries = 20000

// attendeeLoadChunk is how many events one attendee query names.
const attendeeLoadChunk = 5000

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
	exceptions recurrence.ExceptionList
	// originalStart is set for a changed occurrence, which shares the series'
	// UID and names the occurrence it replaces. RECURRENCE-ID has to take the
	// series' value type and zone rather than the override's own, so those
	// are carried alongside.
	originalStart  time.Time
	isOverride     bool
	parentAllDay   bool
	parentTimezone string
	// masked is set when the reader sees the event with its details withheld:
	// the title and times leave, the location, description and URL do not.
	masked bool
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
			return nil, apierrors.Respond(ctx, err)
		}

		loc := daterange.Location(in.TZ, actorTimezone(ctx, deps, userID))
		window, err := parseExportWindow(in.From, in.To, loc)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		viewer := exportViewer{userID: userID, calendarDefault: eventacl.Visibility(cal.DefaultEventVisibility)}
		if in.Format == "csv" {
			return exportCSV(ctx, deps, cal, viewer, window)
		}
		return exportICS(ctx, deps, cal, viewer, window)
	}
}

// exportViewer is the member an export is for. What each row shows of itself
// follows from the same rule that decides what a listing shows them.
type exportViewer struct {
	userID          uint32
	calendarDefault eventacl.Visibility
}

func (v exportViewer) decide(e generated.CalendarEvent, attendees []uint32) eventacl.Decision {
	return eventacl.Decide(eventacl.Event{
		Visibility:      eventacl.Visibility(e.Visibility),
		CalendarDefault: v.calendarDefault,
		OwnerUserID:     e.OwnerUserID,
		CreatedByUserID: e.CreatedByUserID,
	}, eventacl.Reader{UserID: v.userID, IsMember: true}, attendees)
}

// exportAttendees holds the attendees of the rows an export reads, so every
// occurrence is judged without a query of its own.
type exportAttendees struct {
	byEvent  map[uint32][]eventacl.Attendee
	withRows map[uint32]bool
}

func loadExportAttendees(ctx context.Context, q *generated.Queries, ids []uint32) (exportAttendees, error) {
	out := exportAttendees{byEvent: map[uint32][]eventacl.Attendee{}, withRows: map[uint32]bool{}}
	for lo := 0; lo < len(ids); lo += attendeeLoadChunk {
		keys := make([]sql.NullInt32, 0, attendeeLoadChunk)
		for _, id := range ids[lo:min(lo+attendeeLoadChunk, len(ids))] {
			keys = append(keys, sql.NullInt32{Int32: int32(id), Valid: true})
		}
		rows, err := q.ListEventAttendeesByEvents(ctx, keys)
		if err != nil {
			return out, err
		}
		for _, r := range rows {
			out.byEvent[uint32(r.EventID.Int32)] = append(out.byEvent[uint32(r.EventID.Int32)],
				eventacl.Attendee{UserID: r.UserID, Enabled: true})
		}
		held, err := q.ListEventIDsWithAttendeeRows(ctx, keys)
		if err != nil {
			return out, err
		}
		for _, id := range held {
			out.withRows[uint32(id.Int32)] = true
		}
	}
	return out, nil
}

// of returns who attends an occurrence: an override that has attendee rows of
// its own replaces the series' attendees, one with none inherits them.
func (a exportAttendees) of(series uint32, override *generated.CalendarEvent) []uint32 {
	var replacement []eventacl.Attendee
	if override != nil && a.withRows[override.ID] {
		replacement = a.byEvent[override.ID]
		if len(replacement) == 0 {
			replacement = []eventacl.Attendee{{}}
		}
	}
	return eventacl.OccurrenceAttendees(a.byEvent[series], replacement)
}

// loadExportEvents reads what an export renders. Every series' changed
// occurrences come from one query, rows the reader may not see are left out,
// and a range holding more than exportMaxEntries is refused.
//
// With expand set each series becomes its occurrences inside the window (CSV);
// otherwise it stays one entry carrying its rule, followed by its changed
// occurrences (iCalendar).
func loadExportEvents(
	ctx context.Context, deps Deps, cal generated.Calendar, viewer exportViewer, window exportWindow, expand bool,
) ([]exportEvent, error) {
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
			return nil, err
		}
		series, err := deps.Queries.ListRecurringCalendarEventsByCalendarAndRange(ctx, generated.ListRecurringCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   rangeEnd,
			RangeStart: rangeStart,
		})
		if err != nil {
			return nil, err
		}
		heads = append(plain, series...)
	} else {
		rows, err := deps.Queries.ListCalendarEventsForExport(ctx, cal.ID)
		if err != nil {
			return nil, err
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
			return nil, err
		}
		for _, r := range rows {
			parent := uint32(r.RecurrenceParentID.Int32)
			overrides[parent] = append(overrides[parent], r)
		}
	}

	ids := make([]uint32, 0, len(heads))
	for _, e := range heads {
		ids = append(ids, e.ID)
		for _, child := range overrides[e.ID] {
			ids = append(ids, child.ID)
		}
	}
	attendees, err := loadExportAttendees(ctx, deps.Queries, ids)
	if err != nil {
		return nil, err
	}

	var entries []exportEvent
	// add keeps x if the reader may see it. d is the decision for the row x
	// stands for.
	add := func(x exportEvent, d eventacl.Decision) error {
		if !d.Exists {
			return nil
		}
		if len(entries) >= exportMaxEntries {
			return apierrors.ExportRangeTooLarge
		}
		x.masked = d.DetailsHidden
		entries = append(entries, x)
		return nil
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
				var override *generated.CalendarEvent
				if inst.IsOverride {
					if !inst.Event.StartAt.Valid || !inst.Event.EndAt.Valid {
						continue
					}
					x = exportEvent{event: inst.Event, startAt: inst.Event.StartAt.Time, endAt: inst.Event.EndAt.Time}
					override = &inst.Event
				}
				if err := add(x, viewer.decide(x.event, attendees.of(e.ID, override))); err != nil {
					return nil, err
				}
			}
			continue
		}

		entry := exportEvent{event: e, startAt: e.StartAt.Time, endAt: e.EndAt.Time}
		if !expand && e.RecurrenceRule != nil {
			rule, err := recurrence.ParseRule(*e.RecurrenceRule)
			if err != nil {
				return nil, err
			}
			entry.rule = rule
			entry.exceptions = recurrence.ParseExceptionList(e.RecurrenceExceptions)
		}
		if err := add(entry, viewer.decide(e, attendees.of(e.ID, nil))); err != nil {
			return nil, err
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
			if entry.exceptions.Cancels(child.RecurrenceOriginalStart.Time, e.AllDay, e.Timezone) {
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
			decision := viewer.decide(child, attendees.of(e.ID, &child))
			x.event.PublicID = e.PublicID
			if err := add(x, decision); err != nil {
				return nil, err
			}
		}
	}
	return entries, nil
}

// exportICS writes the calendar as iCalendar, series as series.
func exportICS(ctx context.Context, deps Deps, cal generated.Calendar, viewer exportViewer, window exportWindow) (*ExportOutput, error) {
	exports, err := loadExportEvents(ctx, deps, cal, viewer, window, false)
	if err != nil {
		return nil, apierrors.Respond(ctx, err)
	}
	return &ExportOutput{
		ContentType:        "text/calendar; charset=utf-8",
		ContentDisposition: exportDisposition(cal.Name, "ics"),
		ExportWindow:       window.header(),
		Body:               []byte(buildICS(deps.AppName, cal.Name, exports)),
	}, nil
}

// exportCSV writes one row per occurrence, which means a series has to be
// expanded and so bounded. The bound is whatever the caller asked for, and the
// response says what it was.
func exportCSV(ctx context.Context, deps Deps, cal generated.Calendar, viewer exportViewer, window exportWindow) (*ExportOutput, error) {
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

	exports, err := loadExportEvents(ctx, deps, cal, viewer, window, true)
	if err != nil {
		return nil, apierrors.Respond(ctx, err)
	}
	return &ExportOutput{
		ContentType:        "text/csv; charset=utf-8",
		ContentDisposition: exportDisposition(cal.Name, "csv"),
		ExportWindow:       window.header(),
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

// icsDate renders an all-day bound as its date. An all-day event is dates, not
// instants: the UTC date of its bound is the date, whatever zone it records.
func icsDate(t time.Time) string {
	return t.UTC().Format("20060102")
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

// icsExdate renders the series' cancellations as EXDATE, in the value type and
// zone DTSTART uses. start is the series' first occurrence. An element names
// an occurrence in the form it was written in; one nothing here can read is
// left out of the file, though it stays in the stored list.
func icsExdate(ex recurrence.ExceptionList, allDay bool, tz string, start time.Time) string {
	zone := icsZone(tz)
	loc := loadLocationOrUTC(zone)
	clock := start.In(loc)
	var values []string
	for _, el := range ex {
		var s string
		if json.Unmarshal(el, &s) != nil {
			continue
		}
		date, isDate := parseExceptionDate(s)
		instant, instantErr := time.Parse(time.RFC3339Nano, s)
		floating, floatingErr := time.ParseInLocation("2006-01-02T15:04:05", s, loc)
		switch {
		case allDay && isDate:
			values = append(values, icsDate(date))
		case allDay && instantErr == nil:
			values = append(values, icsDate(instant))
		case allDay && floatingErr == nil:
			values = append(values, floating.Format("20060102"))
		case allDay:
		case isDate:
			values = append(values, exdateClock(time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), clock.Second(), 0, loc), zone))
		case instantErr == nil:
			values = append(values, exdateClock(instant, zone))
		case floatingErr == nil:
			values = append(values, exdateClock(floating, zone))
		}
	}
	if len(values) == 0 {
		return ""
	}
	switch {
	case allDay:
		return "EXDATE;VALUE=DATE:" + strings.Join(values, ",")
	case zone != "":
		return "EXDATE;TZID=" + zone + ":" + strings.Join(values, ",")
	}
	return "EXDATE:" + strings.Join(values, ",")
}

// parseExceptionDate reads a YYYY-MM-DD element.
func parseExceptionDate(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// exdateClock renders one cancelled instant as a wall clock in zone, or as a
// UTC instant when the series has no zone.
func exdateClock(t time.Time, zone string) string {
	if zone != "" {
		return t.In(loadLocationOrUTC(zone)).Format("20060102T150405")
	}
	return icsTime(t)
}

// icsRecurrenceID names the occurrence an override replaces. RFC 5545 requires
// it to match the series' DTSTART in value type and zone, whatever the
// override itself has become.
func icsRecurrenceID(x exportEvent) string {
	if x.parentAllDay {
		return "RECURRENCE-ID;VALUE=DATE:" + icsDate(x.originalStart)
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
			writeFolded(&b, "DTSTART;VALUE=DATE:"+icsDate(x.startAt))
			writeFolded(&b, "DTEND;VALUE=DATE:"+icsDate(x.endAt))
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
		if rrule := recurrence.FormatRRule(x.rule, x.startAt.In(loadLocationOrUTC(recurrence.ExpansionZone(e.AllDay, e.Timezone))), e.AllDay); rrule != "" {
			writeFolded(&b, "RRULE:"+rrule)
			if exdate := icsExdate(x.exceptions, e.AllDay, e.Timezone, x.startAt); exdate != "" {
				writeFolded(&b, exdate)
			}
		}
		writeFolded(&b, "SUMMARY:"+icsEscape(e.Title))
		if !x.masked {
			if e.Location.Valid && e.Location.String != "" {
				writeFolded(&b, "LOCATION:"+icsEscape(e.Location.String))
			}
			if e.Memo.Valid && e.Memo.String != "" {
				writeFolded(&b, "DESCRIPTION:"+icsEscape(e.Memo.String))
			}
			if e.URL.Valid && e.URL.String != "" {
				writeFolded(&b, "URL:"+icsURI(e.URL.String))
			}
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
		// Timed events render in their own zone; an all-day event is dates, so
		// its bounds are read as UTC dates.
		loc := loadLocationOrUTC(e.Timezone)
		if e.AllDay {
			loc = time.UTC
		}
		start := x.startAt.In(loc)
		end := x.endAt.In(loc)
		// All-day end is stored exclusively (midnight after the last day); show the
		// inclusive last day so a one-day event reads as the same start/end date and
		// round-trips correctly into spreadsheet/calendar importers.
		endDate := end.Format("2006-01-02")
		if e.AllDay && end.After(start) {
			endDate = end.AddDate(0, 0, -1).Format("2006-01-02")
		}
		location, memo, url := nullStringValue(e.Location), nullStringValue(e.Memo), nullStringValue(e.URL)
		if x.masked {
			location, memo, url = "", "", ""
		}
		fields := []string{
			csvEscape(e.Title),
			start.Format("2006-01-02"),
			start.Format("15:04:05"),
			endDate,
			end.Format("15:04:05"),
			fmt.Sprintf("%t", e.AllDay),
			csvEscape(location),
			csvEscape(memo),
			csvEscape(url),
		}
		b.WriteString(strings.Join(fields, ","))
		b.WriteString("\r\n")
	}
	return b.Bytes()
}
