package calendars

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

var testPublicID = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

// A Monday 08:00 Tokyo series is Sunday 23:00 in UTC. Written as a UTC
// DTSTART, BYDAY=MO expands to Mondays in UTC -- Tuesdays in Tokyo.
func TestBuildICSWritesASeriesInItsOwnZone(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	start := time.Date(2026, 1, 5, 8, 0, 0, 0, tokyo) // Monday
	ev := generated.CalendarEvent{PublicID: testPublicID, Title: "Standup", Timezone: "Asia/Tokyo"}
	out := buildICS("Nodate Time", "Cal", []exportEvent{{
		event: ev, startAt: start.UTC(), endAt: start.Add(time.Hour).UTC(),
		rule:       &recurrence.Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO"}},
		exceptions: recurrence.Exceptions{start.AddDate(0, 0, 7).UTC()},
	}})
	for _, want := range []string{
		"DTSTART;TZID=Asia/Tokyo:20260105T080000",
		"DTEND;TZID=Asia/Tokyo:20260105T090000",
		"EXDATE;TZID=Asia/Tokyo:20260112T080000",
		"BEGIN:VTIMEZONE\r\nTZID:Asia/Tokyo\r\n",
		"TZOFFSETTO:+0900",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "DTSTART:20260104T230000Z") {
		t.Errorf("the series must not start on the UTC Sunday:\n%s", out)
	}
}

// A New York series keeps its wall-clock time across DST only when the rule
// expands in New York, which needs the zone's rules in the file.
func TestBuildICSDefinesTheZoneAcrossDST(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	start := time.Date(2026, 3, 2, 9, 0, 0, 0, ny)
	ev := generated.CalendarEvent{PublicID: testPublicID, Title: "Sync", Timezone: "America/New_York"}
	out := buildICS("Nodate Time", "Cal", []exportEvent{{
		event: ev, startAt: start.UTC(), endAt: start.Add(time.Hour).UTC(),
		rule: &recurrence.Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO"}},
	}})
	for _, want := range []string{
		"DTSTART;TZID=America/New_York:20260302T090000",
		"TZID:America/New_York",
		"TZOFFSETFROM:-0500\r\nTZOFFSETTO:-0400\r\nTZNAME:EDT\r\nRRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=2SU",
		"TZOFFSETFROM:-0400\r\nTZOFFSETTO:-0500\r\nTZNAME:EST\r\nRRULE:FREQ=YEARLY;BYMONTH=11;BYDAY=1SU",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// A rule counted from the end of the month is the one Europe uses.
func TestVTimezoneRecoversLastSundayRules(t *testing.T) {
	var b strings.Builder
	writeVTimezone(&b, "Europe/London", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	out := b.String()
	for _, want := range []string{
		"RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU",
		"RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// RECURRENCE-ID takes the series' value type, whatever the override became.
func TestRecurrenceIDFollowsTheSeries(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	original := time.Date(2026, 1, 12, 8, 0, 0, 0, tokyo)
	allDayOverride := generated.CalendarEvent{PublicID: testPublicID, Title: "Off", AllDay: true, Timezone: "Asia/Tokyo"}
	out := buildICS("Nodate Time", "Cal", []exportEvent{{
		event: allDayOverride, startAt: time.Date(2026, 1, 12, 0, 0, 0, 0, tokyo).UTC(),
		endAt:         time.Date(2026, 1, 13, 0, 0, 0, 0, tokyo).UTC(),
		originalStart: original.UTC(), isOverride: true, parentTimezone: "Asia/Tokyo",
	}})
	if !strings.Contains(out, "RECURRENCE-ID;TZID=Asia/Tokyo:20260112T080000") {
		t.Errorf("an all-day override of a timed series must name a timed occurrence:\n%s", out)
	}
	if strings.Contains(out, "RECURRENCE-ID;VALUE=DATE") {
		t.Errorf("RECURRENCE-ID must not follow the override's own all-day flag:\n%s", out)
	}

	timedOverride := generated.CalendarEvent{PublicID: testPublicID, Title: "Moved", Timezone: "Asia/Tokyo"}
	day := time.Date(2026, 1, 12, 0, 0, 0, 0, tokyo)
	out = buildICS("Nodate Time", "Cal", []exportEvent{{
		event: timedOverride, startAt: day.Add(10 * time.Hour).UTC(), endAt: day.Add(11 * time.Hour).UTC(),
		originalStart: day.UTC(), isOverride: true, parentAllDay: true, parentTimezone: "Asia/Tokyo",
	}})
	if !strings.Contains(out, "RECURRENCE-ID;VALUE=DATE:20260112") {
		t.Errorf("a timed override of an all-day series must name a date:\n%s", out)
	}
}

func TestCSVEscapeNeutralizesFormulasAndControls(t *testing.T) {
	cases := map[string]string{
		`=HYPERLINK("http://x")`: `"'=HYPERLINK(""http://x"")"`,
		"+1":                     "'+1",
		"-2":                     "'-2",
		"@SUM(A1)":               "'@SUM(A1)",
		"\t=1":                   "'\t=1",
		"a\x1b[31mb":             "a[31mb",
		"a\rb":                   "ab",
		"a,b":                    `"a,b"`,
		"line1\nline2":           "\"line1\nline2\"",
		"plain":                  "plain",
	}
	for in, want := range cases {
		if got := csvEscape(in); got != want {
			t.Errorf("csvEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExportDispositionIsASCIIAndCarriesTheName(t *testing.T) {
	for name, want := range map[string]string{
		"家族 予定":     "家族_予定.ics",
		"a\r\nb\"c": "a__b_c.ics",
		"":          "calendar.ics",
	} {
		got := exportDisposition(name, "ics")
		for _, r := range got {
			if r < 0x20 || r > 0x7e {
				t.Fatalf("header for %q is not printable ASCII: %q", name, got)
			}
		}
		i := strings.Index(got, "filename*=UTF-8''")
		if i < 0 {
			t.Fatalf("header for %q has no filename*: %q", name, got)
		}
		decoded, err := url.PathUnescape(got[i+len("filename*=UTF-8''"):])
		if err != nil || decoded != want {
			t.Errorf("filename* for %q decodes to %q (%v), want %q", name, decoded, err, want)
		}
		if !strings.Contains(got, `filename="`) {
			t.Errorf("header for %q has no ASCII fallback: %q", name, got)
		}
	}
}

// from/to are calendar days in the reader's zone, and the inclusive end is
// the end of that local day even when it is 25 hours long.
func TestExportWindowIsLocalDays(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	w, err := parseExportWindow("2026-10-01", "2026-10-31", tokyo)
	if err != nil {
		t.Fatal(err)
	}
	if !w.start.Equal(time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)) ||
		!w.end.Equal(time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("window = %s..%s, want Tokyo days", w.start.UTC(), w.end.UTC())
	}
	if got := w.header(); got != "2026-10-01/2026-11-01" {
		t.Errorf("header = %q", got)
	}

	ny, _ := time.LoadLocation("America/New_York")
	w, err = parseExportWindow("2026-11-01", "2026-11-01", ny)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.end.Sub(w.start); got != 25*time.Hour {
		t.Errorf("the day DST ends spans %s, want 25h", got)
	}
}

// A head is kept when it reaches the window start exactly: a series whose end
// boundary is the window start, and a zero-length event sitting on it.
func TestSeriesReachesWindowKeepsTheWindowStart(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	w := exportWindow{start: start, end: start.AddDate(0, 1, 0), loc: time.UTC}
	rule := json.RawMessage(`{"freq":"daily","interval":1,"count":3}`)
	series := generated.CalendarEvent{
		StartAt:        sql.NullTime{Time: start.AddDate(0, 0, -2), Valid: true},
		EndAt:          sql.NullTime{Time: start.AddDate(0, 0, -2), Valid: true},
		RecurrenceRule: &rule,
		RecurrenceEnd:  sql.NullTime{Time: start, Valid: true},
	}
	if !seriesReachesWindow(series, w) {
		t.Error("a series ending exactly at the window start must be kept")
	}
	marker := generated.CalendarEvent{
		StartAt: sql.NullTime{Time: start, Valid: true},
		EndAt:   sql.NullTime{Time: start, Valid: true},
	}
	if !seriesReachesWindow(marker, w) {
		t.Error("a zero-length event at the window start must be kept")
	}
	before := marker
	before.StartAt.Time, before.EndAt.Time = start.Add(-time.Hour), start
	if seriesReachesWindow(before, w) {
		t.Error("an event ending at the window start is outside it")
	}
}
