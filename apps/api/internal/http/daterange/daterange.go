// Package daterange resolves the window a calendar listing covers.
//
// It exists because the window is asked for in calendar days and answered in
// instants, and the two are only the same thing once a timezone is named. The
// authenticated listing and the public feed both take the same parameters and
// have to agree on what they mean: a window resolved one way in one and
// another way in the other shows a different calendar through each door.
package daterange

import (
	"errors"
	"time"
)

// MaxDays bounds a single request. A recurring series is expanded per
// occurrence, so the width of the window -- not the number of rows behind it
// -- is what decides how much work one request is.
const MaxDays = 366

// MaxInstances bounds how many rendered occurrences one listing may carry.
//
// MaxDays bounds a single series, but nothing bounds how many series a
// calendar holds, and each one expands per occurrence. Without this a calendar
// of daily series answers one request with the product of the two.
const MaxInstances = 5000

var (
	// ErrMalformed is a date that is not a date.
	ErrMalformed = errors.New("daterange: unparseable date")
	// ErrInverted is a window that ends before it starts.
	ErrInverted = errors.New("daterange: end before start")
	// ErrTooWide is a window past MaxDays.
	ErrTooWide = errors.New("daterange: window too wide")
)

// Range is a resolved window, half-open: an event is inside it when it starts
// before End and ends after Start.
type Range struct {
	Start time.Time
	End   time.Time
}

// Parse resolves an inclusive from/to pair of calendar dates into the instants
// they span in loc.
//
// The window is rejected rather than narrowed when it is too wide. A clamped
// window would answer a question the caller did not ask and look like a
// calendar with nothing in the rest of the period.
func Parse(from, to string, loc *time.Location) (Range, error) {
	start, err := time.ParseInLocation("2006-01-02", from, loc)
	if err != nil {
		return Range{}, ErrMalformed
	}
	end, err := time.ParseInLocation("2006-01-02", to, loc)
	if err != nil {
		return Range{}, ErrMalformed
	}
	if end.Before(start) {
		return Range{}, ErrInverted
	}
	// Inclusive: a caller asking through the 5th means the end of it.
	end = end.AddDate(0, 0, 1)
	if end.Sub(start) > time.Duration(MaxDays)*24*time.Hour {
		return Range{}, ErrTooWide
	}
	return Range{Start: start, End: end}, nil
}

// Default is the window used when the caller named no dates: a week back for
// context, and as far ahead as they asked.
func Default(days int, loc *time.Location) Range {
	if days < 1 {
		days = 1
	}
	if days > MaxDays {
		days = MaxDays
	}
	now := time.Now().In(loc)
	return Range{Start: now.AddDate(0, 0, -7), End: now.AddDate(0, 0, days)}
}

// Location resolves a requested IANA timezone, falling back to fallback and
// then to UTC. An unknown zone is a fallback rather than an error: the window
// is still answerable, and refusing the whole listing over a zone name would
// be a harsher answer than the request deserves.
func Location(tz, fallback string) *time.Location {
	for _, name := range []string{tz, fallback} {
		if name == "" {
			continue
		}
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.UTC
}
