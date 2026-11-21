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
		data, ok := convertRRule(c.rule, c.start, false)
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

func TestExceptionListWritesTheSeriesForm(t *testing.T) {
	day := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	instant := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	allDay, err := exceptionList([]time.Time{day, day}, true, "America/Los_Angeles").MarshalColumn()
	if err != nil || allDay == nil || string(*allDay) != `["2026-07-11"]` {
		t.Errorf("all-day = %v, %v; want one date", allDay, err)
	}
	timed, err := exceptionList([]time.Time{instant}, false, "Asia/Tokyo").MarshalColumn()
	if err != nil || timed == nil || string(*timed) != `["2026-07-11T00:00:00Z"]` {
		t.Errorf("timed = %v, %v; want a UTC instant", timed, err)
	}
	if none, _ := exceptionList(nil, true, "UTC").MarshalColumn(); none != nil {
		t.Errorf("no exceptions must be NULL, got %s", *none)
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
		got := untilInSeriesForm(c.until, c.loc, c.allDay)
		if got == nil || *got != c.want {
			t.Errorf("%s: got %v, want %s", c.name, got, c.want)
		}
	}
	if untilInSeriesForm(nil, time.UTC, true) != nil {
		t.Error("no until stays no until")
	}
}

func TestAllDayEndDefaultsAndOrder(t *testing.T) {
	day := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		end     time.Time
		wantEnd time.Time
		wantOK  bool
	}{
		{"no end is a day", time.Time{}, day.AddDate(0, 0, 1), true},
		{"end on the start day is a day", day, day.AddDate(0, 0, 1), true},
		{"end before the start is refused", day.AddDate(0, 0, -1), time.Time{}, false},
		{"a range is kept", day.AddDate(0, 0, 3), day.AddDate(0, 0, 3), true},
	}
	for _, c := range cases {
		end, _, ok := validateImported(Event{Title: "x", Start: day, End: c.end, AllDay: true})
		if ok != c.wantOK || (ok && !end.Equal(c.wantEnd)) {
			t.Errorf("%s: end %s ok %v, want %s %v", c.name, end, ok, c.wantEnd, c.wantOK)
		}
	}
}
