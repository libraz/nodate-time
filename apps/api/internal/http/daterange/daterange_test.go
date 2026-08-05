package daterange

import (
	"errors"
	"testing"
	"time"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

// TestWideWindowIsRefused verifies the bound on a single request. The days
// parameter is capped, but naming both ends went around the cap entirely: a
// ten-year window expands every series in the calendar per occurrence, and one
// unauthenticated link holder could ask for it.
func TestWideWindowIsRefused(t *testing.T) {
	if _, err := Parse("2020-01-01", "2030-01-01", time.UTC); !errors.Is(err, ErrTooWide) {
		t.Errorf("a ten-year window should be refused, got %v", err)
	}
	// A year is what the days parameter already allowed, and stays allowed.
	if _, err := Parse("2026-01-01", "2026-12-31", time.UTC); err != nil {
		t.Errorf("a year-wide window should be accepted, got %v", err)
	}
}

// TestInvertedWindowIsRefusedEitherWay verifies both listings answer the same
// way. One returned 400 and the other quietly returned nothing, so the same
// request read as a client bug through one door and an empty calendar through
// the other.
func TestInvertedWindowIsRefusedEitherWay(t *testing.T) {
	if _, err := Parse("2026-03-10", "2026-03-01", time.UTC); !errors.Is(err, ErrInverted) {
		t.Errorf("an inverted window should be refused, got %v", err)
	}
}

func TestMalformedDates(t *testing.T) {
	for _, c := range []struct{ from, to string }{
		{"not-a-date", "2026-03-01"},
		{"2026-03-01", "not-a-date"},
		{"", "2026-03-01"},
		{"2026-13-45", "2026-12-01"},
	} {
		if _, err := Parse(c.from, c.to, time.UTC); !errors.Is(err, ErrMalformed) {
			t.Errorf("Parse(%q, %q) should be malformed, got %v", c.from, c.to, err)
		}
	}
}

// TestWindowIsResolvedInTheRequestedZone verifies a calendar day is read where
// the caller reads it. Resolved at UTC midnight, a JST window starts nine
// hours late: an event at 08:00 on the first of the month ends before the
// window opens and vanishes from the view showing that month.
func TestWindowIsResolvedInTheRequestedZone(t *testing.T) {
	tokyo := mustLoad(t, "Asia/Tokyo")
	r, err := Parse("2026-04-01", "2026-04-30", tokyo)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// 2026-04-01 08:00 JST -- 09:00 JST, the case that disappeared.
	eventStart := time.Date(2026, 4, 1, 8, 0, 0, 0, tokyo)
	eventEnd := eventStart.Add(time.Hour)
	if !eventEnd.After(r.Start) || !eventStart.Before(r.End) {
		t.Errorf("an 08:00 event on the first should be inside the month, window %v..%v", r.Start, r.End)
	}

	// The last day is included whole: 23:00 on the 30th is still April.
	lastEvening := time.Date(2026, 4, 30, 23, 0, 0, 0, tokyo)
	if !lastEvening.Before(r.End) {
		t.Errorf("the last day should be covered to its end, window ends %v", r.End)
	}
	// And the day after is not.
	if time.Date(2026, 5, 1, 0, 30, 0, 0, tokyo).Before(r.End) {
		t.Error("the window should stop at the end of the requested last day")
	}
}

// TestUnknownZoneFallsBack verifies a zone name nobody recognises does not
// take the listing down with it.
func TestUnknownZoneFallsBack(t *testing.T) {
	if got := Location("Mars/Olympus", "Asia/Tokyo"); got.String() != "Asia/Tokyo" {
		t.Errorf("an unknown zone should fall back to the caller's own, got %v", got)
	}
	if got := Location("", ""); got != time.UTC {
		t.Errorf("with nothing to go on the window is UTC, got %v", got)
	}
	if got := Location("Europe/Berlin", "Asia/Tokyo"); got.String() != "Europe/Berlin" {
		t.Errorf("an explicit zone should win, got %v", got)
	}
}

func TestDefaultWindowIsBounded(t *testing.T) {
	r := Default(100000, time.UTC)
	if span := r.End.Sub(r.Start); span > time.Duration(MaxDays+7)*24*time.Hour {
		t.Errorf("the default window should stay bounded, got %v", span)
	}
	if r := Default(0, time.UTC); !r.End.After(r.Start) {
		t.Error("a zero-day request should still be a window")
	}
}
