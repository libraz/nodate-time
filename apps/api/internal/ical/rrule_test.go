package ical

import (
	"strings"
	"testing"
	"time"
)

// An RRULE without WKST means a Monday week start, and the stored rule keeps
// the week start wherever it changes the dates (a weekly rule with an interval
// and BYDAY). A week start other than MO or SU has no stored form and is
// refused.
func TestConvertRRuleKeepsTheWeekStart(t *testing.T) {
	saturday := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	monday := time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC)
	sunday := time.Date(2026, 1, 11, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		rule  string
		start time.Time
		ok    bool
		wkst  string
	}{
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU", saturday, true, `"wkst":"MO"`},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU;WKST=MO", saturday, true, `"wkst":"MO"`},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU;WKST=SU", saturday, true, `"wkst":"SU"`},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE", monday, true, `"wkst":"MO"`},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO", sunday, true, `"wkst":"MO"`},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=SU", sunday, true, `"wkst":"MO"`},
		{"FREQ=WEEKLY;BYDAY=SA,SU", saturday, true, ""},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,SA;WKST=TU", monday, false, ""},
		// The rule contract the API holds rules to applies to imports too.
		{"FREQ=DAILY;BYDAY=MO", monday, false, ""},
		{"FREQ=WEEKLY;BYMONTHDAY=5", monday, false, ""},
		{"FREQ=MONTHLY;BYDAY=MO", monday, false, ""},
		{"FREQ=MONTHLY;BYDAY=MO;BYSETPOS=2", monday, true, ""},
		{"FREQ=DAILY;UNTIL=20260101T000000Z", monday, false, ""},
	}
	for _, c := range cases {
		data, ok := ConvertRRule(c.rule, c.start, false)
		if ok != c.ok {
			t.Errorf("ConvertRRule(%q, %s) ok = %v, want %v", c.rule, c.start.Weekday(), ok, c.ok)
			continue
		}
		if ok && c.wkst != "" && !strings.Contains(string(*data), c.wkst) {
			t.Errorf("ConvertRRule(%q) = %s, want %s", c.rule, *data, c.wkst)
		}
		if ok && c.wkst == "" && strings.Contains(string(*data), "wkst") {
			t.Errorf("ConvertRRule(%q) = %s, want no wkst where it changes nothing", c.rule, *data)
		}
	}
}

func TestUntilTakesTheFormOfItsSeries(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	str := func(s string) *string { return &s }
	cases := []struct {
		name   string
		until  *string
		loc    *time.Location
		allDay bool
		want   string
	}{
		{"instant on an all-day series", str("2026-08-01T10:00:00Z"), time.UTC, true, "2026-08-01"},
		{"date on an all-day series", str("2026-08-01"), time.UTC, true, "2026-08-01"},
		{"date on a timed series", str("2026-08-01"), tokyo, false, "2026-08-01T14:59:59Z"},
		{"instant on a timed series", str("2026-08-01T10:00:00Z"), tokyo, false, "2026-08-01T10:00:00Z"},
	}
	for _, c := range cases {
		got := UntilInSeriesForm(c.until, c.loc, c.allDay)
		if got == nil || *got != c.want {
			t.Errorf("%s: got %v, want %s", c.name, got, c.want)
		}
	}
	if UntilInSeriesForm(nil, time.UTC, true) != nil {
		t.Error("no until stays no until")
	}
}
