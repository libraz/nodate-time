package recurrence

import (
	"testing"
	"time"
)

// TestExpandInZonePreservesWallClockAcrossDST verifies that a daily 09:00 event
// in a DST-observing zone keeps its 09:00 local wall-clock time across the
// spring-forward boundary, rather than drifting by an hour (as a UTC-anchored
// expansion would).
func TestExpandInZonePreservesWallClockAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}

	// 2025-03-08 09:00 EST (UTC-5); DST spring-forward is 2025-03-09.
	start := time.Date(2025, 3, 8, 9, 0, 0, 0, ny)
	end := start.Add(time.Hour)
	rule := &Rule{Freq: "daily", Interval: 1}

	winStart := time.Date(2025, 3, 7, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2025, 3, 12, 0, 0, 0, 0, time.UTC)

	occ := ExpandInZone(rule, start.UTC(), end.UTC(), winStart, winEnd, "America/New_York")
	if len(occ) < 3 {
		t.Fatalf("expected at least 3 occurrences, got %d", len(occ))
	}
	for _, o := range occ {
		local := o.StartAt.In(ny)
		if h, m := local.Hour(), local.Minute(); h != 9 || m != 0 {
			t.Errorf("occurrence %s drifted to %02d:%02d local (want 09:00)", o.StartAt.Format(time.RFC3339), h, m)
		}
	}
}

func TestExpandInZonePreservesWallClockAcrossFallBack(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}

	// 2025-11-01 09:00 EDT; DST fall-back is 2025-11-02.
	start := time.Date(2025, 11, 1, 9, 0, 0, 0, ny)
	end := start.Add(time.Hour)
	rule := &Rule{Freq: "daily", Interval: 1}

	winStart := time.Date(2025, 10, 31, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2025, 11, 5, 0, 0, 0, 0, time.UTC)

	occ := ExpandInZone(rule, start.UTC(), end.UTC(), winStart, winEnd, "America/New_York")
	if len(occ) < 3 {
		t.Fatalf("expected at least 3 occurrences, got %d", len(occ))
	}
	for _, o := range occ {
		local := o.StartAt.In(ny)
		if h, m := local.Hour(), local.Minute(); h != 9 || m != 0 {
			t.Errorf("occurrence %s drifted to %02d:%02d local (want 09:00)", o.StartAt.Format(time.RFC3339), h, m)
		}
	}
}

// TestExpandUTCAnchorWouldDrift documents that the plain UTC Expand (no zone)
// does NOT preserve wall-clock across DST — confirming ExpandInZone is required.
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
