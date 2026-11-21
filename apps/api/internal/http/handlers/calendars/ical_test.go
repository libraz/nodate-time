package calendars

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// An all-day event is dates: its UTC bounds are its dates, whatever zone it
// records.
func TestBuildICSAllDayWritesTheUTCDates(t *testing.T) {
	start := time.Date(2025, 6, 24, 0, 0, 0, 0, time.UTC)
	ev := generated.CalendarEvent{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "Concert",
		AllDay:   true,
		Timezone: "America/Los_Angeles",
	}
	out := buildICS("Nodate Time", "Cal", []exportEvent{{event: ev, startAt: start, endAt: start.AddDate(0, 0, 1)}})
	for _, want := range []string{"DTSTART;VALUE=DATE:20250624", "DTEND;VALUE=DATE:20250625"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "20250623") || strings.Contains(out, "TZID") {
		t.Errorf("an all-day event must not shift to the zone's day or name a zone:\n%s", out)
	}
}

// A one-day all-day event reads as the same start and end date in CSV, since
// the stored end is the exclusive midnight after the last day.
func TestBuildCSVAllDayEndDateIsInclusive(t *testing.T) {
	start := time.Date(2025, 6, 24, 0, 0, 0, 0, time.UTC)
	ev := generated.CalendarEvent{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "Concert",
		AllDay:   true,
		Timezone: "Asia/Tokyo",
	}
	out := string(buildCSV([]exportEvent{{event: ev, startAt: start, endAt: start.AddDate(0, 0, 1)}}))
	if !strings.Contains(out, "Concert,2025-06-24,00:00:00,2025-06-24,00:00:00,true,") {
		t.Errorf("a one-day all-day event should read as 2025-06-24 to 2025-06-24, got:\n%s", out)
	}
	if strings.Contains(out, "2025-06-25") {
		t.Errorf("all-day end date must not show the exclusive midnight day 2025-06-25:\n%s", out)
	}
}

// A timed event leaves in its own zone, defined by a VTIMEZONE in the same
// file, so a re-import keeps the zone; one in UTC stays a bare instant.
func TestBuildICSTimedEventCarriesItsZone(t *testing.T) {
	start := time.Date(2025, 6, 24, 1, 0, 0, 0, time.UTC) // 10:00 JST
	ev := generated.CalendarEvent{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "Meeting",
		Timezone: "Asia/Tokyo",
	}
	out := buildICS("Nodate Time", "Cal", []exportEvent{{event: ev, startAt: start, endAt: start.Add(time.Hour)}})
	for _, want := range []string{
		"DTSTART;TZID=Asia/Tokyo:20250624T100000",
		"DTEND;TZID=Asia/Tokyo:20250624T110000",
		"BEGIN:VTIMEZONE\r\nTZID:Asia/Tokyo\r\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}

	ev.Timezone = "UTC"
	out = buildICS("Nodate Time", "Cal", []exportEvent{{event: ev, startAt: start, endAt: start.Add(time.Hour)}})
	if !strings.Contains(out, "DTSTART:20250624T010000Z") {
		t.Errorf("a UTC event should emit a UTC instant, got:\n%s", out)
	}
	if strings.Contains(out, "TZID") {
		t.Errorf("a UTC event needs no zone definition, got:\n%s", out)
	}
}

// TestBuildICSEscaping verifies TEXT vs URI escaping rules.
func TestBuildICSEscaping(t *testing.T) {
	ev := generated.CalendarEvent{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "A, B; C",
		Memo:     sql.NullString{String: "line1\r\nline2", Valid: true},
		URL:      sql.NullString{String: "https://example.com/a,b;c", Valid: true},
		Timezone: "UTC",
	}
	out := buildICS("Nodate Time", "Cal", []exportEvent{{event: ev, startAt: time.Now().UTC(), endAt: time.Now().UTC()}})
	if !strings.Contains(out, `SUMMARY:A\, B\; C`) {
		t.Errorf("SUMMARY commas/semicolons must be TEXT-escaped:\n%s", out)
	}
	if !strings.Contains(out, `DESCRIPTION:line1\nline2`) {
		t.Errorf("DESCRIPTION CRLF must be normalized to \\n:\n%s", out)
	}
	if !strings.Contains(out, "URL:https://example.com/a,b;c") {
		t.Errorf("URL value must NOT be TEXT-escaped:\n%s", out)
	}
}

// A recurring series leaves as one VEVENT carrying its rule. Writing one
// VEVENT per occurrence would hand an importing client thousands of unrelated
// single events it can no longer edit as a series.
func TestBuildICSWritesASeriesAsOneVEventWithItsRule(t *testing.T) {
	ev := generated.CalendarEvent{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "Standup",
		Timezone: "UTC",
	}
	base := time.Date(2025, 6, 24, 9, 0, 0, 0, time.UTC)
	rows := []exportEvent{{
		event:      ev,
		startAt:    base,
		endAt:      base.Add(time.Hour),
		rule:       &recurrence.Rule{Freq: "daily", Interval: 1, Count: 30},
		exceptions: exceptionsOf(base.AddDate(0, 0, 2).Format(time.RFC3339)),
	}}
	out := buildICS("Nodate Time", "Cal", rows)
	if n := strings.Count(out, "BEGIN:VEVENT"); n != 1 {
		t.Errorf("expected 1 VEVENT for the series, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "RRULE:FREQ=DAILY;COUNT=30;WKST=MO") {
		t.Errorf("the rule itself must be written out:\n%s", out)
	}
	if !strings.Contains(out, "EXDATE:20250626T090000Z") {
		t.Errorf("a cancelled occurrence must be written as EXDATE:\n%s", out)
	}
}

// A changed occurrence shares the series' UID and is told apart by its
// RECURRENCE-ID. A UID of its own would make it a separate event, so a
// re-import would show it next to the occurrence it replaces.
func TestBuildICSTiesAChangedOccurrenceToItsSeries(t *testing.T) {
	ev := generated.CalendarEvent{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "Standup",
		Timezone: "UTC",
	}
	base := time.Date(2025, 6, 24, 9, 0, 0, 0, time.UTC)
	moved := ev
	moved.Title = "Standup (moved)"
	rows := []exportEvent{
		{event: ev, startAt: base, endAt: base.Add(time.Hour),
			rule: &recurrence.Rule{Freq: "daily", Interval: 1}},
		{event: moved, startAt: base.AddDate(0, 0, 1).Add(4 * time.Hour),
			endAt:         base.AddDate(0, 0, 1).Add(5 * time.Hour),
			originalStart: base.AddDate(0, 0, 1), isOverride: true},
	}
	out := buildICS("Nodate Time", "Cal", rows)
	if n := strings.Count(out, "UID:0102030405060708090a0b0c0d0e0f10@nodate-time"); n != 2 {
		t.Errorf("both must carry the series UID, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "RECURRENCE-ID:20250625T090000Z") {
		t.Errorf("the changed occurrence must name the one it replaces:\n%s", out)
	}
}
