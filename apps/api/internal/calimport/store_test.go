package calimport

import (
	"strings"
	"testing"
	"time"
)

func TestImportEndAtUsesDuration(t *testing.T) {
	start := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	timed := Event{Start: start, HasDuration: true, Duration: 3 * time.Hour}
	if end, ok := importEndAt(timed); !ok || !end.Equal(time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("timed end = %s %v, want 13:00Z", end, ok)
	}
	allDay := Event{Start: day, AllDay: true, HasDuration: true, Duration: 72 * time.Hour}
	if end, ok := importEndAt(allDay); !ok || end.Format("20060102") != "20260604" {
		t.Errorf("all-day end = %s %v, want 20260604", end, ok)
	}
	bad := Event{Start: start, HasDuration: true, BadDuration: true}
	if _, ok := importEndAt(bad); ok {
		t.Error("an unreadable duration must be refused, not replaced by a default length")
	}
	partial := Event{Start: day, AllDay: true, HasDuration: true, Duration: 12 * time.Hour}
	if _, ok := importEndAt(partial); ok {
		t.Error("a part-day duration on an all-day event must be refused")
	}
	none := Event{Start: start}
	if end, ok := importEndAt(none); !ok || !end.Equal(time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("default end = %s %v", end, ok)
	}
}

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
		data, ok := convertRRule(c.rule, c.start)
		if ok != c.ok {
			t.Errorf("convertRRule(%q, %s) ok = %v, want %v", c.rule, c.start.Weekday(), ok, c.ok)
			continue
		}
		if ok && c.wkst != "" && !strings.Contains(string(*data), c.wkst) {
			t.Errorf("convertRRule(%q) = %s, want %s", c.rule, *data, c.wkst)
		}
		if ok && c.wkst == "" && strings.Contains(string(*data), "wkst") {
			t.Errorf("convertRRule(%q) = %s, want no wkst where it changes nothing", c.rule, *data)
		}
	}
}
