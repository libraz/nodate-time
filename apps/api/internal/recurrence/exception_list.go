package recurrence

import (
	"encoding/json"
	"regexp"
	"time"
)

// ExpansionZone is the zone a series expands in: UTC for an all-day series,
// whatever its recorded timezone says, and the event's own zone otherwise.
// Every caller that expands, names an occurrence or matches an exception
// takes the zone from here.
func ExpansionZone(allDay bool, tz string) string {
	if allDay {
		return "UTC"
	}
	return tz
}

var (
	exceptionDate     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	exceptionRFC3339  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-][0-9]{2}:[0-9]{2})$`)
	exceptionFloating = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}$`)
)

const (
	dateLayout     = "2006-01-02"
	floatingLayout = "2006-01-02T15:04:05"
)

// ExceptionList is the stored exclusion list with every element in its
// original spelling. An element this package cannot read is kept as it is, so
// rewriting the list never drops what another writer put there.
type ExceptionList []json.RawMessage

// ParseExceptionList reads the stored exclusion list. A value that is not a
// JSON array yields an empty list.
func ParseExceptionList(data *json.RawMessage) ExceptionList {
	if data == nil || len(*data) == 0 {
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(*data, &raw); err != nil {
		return nil
	}
	return ExceptionList(raw)
}

// Cancels reports whether any element cancels the occurrence starting at
// start in a series with the given all-day flag and recorded timezone.
func (e ExceptionList) Cancels(start time.Time, allDay bool, tz string) bool {
	loc := LoadLocation(ExpansionZone(allDay, tz))
	for _, el := range e {
		if elementCancels(el, start, allDay, loc) {
			return true
		}
	}
	return false
}

// With returns the list with a cancellation of the occurrence added, in the
// form the series writes: a date for all-day, a UTC instant for timed. It is
// unchanged when an element already cancels the occurrence.
func (e ExceptionList) With(start time.Time, allDay bool, tz string) ExceptionList {
	if e.Cancels(start, allDay, tz) {
		return e
	}
	form := start.UTC().Format(time.RFC3339)
	if allDay {
		form = start.UTC().Format(dateLayout)
	}
	body, _ := json.Marshal(form)
	return append(append(ExceptionList{}, e...), body)
}

// Without returns the list with every element that cancels the occurrence
// removed, whatever form it is written in.
func (e ExceptionList) Without(start time.Time, allDay bool, tz string) ExceptionList {
	loc := LoadLocation(ExpansionZone(allDay, tz))
	out := make(ExceptionList, 0, len(e))
	for _, el := range e {
		if !elementCancels(el, start, allDay, loc) {
			out = append(out, el)
		}
	}
	return out
}

// MarshalColumn renders the list for storage; an empty list is NULL.
func (e ExceptionList) MarshalColumn() (*json.RawMessage, error) {
	if len(e) == 0 {
		return nil, nil
	}
	body, err := json.Marshal([]json.RawMessage(e))
	if err != nil {
		return nil, err
	}
	msg := json.RawMessage(body)
	return &msg, nil
}

// elementCancels classifies one element and applies the form-by-series table.
// Anything unreadable, including a calendar-invalid date, cancels nothing.
func elementCancels(el json.RawMessage, start time.Time, allDay bool, loc *time.Location) bool {
	var s string
	if err := json.Unmarshal(el, &s); err != nil {
		return false
	}
	switch {
	case exceptionDate.MatchString(s):
		d, err := time.Parse(dateLayout, s)
		if err != nil {
			return false
		}
		return sameDate(d, start.In(loc))
	case exceptionRFC3339.MatchString(s):
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return false
		}
		if allDay {
			return sameDate(t.UTC(), start.UTC())
		}
		return t.Equal(start)
	case exceptionFloating.MatchString(s):
		t, err := time.Parse(floatingLayout, s)
		if err != nil {
			return false
		}
		if allDay {
			return sameDate(t, start.UTC())
		}
		return t.Equal(wallClock(start.In(loc)))
	}
	return false
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// wallClock is t's wall-clock reading as a UTC time, so two readings compare
// without the zone.
func wallClock(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}
