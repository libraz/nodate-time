package recurrence

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// An event whose end equals its start is a marker at a moment. Treated as the
// empty interval it literally is, it overlaps nothing, and the arithmetic
// quietly agrees: a marker sitting exactly on a window edge is too late for the
// window that closes there and too early for the one that opens there.
//
// The expander says so in two independent places -- the overlap check, and the
// fast-forward that decides which candidate the iterator starts from -- and the
// second is the dangerous one. A wrong overlap check rejects an occurrence it
// was offered; a wrong fast-forward steps over it, so nothing is ever asked.
// Fixing only the first leaves a daily marker visible everywhere except the
// days it lands on.

func tokyoTime(t *testing.T, y int, m time.Month, d, h int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	return time.Date(y, m, d, h, 0, 0, 0, loc)
}

// The window that opens on the marker is the one it belongs to.
func TestAMarkerBelongsToTheWindowItOpens(t *testing.T) {
	start := tokyoTime(t, 2026, 6, 15, 0)
	rule := &Rule{Freq: "daily", Interval: 1, Count: 5}

	occ := Expand(rule, start, start, tokyoTime(t, 2026, 6, 15, 0), tokyoTime(t, 2026, 6, 16, 0))
	require.Len(t, occ, 1, "the marker at the window's own start must be in it")
	require.Equal(t, start, occ[0].StartAt)
}

// And not to the window that closes on it, or it would show on two days.
func TestAMarkerIsNotInTheWindowItCloses(t *testing.T) {
	start := tokyoTime(t, 2026, 6, 15, 0)
	rule := &Rule{Freq: "daily", Interval: 1, Count: 5}

	occ := Expand(rule, start, start, tokyoTime(t, 2026, 6, 14, 0), tokyoTime(t, 2026, 6, 15, 0))
	require.Empty(t, occ, "a marker at midnight is not part of the day that ends there")
}

// A window later in the series is where the fast-forward decides the starting
// candidate, so this is the case that catches it stepping over one.
func TestAMarkerSurvivesTheFastForwardIntoALaterWindow(t *testing.T) {
	start := tokyoTime(t, 2026, 6, 15, 0)
	rule := &Rule{Freq: "daily", Interval: 1, Count: 5}

	for _, day := range []int{16, 17, 18, 19} {
		occ := Expand(rule, start, start,
			tokyoTime(t, 2026, 6, day, 0), tokyoTime(t, 2026, 6, day+1, 0))
		require.Len(t, occ, 1, "the occurrence on the %dth must survive the fast-forward", day)
		require.Equal(t, tokyoTime(t, 2026, 6, day, 0), occ[0].StartAt)
	}
}

// The control: an occurrence with a duration is unaffected in either direction.
func TestAnEventWithDurationKeepsHalfOpenBounds(t *testing.T) {
	start := tokyoTime(t, 2026, 6, 15, 0)
	end := start.Add(time.Hour)
	rule := &Rule{Freq: "daily", Interval: 1, Count: 5}

	occ := Expand(rule, start, end, tokyoTime(t, 2026, 6, 15, 0), tokyoTime(t, 2026, 6, 16, 0))
	require.Len(t, occ, 1)

	// It ends after the previous window opens only because it starts inside
	// this one; the day before must still not claim it.
	occ = Expand(rule, start, end, tokyoTime(t, 2026, 6, 14, 0), tokyoTime(t, 2026, 6, 15, 0))
	require.Empty(t, occ, "an event starting at midnight is not part of the day before")
}

// A series whose count and interval are both at their validated limits ends
// past any date a DATETIME column can store. The boundary is only used to skip
// series that cannot reach a window, so it answers the same way as one that
// never ends -- rather than overflowing and failing the insert.
func TestAFarFutureSeriesEndIsStorable(t *testing.T) {
	start := tokyoTime(t, 2026, 6, 15, 10)

	for _, freq := range []string{"yearly", "monthly", "weekly", "daily"} {
		rule := &Rule{Freq: freq, Interval: 999, Count: 1000}
		got := ComputeEnd(rule, start, start.Add(time.Hour))
		require.False(t, got.After(farFutureEnd),
			"%s at both limits must stay inside what the column can hold, got %v", freq, got)
		require.True(t, got.Year() <= 9999, "%s: year %d is past DATETIME", freq, got.Year())
	}
}

// A series that does end within reach keeps its real boundary; the clamp must
// not flatten every series into the sentinel.
func TestAnOrdinarySeriesEndIsNotClamped(t *testing.T) {
	start := tokyoTime(t, 2026, 6, 15, 10)
	rule := &Rule{Freq: "daily", Interval: 1, Count: 5}

	got := ComputeEnd(rule, start, start.Add(time.Hour))
	require.True(t, got.Before(farFutureEnd), "a five-day series must keep its own end, got %v", got)
	require.Equal(t, 2026, got.Year())
}
