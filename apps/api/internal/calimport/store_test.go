package calimport

import (
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
	recurring := syncEvent()
	recurring.RRule, recurring.Exdates = "FREQ=DAILY;COUNT=3", nil
	if h := mustHead(t, recurring); h.exceptions != nil {
		t.Errorf("a series imported without EXDATE must be NULL, got %s", *h.exceptions)
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
