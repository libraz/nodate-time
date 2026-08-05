package calendars

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// --- Export ---

type ExportInput struct {
	CalendarID string `path:"calendarId"`
	Format     string `query:"format" enum:"ics,csv" default:"ics"`
	From       string `query:"from" doc:"first day to export (YYYY-MM-DD)" required:"false"`
	To         string `query:"to" doc:"last day to export, inclusive (YYYY-MM-DD)" required:"false"`
}

type ExportOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	// ExportWindow states the range the body actually covers, so a caller can
	// tell a calendar with nothing in a period from an export that stopped
	// short of it. Empty means the whole calendar.
	ExportWindow string `header:"X-Export-Window"`
	Body         []byte
}

// csvWindow bounds the CSV export, which lists one row per occurrence and so
// cannot represent an unbounded series. The iCalendar export carries the rule
// itself and needs no bound.
const (
	csvWindowPast   = -5 * 365 * 24 * time.Hour
	csvWindowFuture = 10 * 365 * 24 * time.Hour
)

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
	// UID and names the occurrence it replaces.
	originalStart time.Time
	isOverride    bool
}

// exportWindow is the range an export covers. A zero bound means unbounded.
type exportWindow struct {
	start time.Time
	end   time.Time
}

func (w exportWindow) header() string {
	if w.start.IsZero() && w.end.IsZero() {
		return "full"
	}
	from, to := "", ""
	if !w.start.IsZero() {
		from = w.start.UTC().Format("2006-01-02")
	}
	if !w.end.IsZero() {
		to = w.end.UTC().Format("2006-01-02")
	}
	return from + "/" + to
}

// parseExportWindow reads the optional from/to parameters. An absent bound
// stays zero, which means "no bound" rather than a default the caller cannot
// see.
func parseExportWindow(from, to string) (exportWindow, error) {
	var w exportWindow
	if from != "" {
		t, err := time.Parse("2006-01-02", from)
		if err != nil {
			return w, err
		}
		w.start = t
	}
	if to != "" {
		t, err := time.Parse("2006-01-02", to)
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

// overlapsWindow reports whether a series head could produce anything inside
// the window. A recurring head is judged by the end of its series, not of its
// first occurrence.
func overlapsWindow(e generated.CalendarEvent, w exportWindow) bool {
	if !w.end.IsZero() && !e.StartAt.Time.Before(w.end) {
		return false
	}
	if w.start.IsZero() {
		return true
	}
	if e.RecurrenceRule != nil {
		return !e.RecurrenceEnd.Valid || e.RecurrenceEnd.Time.After(w.start)
	}
	return e.EndAt.Time.After(w.start)
}

func ExportEvents(deps Deps) func(context.Context, *ExportInput) (*ExportOutput, error) {
	return func(ctx context.Context, in *ExportInput) (*ExportOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		window, err := parseExportWindow(in.From, in.To)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		if in.Format == "csv" {
			return exportCSV(ctx, deps, cal, window)
		}
		return exportICS(ctx, deps, cal, window)
	}
}

// exportICS writes the calendar as iCalendar, series as series.
func exportICS(ctx context.Context, deps Deps, cal generated.Calendar, window exportWindow) (*ExportOutput, error) {
	rows, err := deps.Queries.ListCalendarEventsForExport(ctx, cal.ID)
	if err != nil {
		return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
	}

	exports := make([]exportEvent, 0, len(rows))
	for _, e := range rows {
		// An undated row has nothing to render as DTSTART. The shared schema
		// allows one; this product does not create them, but the export must
		// not invent a zero time if another writer has.
		if !e.StartAt.Valid || !e.EndAt.Valid || !overlapsWindow(e, window) {
			continue
		}
		entry := exportEvent{event: e, startAt: e.StartAt.Time, endAt: e.EndAt.Time}
		if e.RecurrenceRule != nil {
			entry.rule = recurrence.ParseRule(*e.RecurrenceRule)
			entry.exceptions = recurrence.ParseExceptions(e.RecurrenceExceptions)
		}
		exports = append(exports, entry)

		if entry.rule == nil {
			continue
		}
		// A changed occurrence is its own VEVENT under the same UID, so an
		// importing client can tell it apart from the series and put it back
		// where it belongs.
		overrides, err := deps.Queries.ListRecurrenceOverridesByParent(ctx,
			sql.NullInt32{Int32: int32(e.ID), Valid: true})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		for _, child := range overrides {
			if !child.StartAt.Valid || !child.EndAt.Valid || !child.RecurrenceOriginalStart.Valid {
				continue
			}
			if entry.exceptions.Contains(child.RecurrenceOriginalStart.Time) {
				continue
			}
			exports = append(exports, exportEvent{
				// The child's own fields render, but the series' identity and
				// zone are what name the occurrence being replaced.
				event:         child,
				startAt:       child.StartAt.Time,
				endAt:         child.EndAt.Time,
				originalStart: child.RecurrenceOriginalStart.Time,
				isOverride:    true,
				rule:          nil,
			})
			exports[len(exports)-1].event.PublicID = e.PublicID
			exports[len(exports)-1].event.Timezone = e.Timezone
		}
	}

	return &ExportOutput{
		ContentType:        "text/calendar; charset=utf-8",
		ContentDisposition: fmt.Sprintf(`attachment; filename="%s.ics"`, sanitizeFilename(cal.Name)),
		ExportWindow:       window.header(),
		Body:               []byte(buildICS(cal.Name, exports)),
	}, nil
}

// exportCSV writes one row per occurrence, which means a series has to be
// expanded and so bounded. The bound is whatever the caller asked for, and the
// response says what it was.
func exportCSV(ctx context.Context, deps Deps, cal generated.Calendar, window exportWindow) (*ExportOutput, error) {
	if window.start.IsZero() {
		window.start = time.Now().Add(csvWindowPast)
	}
	if window.end.IsZero() {
		window.end = time.Now().Add(csvWindowFuture)
	}

	rows, err := deps.Queries.ListCalendarEventsByCalendarAndRange(ctx, generated.ListCalendarEventsByCalendarAndRangeParams{
		CalendarID: cal.ID,
		RangeEnd:   sql.NullTime{Time: window.end, Valid: true},
		RangeStart: sql.NullTime{Time: window.start, Valid: true},
	})
	if err != nil {
		return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
	}

	exports := make([]exportEvent, 0, len(rows))
	for _, e := range rows {
		if !e.StartAt.Valid || !e.EndAt.Valid {
			continue
		}
		exports = append(exports, exportEvent{event: e, startAt: e.StartAt.Time, endAt: e.EndAt.Time})
	}

	recurringRows, err := deps.Queries.ListRecurringCalendarEventsByCalendarAndRange(ctx, generated.ListRecurringCalendarEventsByCalendarAndRangeParams{
		CalendarID: cal.ID,
		RangeEnd:   sql.NullTime{Time: window.end, Valid: true},
		RangeStart: sql.NullTime{Time: window.start, Valid: true},
	})
	if err != nil {
		return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
	}
	// Expansion goes through eventexpand so both departures from the rule are
	// honored: cancelled occurrences stay out and changed ones carry their
	// override's values, matching what the app displays.
	for _, e := range recurringRows {
		if e.RecurrenceRule == nil {
			continue
		}
		for _, inst := range eventexpand.ExpandRecurringEvent(ctx, deps.Queries, e, window.start, window.end) {
			if inst.IsOverride {
				if !inst.Event.StartAt.Valid || !inst.Event.EndAt.Valid {
					continue
				}
				exports = append(exports, exportEvent{
					event:   inst.Event,
					startAt: inst.Event.StartAt.Time,
					endAt:   inst.Event.EndAt.Time,
				})
				continue
			}
			exports = append(exports, exportEvent{
				event:   e,
				startAt: inst.Occurrence.StartAt,
				endAt:   inst.Occurrence.EndAt,
			})
		}
	}

	return &ExportOutput{
		ContentType:        "text/csv; charset=utf-8",
		ContentDisposition: fmt.Sprintf(`attachment; filename="%s.csv"`, sanitizeFilename(cal.Name)),
		ExportWindow:       window.header(),
		Body:               []byte(buildCSV(exports)),
	}, nil
}

func sanitizeFilename(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", "\"", "_", " ", "_")
	return r.Replace(s)
}

func icsEscape(s string) string {
	// Normalize all newline variants to a single \n before escaping so bare CR
	// and CRLF do not leak raw control characters into the output.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
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
	return s
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
// Continuation lines start with a single space.
func writeFolded(b *strings.Builder, line string) {
	const limit = 75
	bs := []byte(line)
	if len(bs) <= limit {
		b.Write(bs)
		b.WriteString("\r\n")
		return
	}
	// First chunk
	b.Write(bs[:limit])
	b.WriteString("\r\n")
	bs = bs[limit:]
	// 74 octets per continuation (1 reserved for the leading space).
	const cont = 74
	for len(bs) > 0 {
		n := cont
		if n > len(bs) {
			n = len(bs)
		}
		b.WriteByte(' ')
		b.Write(bs[:n])
		b.WriteString("\r\n")
		bs = bs[n:]
	}
}

// icsRRule renders an internal recurrence rule as an RFC 5545 RRULE value.
//
// WKST is always written out: the expander is fixed to a Sunday week start,
// which differs from the RFC default, and a weekly rule with an interval
// would land on different days in a client that assumed Monday.
func icsRRule(r *recurrence.Rule, allDay bool, tz string) string {
	if r == nil || r.Freq == "" {
		return ""
	}
	parts := []string{"FREQ=" + strings.ToUpper(r.Freq)}
	if r.Interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(r.Interval))
	}
	if len(r.ByDay) > 0 {
		parts = append(parts, "BYDAY="+strings.Join(r.ByDay, ","))
	}
	if r.ByMonthDay > 0 {
		parts = append(parts, "BYMONTHDAY="+strconv.Itoa(r.ByMonthDay))
	}
	if r.BySetPos > 0 {
		parts = append(parts, "BYSETPOS="+strconv.Itoa(r.BySetPos))
	}
	if r.Count > 0 {
		parts = append(parts, "COUNT="+strconv.Itoa(r.Count))
	}
	if until := icsUntil(r.Until, allDay, tz); until != "" {
		parts = append(parts, "UNTIL="+until)
	}
	parts = append(parts, "WKST=SU")
	return strings.Join(parts, ";")
}

// icsUntil renders the rule's end boundary in the value type DTSTART uses: a
// date for an all-day series, a UTC instant for a timed one. Mixing the two
// is what makes some clients drop the boundary and repeat forever.
func icsUntil(until *string, allDay bool, tz string) string {
	if until == nil || *until == "" {
		return ""
	}
	loc := loadLocationOrUTC(tz)
	if t, err := time.Parse(time.RFC3339, *until); err == nil {
		if allDay {
			return t.In(loc).Format("20060102")
		}
		return icsTime(t)
	}
	if t, err := time.ParseInLocation("2006-01-02", *until, loc); err == nil {
		if allDay {
			return t.Format("20060102")
		}
		return icsTime(t.AddDate(0, 0, 1).Add(-time.Second))
	}
	return ""
}

// icsExdate renders the series' cancellations. They are stored as the instants
// the occurrences would have started at, which is exactly what EXDATE names.
func icsExdate(ex recurrence.Exceptions, allDay bool, tz string) string {
	if len(ex) == 0 {
		return ""
	}
	values := make([]string, 0, len(ex))
	for _, t := range ex {
		if allDay {
			values = append(values, icsDate(t, tz))
			continue
		}
		values = append(values, icsTime(t))
	}
	if allDay {
		return "EXDATE;VALUE=DATE:" + strings.Join(values, ",")
	}
	return "EXDATE:" + strings.Join(values, ",")
}

func buildICS(calName string, rows []exportEvent) string {
	var b strings.Builder
	writeFolded(&b, "BEGIN:VCALENDAR")
	writeFolded(&b, "VERSION:2.0")
	writeFolded(&b, "PRODID:-//Nodate Time//EN")
	writeFolded(&b, "CALSCALE:GREGORIAN")
	writeFolded(&b, "X-WR-CALNAME:"+icsEscape(calName))
	stamp := icsTime(time.Now())
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
			if x.isOverride {
				writeFolded(&b, "RECURRENCE-ID;VALUE=DATE:"+icsDate(x.originalStart, e.Timezone))
			}
		} else {
			// Timed values are normalized to UTC (Z suffix). Referencing a
			// TZID without emitting a matching VTIMEZONE component violates
			// RFC 5545 and makes some clients treat the times as floating,
			// so the display-zone hint is intentionally dropped.
			writeFolded(&b, "DTSTART:"+icsTime(x.startAt))
			writeFolded(&b, "DTEND:"+icsTime(x.endAt))
			if x.isOverride {
				writeFolded(&b, "RECURRENCE-ID:"+icsTime(x.originalStart))
			}
		}
		if rrule := icsRRule(x.rule, e.AllDay, e.Timezone); rrule != "" {
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
		writeFolded(&b, "END:VEVENT")
	}
	writeFolded(&b, "END:VCALENDAR")
	return b.String()
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func buildCSV(rows []exportEvent) string {
	var b strings.Builder
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
	return b.String()
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
		// Truncated is how many events past the per-file limit were never
		// looked at. It is reported separately from Skipped because it is the
		// one outcome the file itself cannot explain.
		Truncated int `json:"truncated"`
	}
}

type rawEvent struct {
	uid      string
	summary  string
	location string
	desc     string
	url      string
	start    time.Time
	end      time.Time
	allDay   bool
	tzid     string
	rrule    string
	// exdates are the occurrences the series cancels.
	exdates []time.Time
	// recurrenceID names the occurrence this entry replaces; when set the
	// entry is a changed occurrence of the series sharing its uid, not an
	// event of its own.
	recurrenceID    time.Time
	hasRecurrenceID bool
}

// parseICSTime parses a DTSTART/DTEND value. Wall-clock values carrying a TZID
// are anchored in that zone; UTC values end in Z; floating values (no TZID, no
// Z) are treated as UTC. An unknown TZID falls back to UTC.
func parseICSTime(value, tzid string, allDay bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	loc := loadLocationOrUTC(tzid)
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
	text = strings.ReplaceAll(text, "\n ", "")
	text = strings.ReplaceAll(text, "\n\t", "")
	return text
}

func parseICS(text string) []rawEvent {
	text = unfoldICS(text)
	lines := strings.Split(text, "\n")
	var events []rawEvent
	var cur *rawEvent
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "BEGIN:VEVENT":
			cur = &rawEvent{}
		case line == "END:VEVENT":
			if cur != nil {
				events = append(events, *cur)
				cur = nil
			}
		case cur != nil:
			colon := strings.Index(line, ":")
			if colon < 0 {
				continue
			}
			rawKey := line[:colon]
			val := line[colon+1:]
			parts := strings.Split(rawKey, ";")
			key := strings.ToUpper(parts[0])
			isDate := false
			tzid := ""
			for _, p := range parts[1:] {
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
				cur.desc = unescapeICS(val)
			case "URL":
				cur.url = val
			case "RRULE":
				cur.rrule = val
			case "DTSTART":
				cur.allDay = isDate
				if tzid != "" {
					cur.tzid = tzid
				}
				if t, err := parseICSTime(val, tzid, isDate); err == nil {
					cur.start = t
				}
			case "DTEND":
				if cur.tzid == "" && tzid != "" {
					cur.tzid = tzid
				}
				if t, err := parseICSTime(val, tzid, isDate); err == nil {
					cur.end = t
				}
			case "EXDATE":
				// EXDATE is multi-valued and may appear more than once. Every
				// value names an occurrence the series does not have, so
				// dropping them resurrects occurrences the author cancelled.
				for _, v := range strings.Split(val, ",") {
					if t, err := parseICSTime(v, tzid, isDate); err == nil {
						cur.exdates = append(cur.exdates, t.UTC())
					}
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

var icalWeekdays = map[string]bool{
	"SU": true, "MO": true, "TU": true, "WE": true, "TH": true, "FR": true, "SA": true,
}

// convertRRuleUntil maps an RRULE UNTIL value onto the internal rule's until
// string: dates stay date-only, instants become RFC 3339. A local-time UNTIL
// (no Z) is anchored in the event's zone.
func convertRRuleUntil(val string, loc *time.Location) (string, bool) {
	if t, err := time.Parse("20060102T150405Z", val); err == nil {
		return t.UTC().Format(time.RFC3339), true
	}
	if t, err := time.ParseInLocation("20060102T150405", val, loc); err == nil {
		return t.Format(time.RFC3339), true
	}
	if _, err := time.Parse("20060102", val); err == nil {
		return val[:4] + "-" + val[4:6] + "-" + val[6:8], true
	}
	return "", false
}

// convertRRule maps an RFC 5545 RRULE value onto the app's internal recurrence
// rule JSON. It supports the subset the expander implements (FREQ, INTERVAL,
// COUNT, UNTIL, BYDAY, BYMONTHDAY, BYSETPOS); anything else returns false so
// the caller can skip the event instead of silently importing a single
// occurrence.
func convertRRule(value string, loc *time.Location) (*json.RawMessage, bool) {
	rule := recurrence.Rule{Interval: 1}
	wkst := ""
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.Index(part, "=")
		if eq < 0 {
			return nil, false
		}
		key := strings.ToUpper(part[:eq])
		val := part[eq+1:]
		switch key {
		case "FREQ":
			switch strings.ToUpper(val) {
			case "DAILY":
				rule.Freq = "daily"
			case "WEEKLY":
				rule.Freq = "weekly"
			case "MONTHLY":
				rule.Freq = "monthly"
			case "YEARLY":
				rule.Freq = "yearly"
			default:
				return nil, false
			}
		case "INTERVAL":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > 999 {
				return nil, false
			}
			rule.Interval = n
		case "COUNT":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > 1000 {
				return nil, false
			}
			rule.Count = n
		case "UNTIL":
			until, ok := convertRRuleUntil(val, loc)
			if !ok {
				return nil, false
			}
			rule.Until = &until
		case "BYDAY":
			for _, d := range strings.Split(val, ",") {
				d = strings.ToUpper(strings.TrimSpace(d))
				// Ordinal prefixes such as 2MO or -1FR are not supported.
				if !icalWeekdays[d] {
					return nil, false
				}
				rule.ByDay = append(rule.ByDay, d)
			}
		case "BYMONTHDAY":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > 31 {
				return nil, false
			}
			rule.ByMonthDay = n
		case "BYSETPOS":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > 5 {
				return nil, false
			}
			rule.BySetPos = n
		case "WKST":
			wkst = strings.ToUpper(val)
		default:
			return nil, false
		}
	}
	if rule.Freq == "" {
		return nil, false
	}
	// The expander is fixed to WKST=SU; a differing week start only changes the
	// result for weekly byDay rules with interval > 1.
	if wkst != "" && wkst != "SU" && rule.Freq == "weekly" && rule.Interval > 1 && len(rule.ByDay) > 0 {
		return nil, false
	}
	// The expander treats monthly byDay without bySetPos as ambiguous.
	if rule.Freq == "monthly" && len(rule.ByDay) > 0 && rule.BySetPos == 0 {
		return nil, false
	}
	data, err := json.Marshal(rule)
	if err != nil {
		return nil, false
	}
	raw := json.RawMessage(data)
	return &raw, true
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

		var imported, skipped, failed int
		// Series heads are written first: a changed occurrence names the one
		// it belongs to by UID, and the file may put it either side of it.
		seriesByUID := map[string]importedSeries{}
		var changed []rawEvent

		for _, e := range events {
			if e.hasRecurrenceID {
				changed = append(changed, e)
				continue
			}
			series, outcome := importSeriesHead(ctx, deps, cal, userID, e)
			switch outcome {
			case importSkipped:
				skipped++
			case importFailed:
				failed++
			default:
				imported++
				if e.uid != "" {
					seriesByUID[e.uid] = series
				}
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
			if err := importChangedOccurrence(ctx, deps, cal, userID, e, parent); err != nil {
				failed++
				continue
			}
			imported++
		}

		out := &ImportOutput{}
		out.Body.Imported = imported
		out.Body.Skipped = skipped
		out.Body.Failed = failed
		out.Body.Truncated = truncated
		return out, nil
	}
}

// importedSeries is what a changed occurrence needs to attach itself to the
// series it belongs to.
type importedSeries struct {
	id       uint32
	timezone string
	rule     *json.RawMessage
}

type importOutcome int

const (
	importCreated importOutcome = iota
	importSkipped
	importFailed
)

// importZone keeps the source zone so wall-clock semantics (recurrence,
// all-day rendering) survive the import. Unknown TZIDs fall back to UTC to
// match how the times were parsed.
func importZone(tzid string) string {
	if tzid != "" {
		if _, err := time.LoadLocation(tzid); err == nil {
			return tzid
		}
	}
	return "UTC"
}

func importEndAt(e rawEvent) time.Time {
	if !e.end.IsZero() {
		return e.end
	}
	if e.allDay {
		return e.start.AddDate(0, 0, 1)
	}
	return e.start.Add(time.Hour)
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
func importSeriesHead(
	ctx context.Context, deps Deps, cal generated.Calendar, userID uint32, e rawEvent,
) (importedSeries, importOutcome) {
	if e.summary == "" || e.start.IsZero() {
		return importedSeries{}, importSkipped
	}
	endAt := importEndAt(e)
	tz := importZone(e.tzid)

	var ruleData *json.RawMessage
	var exceptions *json.RawMessage
	recEnd := sql.NullTime{}
	if e.rrule != "" {
		converted, ok := convertRRule(e.rrule, loadLocationOrUTC(tz))
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
		var ex recurrence.Exceptions
		for _, d := range e.exdates {
			ex = ex.With(d)
		}
		column, err := ex.MarshalColumn()
		if err != nil {
			return importedSeries{}, importFailed
		}
		exceptions = column
	}

	pubID, err := uuid.NewV7()
	if err != nil {
		return importedSeries{}, importFailed
	}

	var newID uint32
	err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		res, err := q.CreateCalendarEvent(ctx, generated.CreateCalendarEventParams{
			PublicID:        pubID[:],
			WorkspaceID:     deps.WorkspaceID,
			CalendarID:      cal.ID,
			Kind:            generated.CalendarEventsKindEvent,
			Visibility:      generated.CalendarEventsVisibilityDefault,
			ShowAs:          generated.CalendarEventsShowAsBusy,
			Flexibility:     generated.CalendarEventsFlexibilityFixed,
			Title:           e.summary,
			AllDay:          e.allDay,
			StartAt:         sql.NullTime{Time: e.start, Valid: true},
			EndAt:           sql.NullTime{Time: endAt, Valid: true},
			Timezone:        tz,
			Location:        nullString(e.location),
			Memo:            nullString(e.desc),
			URL:             nullString(e.url),
			OwnerUserID:     userID,
			CreatedByUserID: userID,
			RecurrenceRule:  ruleData,
			RecurrenceEnd:   recEnd,
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
		return importedSeries{}, importFailed
	}
	return importedSeries{id: newID, timezone: tz, rule: ruleData}, importCreated
}

// importChangedOccurrence writes a VEVENT carrying a RECURRENCE-ID as an
// override of the series it names, which is how the app stores a changed
// occurrence. Written as a free-standing event it would show up beside the
// occurrence it was meant to replace.
func importChangedOccurrence(
	ctx context.Context, deps Deps, cal generated.Calendar, userID uint32,
	e rawEvent, parent importedSeries,
) error {
	if e.start.IsZero() {
		return fmt.Errorf("changed occurrence has no start")
	}
	overridePubID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	tz := parent.timezone
	if e.tzid != "" {
		tz = importZone(e.tzid)
	}
	parentRef := sql.NullInt32{Int32: int32(parent.id), Valid: true}
	originalRef := sql.NullTime{Time: e.recurrenceID, Valid: true}

	return dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		_, err := q.UpsertRecurrenceOverride(ctx, generated.UpsertRecurrenceOverrideParams{
			PublicID:                overridePubID[:],
			WorkspaceID:             deps.WorkspaceID,
			CalendarID:              cal.ID,
			Kind:                    generated.CalendarEventsKindEvent,
			Visibility:              generated.CalendarEventsVisibilityDefault,
			ShowAs:                  generated.CalendarEventsShowAsBusy,
			Flexibility:             generated.CalendarEventsFlexibilityFixed,
			Title:                   e.summary,
			AllDay:                  e.allDay,
			StartAt:                 sql.NullTime{Time: e.start, Valid: true},
			EndAt:                   sql.NullTime{Time: importEndAt(e), Valid: true},
			Timezone:                tz,
			Location:                nullString(e.location),
			Memo:                    nullString(e.desc),
			URL:                     nullString(e.url),
			OwnerUserID:             userID,
			CreatedByUserID:         userID,
			RecurrenceParentID:      parentRef,
			RecurrenceOriginalStart: originalRef,
		})
		return err
	})
}
