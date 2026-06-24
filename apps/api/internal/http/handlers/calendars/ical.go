package calendars

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// --- Export ---

type ExportInput struct {
	CalendarID string `path:"calendarId"`
	Format     string `query:"format" enum:"ics,csv" default:"ics"`
}

type ExportOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

// Export window — all events whose [start_at, end_at] overlaps [windowStart, windowEnd].
const (
	exportWindowPast   = -5 * 365 * 24 * time.Hour
	exportWindowFuture = 10 * 365 * 24 * time.Hour
)

// exportEvent is a single event instance to render. For non-recurring events it
// mirrors the stored row; for recurring masters it carries one expanded
// occurrence's start/end plus a uidSuffix to keep the UID unique per instance.
type exportEvent struct {
	event     generated.Event
	startAt   time.Time
	endAt     time.Time
	uidSuffix string
}

func ExportEvents(deps Deps) func(context.Context, *ExportInput) (*ExportOutput, error) {
	return func(ctx context.Context, in *ExportInput) (*ExportOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		now := time.Now()
		windowStart := now.Add(exportWindowPast)
		windowEnd := now.Add(exportWindowFuture)

		// sqlc generates StartAt/EndAt for the bound params, but the underlying SQL is
		//   `start_at < ? AND end_at > ?`
		// so StartAt actually receives the upper bound (windowEnd) and EndAt the lower
		// bound (windowStart). See sql/queries/events.sql.
		rows, err := deps.Queries.ListEventsByCalendarAndRange(ctx, generated.ListEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			StartAt:    windowEnd,
			EndAt:      windowStart,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		exports := make([]exportEvent, 0, len(rows))
		for _, e := range rows {
			exports = append(exports, exportEvent{event: e, startAt: e.StartAt, endAt: e.EndAt})
		}

		// Recurring masters are stored once; expand each into its occurrences
		// within the export window so they are not silently dropped.
		recurringRows, err := deps.Queries.ListRecurringEventsByCalendarAndRange(ctx, generated.ListRecurringEventsByCalendarAndRangeParams{
			CalendarID:    cal.ID,
			StartAt:       windowEnd,
			RecurrenceEnd: sql.NullTime{Time: windowStart, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		for _, e := range recurringRows {
			if e.RecurrenceRule == nil {
				continue
			}
			rule := recurrence.ParseRule(*e.RecurrenceRule)
			if rule == nil {
				continue
			}
			occurrences := recurrence.ExpandInZone(rule, e.StartAt, e.EndAt, windowStart, windowEnd, e.Timezone)
			for _, occ := range occurrences {
				exports = append(exports, exportEvent{
					event:     e,
					startAt:   occ.StartAt,
					endAt:     occ.EndAt,
					uidSuffix: "-" + occ.StartAt.UTC().Format("20060102T150405"),
				})
			}
		}

		switch in.Format {
		case "csv":
			body := buildCSV(exports)
			return &ExportOutput{
				ContentType:        "text/csv; charset=utf-8",
				ContentDisposition: fmt.Sprintf(`attachment; filename="%s.csv"`, sanitizeFilename(cal.Name)),
				Body:               []byte(body),
			}, nil
		default:
			body := buildICS(cal.Name, exports)
			return &ExportOutput{
				ContentType:        "text/calendar; charset=utf-8",
				ContentDisposition: fmt.Sprintf(`attachment; filename="%s.ics"`, sanitizeFilename(cal.Name)),
				Body:               []byte(body),
			}, nil
		}
	}
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

func buildICS(calName string, rows []exportEvent) string {
	var b strings.Builder
	writeFolded(&b, "BEGIN:VCALENDAR")
	writeFolded(&b, "VERSION:2.0")
	writeFolded(&b, "PRODID:-//Nodate Time//EN")
	writeFolded(&b, "CALSCALE:GREGORIAN")
	writeFolded(&b, "X-WR-CALNAME:"+icsEscape(calName))
	for _, x := range rows {
		e := x.event
		writeFolded(&b, "BEGIN:VEVENT")
		writeFolded(&b, "UID:"+hex.EncodeToString(e.PublicID)+x.uidSuffix+"@nodate-time")
		writeFolded(&b, "DTSTAMP:"+icsTime(time.Now()))
		if e.AllDay {
			writeFolded(&b, "DTSTART;VALUE=DATE:"+icsDate(x.startAt, e.Timezone))
			writeFolded(&b, "DTEND;VALUE=DATE:"+icsDate(x.endAt, e.Timezone))
		} else if tz := e.Timezone; tz != "" && tz != "UTC" {
			// Emit local wall time anchored to the event's timezone so the
			// original zone survives the round-trip.
			loc := loadLocationOrUTC(tz)
			writeFolded(&b, "DTSTART;TZID="+tz+":"+x.startAt.In(loc).Format("20060102T150405"))
			writeFolded(&b, "DTEND;TZID="+tz+":"+x.endAt.In(loc).Format("20060102T150405"))
		} else {
			writeFolded(&b, "DTSTART:"+icsTime(x.startAt))
			writeFolded(&b, "DTEND:"+icsTime(x.endAt))
		}
		writeFolded(&b, "SUMMARY:"+icsEscape(e.Title))
		if e.Location != "" {
			writeFolded(&b, "LOCATION:"+icsEscape(e.Location))
		}
		if e.Memo != "" {
			writeFolded(&b, "DESCRIPTION:"+icsEscape(e.Memo))
		}
		if e.Url != "" {
			writeFolded(&b, "URL:"+icsURI(e.Url))
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
			csvEscape(e.Location),
			csvEscape(e.Memo),
			csvEscape(e.Url),
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
}

func parseICSTime(value string, allDay bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if allDay {
		return time.Parse("20060102", value)
	}
	if strings.HasSuffix(value, "Z") {
		return time.Parse("20060102T150405Z", value)
	}
	return time.ParseInLocation("20060102T150405", value, time.UTC)
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
			for _, p := range parts[1:] {
				if strings.EqualFold(p, "VALUE=DATE") {
					isDate = true
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
			case "DTSTART":
				cur.allDay = isDate
				if t, err := parseICSTime(val, isDate); err == nil {
					cur.start = t
				}
			case "DTEND":
				if t, err := parseICSTime(val, isDate); err == nil {
					cur.end = t
				}
			}
		}
	}
	return events
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
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		events := parseICS(in.Body.ICS)
		if len(events) > importMaxEvents {
			events = events[:importMaxEvents]
		}

		var imported, skipped, failed int
		for _, e := range events {
			if e.summary == "" || e.start.IsZero() {
				skipped++
				continue
			}
			endAt := e.end
			if endAt.IsZero() {
				if e.allDay {
					endAt = e.start.AddDate(0, 0, 1)
				} else {
					endAt = e.start.Add(time.Hour)
				}
			}
			pubID, err := uuid.NewV7()
			if err != nil {
				failed++
				continue
			}
			if _, err := deps.Queries.CreateEvent(ctx, generated.CreateEventParams{
				PublicID:   pubID[:],
				CalendarID: cal.ID,
				Title:      e.summary,
				AllDay:     e.allDay,
				StartAt:    e.start,
				EndAt:      endAt,
				Timezone:   "UTC",
				Color:      "#47B2F7",
				Location:   e.location,
				Memo:       e.desc,
				Url:        e.url,
				CreatedBy:  userID,
			}); err != nil {
				failed++
				continue
			}
			imported++
		}

		out := &ImportOutput{}
		out.Body.Imported = imported
		out.Body.Skipped = skipped
		out.Body.Failed = failed
		return out, nil
	}
}
