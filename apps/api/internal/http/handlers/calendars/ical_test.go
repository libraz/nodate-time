package calendars

import (
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// TestBuildICSAllDayUsesEventTimezone verifies an all-day event is rendered on
// its local calendar day, not shifted by UTC conversion (H-15).
func TestBuildICSAllDayUsesEventTimezone(t *testing.T) {
	// Midnight 2025-06-24 in Asia/Tokyo == 2025-06-23T15:00:00Z.
	startUTC := time.Date(2025, 6, 23, 15, 0, 0, 0, time.UTC)
	endUTC := startUTC.AddDate(0, 0, 1)
	ev := generated.Event{
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
	ev := generated.Event{
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

// TestBuildICSTimedEmitsTZID verifies timed events carry their original zone.
func TestBuildICSTimedEmitsTZID(t *testing.T) {
	start := time.Date(2025, 6, 24, 1, 0, 0, 0, time.UTC) // 10:00 JST
	ev := generated.Event{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "Meeting",
		Timezone: "Asia/Tokyo",
	}
	out := buildICS("Cal", []exportEvent{{event: ev, startAt: start, endAt: start.Add(time.Hour)}})
	if !strings.Contains(out, "DTSTART;TZID=Asia/Tokyo:20250624T100000") {
		t.Errorf("timed event should emit TZID local time, got:\n%s", out)
	}
}

// TestBuildICSEscaping verifies TEXT vs URI escaping rules.
func TestBuildICSEscaping(t *testing.T) {
	ev := generated.Event{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "A, B; C",
		Memo:     "line1\r\nline2",
		Url:      "https://example.com/a,b;c",
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

// TestBuildICSExpandsMultipleOccurrences verifies a recurring master expanded
// into several occurrences emits one VEVENT per occurrence (C-5).
func TestBuildICSExpandsMultipleOccurrences(t *testing.T) {
	ev := generated.Event{
		PublicID: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Title:    "Standup",
		Timezone: "UTC",
	}
	base := time.Date(2025, 6, 24, 9, 0, 0, 0, time.UTC)
	rows := []exportEvent{
		{event: ev, startAt: base, endAt: base.Add(time.Hour), uidSuffix: "-20250624T090000"},
		{event: ev, startAt: base.AddDate(0, 0, 1), endAt: base.AddDate(0, 0, 1).Add(time.Hour), uidSuffix: "-20250625T090000"},
	}
	out := buildICS("Cal", rows)
	if n := strings.Count(out, "BEGIN:VEVENT"); n != 2 {
		t.Errorf("expected 2 VEVENTs for 2 occurrences, got %d:\n%s", n, out)
	}
	if strings.Count(out, "UID:") != 2 || !strings.Contains(out, "-20250625T090000@nodate-time") {
		t.Errorf("each occurrence should have a unique UID:\n%s", out)
	}
}
