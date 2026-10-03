package recurrence

import (
	"testing"
	"time"
)

// walkFromStart is the reference expansion: every candidate from the series
// start, with no fast-forward and no cap.
func walkFromStart(rule *Rule, start, end, windowStart, windowEnd time.Time) []time.Time {
	duration := end.Sub(start)
	it := newIterator(rule, start)
	var out []time.Time
	for {
		c := it.next()
		if c.IsZero() || !c.Before(windowEnd) {
			return out
		}
		if c.Before(start) {
			continue
		}
		if c.Add(duration).After(windowStart) {
			out = append(out, c)
		}
	}
}

func starts(occ []Occurrence) []time.Time {
	out := make([]time.Time, 0, len(occ))
	for _, o := range occ {
		out = append(out, o.StartAt)
	}
	return out
}

// A long-running series yields the same occurrences in a recent window as
// walking it from the start would, however old it is. A weekday series from
// 1985 passes ten thousand candidates before 2026, which is where a cap
// counted from the series start used to stop it.
func TestExpandOldSeriesMatchesAWalkFromItsStart(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	windowStart := time.Date(2026, 3, 1, 0, 0, 0, 0, ny)
	windowEnd := time.Date(2026, 4, 1, 0, 0, 0, 0, ny)

	rules := map[string]*Rule{
		"weekdays":             {Freq: "weekly", Interval: 1, ByDay: []string{"MO", "TU", "WE", "TH", "FR"}},
		"every other week":     {Freq: "weekly", Interval: 2, ByDay: []string{"TU", "SA"}},
		"every third week":     {Freq: "weekly", Interval: 3, ByDay: []string{"SU"}},
		"weekly plain":         {Freq: "weekly", Interval: 1},
		"monthly day":          {Freq: "monthly", Interval: 1, ByMonthDay: 31},
		"monthly fifth monday": {Freq: "monthly", Interval: 1, ByDay: []string{"MO"}, BySetPos: 5},
		"quarterly":            {Freq: "monthly", Interval: 3},
		"yearly":               {Freq: "yearly", Interval: 1},
	}
	for _, year := range []int{1985, 1995, 2025} {
		start := time.Date(year, 1, 9, 9, 30, 0, 0, ny) // a Wednesday in 1985
		end := start.Add(90 * time.Minute)
		for name, rule := range rules {
			got := starts(Expand(rule, start, end, windowStart, windowEnd))
			want := walkFromStart(rule, start, end, windowStart, windowEnd)
			if len(got) != len(want) {
				t.Fatalf("%s from %d: %d occurrences, want %d (%v vs %v)", name, year, len(got), len(want), got, want)
			}
			for i := range want {
				if !got[i].Equal(want[i]) {
					t.Fatalf("%s from %d: occurrence %d = %v, want %v", name, year, i, got[i], want[i])
				}
			}
		}
	}
}

// The weekday case concretely: one March of a 1985 series has its 22 weekdays.
func TestExpandWeekdaySeriesFrom1985(t *testing.T) {
	start := time.Date(1985, 1, 7, 9, 0, 0, 0, time.UTC)
	rule := &Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO", "TU", "WE", "TH", "FR"}}
	occ := Expand(rule, start, start.Add(time.Hour),
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if len(occ) != 22 {
		t.Fatalf("got %d occurrences, want 22", len(occ))
	}
	if first := occ[0].StartAt; !first.Equal(time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("first occurrence = %v, want 2026-03-02 09:00", first)
	}
}

// A multi-day occurrence that began before the window still overlaps it.
func TestExpandFastForwardKeepsAnOccurrenceSpanningIntoTheWindow(t *testing.T) {
	start := time.Date(1990, 1, 6, 0, 0, 0, 0, time.UTC) // Saturday
	end := start.AddDate(0, 0, 3)
	rule := &Rule{Freq: "weekly", Interval: 1, ByDay: []string{"SA"}}
	windowStart := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC) // Monday after Sat 2026-02-28
	occ := Expand(rule, start, end, windowStart, windowStart.AddDate(0, 0, 1))
	if len(occ) != 1 || !occ[0].StartAt.Equal(time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %v, want the occurrence starting 2026-02-28", starts(occ))
	}
}
