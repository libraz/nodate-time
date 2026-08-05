package calendars

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// TestBuildICSAllDayUsesEventTimezone verifies an all-day event is rendered on
// its local calendar day, not shifted by UTC conversion.
func TestBuildICSAllDayUsesEventTimezone(t *testing.T) {
	// Midnight 2025-06-24 in Asia/Tokyo == 2025-06-23T15:00:00Z.
	startUTC := time.Date(2025, 6, 23, 15, 0, 0, 0, time.UTC)
	endUTC := startUTC.AddDate(0, 0, 1)
	ev := generated.CalendarEvent{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "Concert",
		AllDay:   true,
		Timezone: "Asia/Tokyo",
	}
	out := buildICS("Cal", []exportEvent{{event: ev, startAt: startUTC, endAt: endUTC}})
	if !strings.Contains(out, "DTSTART;VALUE=DATE:20250624") {
		t.Errorf("all-day DTSTART should be local 20250624, got:\n%s", out)
	}
	if strings.Contains(out, "20250623") {
		t.Errorf("all-day date must not shift to the UTC day 20250623:\n%s", out)
	}
}

// TestBuildCSVAllDayEndDateIsInclusive verifies a one-day all-day event reads as
// the same start/end date in CSV, since the stored end is the exclusive midnight
// after the last day.
func TestBuildCSVAllDayEndDateIsInclusive(t *testing.T) {
	startUTC := time.Date(2025, 6, 23, 15, 0, 0, 0, time.UTC) // 2025-06-24 JST
	endUTC := startUTC.AddDate(0, 0, 1)                       // exclusive: 2025-06-25 JST midnight
	ev := generated.CalendarEvent{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "Concert",
		AllDay:   true,
		Timezone: "Asia/Tokyo",
	}
	out := buildCSV([]exportEvent{{event: ev, startAt: startUTC, endAt: endUTC}})
	if !strings.Contains(out, "Concert,2025-06-24,") {
		t.Errorf("all-day start date should be 2025-06-24, got:\n%s", out)
	}
	if !strings.Contains(out, ",2025-06-24,00:00:00,true,") {
		t.Errorf("one-day all-day end date should be inclusive (2025-06-24), got:\n%s", out)
	}
	if strings.Contains(out, "2025-06-25") {
		t.Errorf("all-day end date must not show the exclusive midnight day 2025-06-25:\n%s", out)
	}
}

// TestBuildICSTimedEmitsUTC verifies timed events are normalized to UTC (Z),
// so no TZID reference is emitted without an accompanying VTIMEZONE component.
func TestBuildICSTimedEmitsUTC(t *testing.T) {
	start := time.Date(2025, 6, 24, 1, 0, 0, 0, time.UTC) // 10:00 JST
	ev := generated.CalendarEvent{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "Meeting",
		Timezone: "Asia/Tokyo",
	}
	out := buildICS("Cal", []exportEvent{{event: ev, startAt: start, endAt: start.Add(time.Hour)}})
	if !strings.Contains(out, "DTSTART:20250624T010000Z") {
		t.Errorf("timed event should emit a UTC instant, got:\n%s", out)
	}
	if strings.Contains(out, "TZID=") {
		t.Errorf("export must not reference TZID without a VTIMEZONE component, got:\n%s", out)
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
	out := buildICS("Cal", []exportEvent{{event: ev, startAt: time.Now().UTC(), endAt: time.Now().UTC()}})
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
		exceptions: recurrence.Exceptions{base.AddDate(0, 0, 2)},
	}}
	out := buildICS("Cal", rows)
	if n := strings.Count(out, "BEGIN:VEVENT"); n != 1 {
		t.Errorf("expected 1 VEVENT for the series, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "RRULE:FREQ=DAILY;COUNT=30;WKST=SU") {
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
	out := buildICS("Cal", rows)
	if n := strings.Count(out, "UID:0102030405060708090a0b0c0d0e0f10@nodate-time"); n != 2 {
		t.Errorf("both must carry the series UID, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "RECURRENCE-ID:20250625T090000Z") {
		t.Errorf("the changed occurrence must name the one it replaces:\n%s", out)
	}
}
