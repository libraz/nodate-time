package ical

import (
	"encoding/json"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// ConvertRRule maps an RFC 5545 RRULE value onto the stored rule JSON, or
// reports false for a rule the expander would expand to other dates, so the
// caller refuses the event instead of storing a different series. start is
// DTSTART in the zone the series expands in.
func ConvertRRule(value string, start time.Time, allDay bool) (*json.RawMessage, bool) {
	rule, ok := recurrence.ParseRRule(value, start)
	if !ok {
		return nil, false
	}
	rule.Until = UntilInSeriesForm(rule.Until, start.Location(), allDay)
	data, err := json.Marshal(rule)
	if err != nil {
		return nil, false
	}
	raw := json.RawMessage(data)
	return &raw, true
}

// UntilInSeriesForm writes an until in the form its series takes: a date for
// an all-day series, an instant for a timed one. A date on a timed series ends
// at the close of that day in loc.
func UntilInSeriesForm(until *string, loc *time.Location, allDay bool) *string {
	if until == nil {
		return nil
	}
	var out string
	if t, err := time.Parse(time.RFC3339, *until); err == nil {
		if !allDay {
			return until
		}
		out = t.UTC().Format("2006-01-02")
	} else if d, err := time.ParseInLocation("2006-01-02", *until, loc); err == nil {
		if allDay {
			return until
		}
		out = d.AddDate(0, 0, 1).Add(-time.Second).UTC().Format(time.RFC3339)
	} else {
		return until
	}
	return &out
}
