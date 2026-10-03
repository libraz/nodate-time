package recurrence

import (
	"slices"
	"testing"
	"time"
)

// localDays renders each occurrence as its local date and wall-clock time, so
// a dropped, duplicated or drifted occurrence shows up as a different list.
func localDays(occ []Occurrence, loc *time.Location) []string {
	out := make([]string, 0, len(occ))
	for _, o := range occ {
		out = append(out, o.StartAt.In(loc).Format("2006-01-02 15:04 MST"))
	}
	return out
}

func newYork(t *testing.T) *time.Location {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	return ny
}

// A daily 09:00 series keeps one occurrence per local day, each at 09:00,
// across the spring-forward transition (2025-03-09 in New York).
func TestExpandInZonePreservesWallClockAcrossDST(t *testing.T) {
	ny := newYork(t)
	start := time.Date(2025, 3, 8, 9, 0, 0, 0, ny)
	rule := &Rule{Freq: "daily", Interval: 1}

	occ := ExpandInZone(rule, start.UTC(), start.Add(time.Hour).UTC(),
		time.Date(2025, 3, 7, 0, 0, 0, 0, time.UTC), time.Date(2025, 3, 12, 0, 0, 0, 0, time.UTC),
		"America/New_York")
	want := []string{
		"2025-03-08 09:00 EST",
		"2025-03-09 09:00 EDT",
		"2025-03-10 09:00 EDT",
		"2025-03-11 09:00 EDT",
	}
	if got := localDays(occ, ny); !slices.Equal(got, want) {
		t.Fatalf("occurrences = %v, want %v", got, want)
	}
}

// The same across fall-back (2025-11-02 in New York).
func TestExpandInZonePreservesWallClockAcrossFallBack(t *testing.T) {
	ny := newYork(t)
	start := time.Date(2025, 11, 1, 9, 0, 0, 0, ny)
	rule := &Rule{Freq: "daily", Interval: 1}

	occ := ExpandInZone(rule, start.UTC(), start.Add(time.Hour).UTC(),
		time.Date(2025, 10, 31, 0, 0, 0, 0, time.UTC), time.Date(2025, 11, 5, 0, 0, 0, 0, time.UTC),
		"America/New_York")
	want := []string{
		"2025-11-01 09:00 EDT",
		"2025-11-02 09:00 EST",
		"2025-11-03 09:00 EST",
		"2025-11-04 09:00 EST",
	}
	if got := localDays(occ, ny); !slices.Equal(got, want) {
		t.Fatalf("occurrences = %v, want %v", got, want)
	}
}

// A wall time that does not exist on the transition day (02:30 on
// 2026-03-08 in New York) still yields exactly one occurrence that day. Go
// resolves the missing time with the post-transition offset, landing it at
// 01:30 EST; the next day is back at 02:30.
func TestExpandInZoneNonexistentWallTimeOccursOnce(t *testing.T) {
	ny := newYork(t)
	start := time.Date(2026, 3, 7, 2, 30, 0, 0, ny)
	rule := &Rule{Freq: "daily", Interval: 1}

	occ := ExpandInZone(rule, start.UTC(), start.Add(30*time.Minute).UTC(),
		time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
		"America/New_York")
	want := []string{
		"2026-03-07 02:30 EST",
		"2026-03-08 01:30 EST",
		"2026-03-09 02:30 EDT",
		"2026-03-10 02:30 EDT",
	}
	if got := localDays(occ, ny); !slices.Equal(got, want) {
		t.Fatalf("occurrences = %v, want %v", got, want)
	}
}

// A wall time that happens twice on the transition day (01:30 on 2026-11-01
// in New York) yields one occurrence, at the first of the two instants.
func TestExpandInZoneAmbiguousWallTimeOccursOnce(t *testing.T) {
	ny := newYork(t)
	start := time.Date(2026, 10, 31, 1, 30, 0, 0, ny)
	rule := &Rule{Freq: "daily", Interval: 1}

	occ := ExpandInZone(rule, start.UTC(), start.Add(30*time.Minute).UTC(),
		time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC),
		"America/New_York")
	want := []string{
		"2026-10-31 01:30 EDT",
		"2026-11-01 01:30 EDT",
		"2026-11-02 01:30 EST",
		"2026-11-03 01:30 EST",
	}
	if got := localDays(occ, ny); !slices.Equal(got, want) {
		t.Fatalf("occurrences = %v, want %v", got, want)
	}
}

func TestExpandHonorsCount(t *testing.T) {
	start := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)
	rule := &Rule{Freq: "daily", Interval: 1, Count: 3}
	winStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	occ := Expand(rule, start, start.Add(time.Hour), winStart, winEnd)
	if len(occ) != 3 {
		t.Fatalf("expected exactly 3 occurrences for Count=3, got %d", len(occ))
	}
}

// TestExpandUnboundedIsCapped ensures an unbounded daily rule over a very wide
// window cannot generate an unbounded number of occurrences.
func TestExpandUnboundedIsCapped(t *testing.T) {
	start := time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC)
	rule := &Rule{Freq: "daily", Interval: 1}
	winStart := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)

	occ := Expand(rule, start, start.Add(time.Hour), winStart, winEnd)
	if len(occ) > maxExpansionIterations {
		t.Fatalf("expansion exceeded safety cap: %d", len(occ))
	}
}
