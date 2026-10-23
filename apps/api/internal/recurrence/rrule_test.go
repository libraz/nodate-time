package recurrence

import (
	"reflect"
	"testing"
	"time"
)

func occurrenceDays(occ []Occurrence) []string {
	out := make([]string, 0, len(occ))
	for _, o := range occ {
		out = append(out, o.StartAt.Format("2006-01-02"))
	}
	return out
}

// A day the month does not have is clamped by Expand and skipped by RFC 5545,
// so the RRULE crossing the boundary has to be the RFC form that clamps. Each
// want is the RFC expansion of the RRULE, worked out by hand.
func TestClampedDaysCrossTheICalBoundaryUnchanged(t *testing.T) {
	cases := []struct {
		name  string
		rule  Rule
		start time.Time
		rrule string
		want  []string
	}{
		{"monthly on the 31st", Rule{Freq: "monthly", Interval: 1},
			time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC),
			"FREQ=MONTHLY;BYMONTHDAY=-1;WKST=SU",
			[]string{"2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30", "2026-05-31", "2026-06-30"}},
		{"monthly byMonthDay 31", Rule{Freq: "monthly", Interval: 1, ByMonthDay: 31},
			time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC),
			"FREQ=MONTHLY;BYMONTHDAY=-1;WKST=SU",
			[]string{"2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30", "2026-05-31", "2026-06-30"}},
		{"monthly on the 30th", Rule{Freq: "monthly", Interval: 1},
			time.Date(2026, 1, 30, 10, 0, 0, 0, time.UTC),
			"FREQ=MONTHLY;BYMONTHDAY=28,29,30;BYSETPOS=-1;WKST=SU",
			[]string{"2026-01-30", "2026-02-28", "2026-03-30", "2026-04-30", "2026-05-30", "2026-06-30"}},
		{"monthly on the 29th", Rule{Freq: "monthly", Interval: 1, ByMonthDay: 29},
			time.Date(2026, 1, 29, 10, 0, 0, 0, time.UTC),
			"FREQ=MONTHLY;BYMONTHDAY=28,29;BYSETPOS=-1;WKST=SU",
			[]string{"2026-01-29", "2026-02-28", "2026-03-29", "2026-04-29", "2026-05-29", "2026-06-29"}},
		{"yearly on Feb 29", Rule{Freq: "yearly", Interval: 1},
			time.Date(2028, 2, 29, 10, 0, 0, 0, time.UTC),
			"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=28,29;BYSETPOS=-1;WKST=SU",
			[]string{"2028-02-29", "2029-02-28", "2030-02-28", "2031-02-28", "2032-02-29", "2033-02-28"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			end := c.start.Add(time.Hour)
			windowEnd := c.start.AddDate(6, 0, 0)
			got := occurrenceDays(Expand(&c.rule, c.start, end, c.start, windowEnd))
			if len(got) > len(c.want) {
				got = got[:len(c.want)]
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Expand = %v, want the RFC set %v", got, c.want)
			}
			if rrule := FormatRRule(&c.rule, c.start, false); rrule != c.rrule {
				t.Fatalf("FormatRRule = %q, want %q", rrule, c.rrule)
			}
			back, ok := ParseRRule(c.rrule, c.start)
			if !ok {
				t.Fatalf("ParseRRule(%q) refused the form it writes", c.rrule)
			}
			again := occurrenceDays(Expand(back, c.start, end, c.start, windowEnd))
			if len(again) > len(c.want) {
				again = again[:len(c.want)]
			}
			if !reflect.DeepEqual(again, c.want) {
				t.Fatalf("imported rule expands to %v, want %v", again, c.want)
			}
		})
	}
}

// The plain RFC forms of those days skip short months, which Expand cannot
// do, so they are refused rather than imported as a different series.
func TestRRuleThatSkipsShortMonthsIsRefused(t *testing.T) {
	jan31 := time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC)
	feb29 := time.Date(2028, 2, 29, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		rrule string
		start time.Time
	}{
		{"FREQ=MONTHLY", jan31},
		{"FREQ=MONTHLY;BYMONTHDAY=31", jan31},
		{"FREQ=MONTHLY;BYMONTHDAY=30", jan31},
		{"FREQ=MONTHLY;BYMONTHDAY=29", jan31},
		{"FREQ=MONTHLY;BYMONTHDAY=-2", jan31},
		{"FREQ=MONTHLY;BYMONTHDAY=28,30;BYSETPOS=-1", jan31},
		{"FREQ=YEARLY", feb29},
		{"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=29", feb29},
		{"FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=28,29;BYSETPOS=-1", feb29},
	} {
		if _, ok := ParseRRule(c.rrule, c.start); ok {
			t.Errorf("ParseRRule(%q, %s) accepted a rule Expand would clamp", c.rrule, c.start.Format("2006-01-02"))
		}
	}
	if _, ok := ParseRRule("FREQ=MONTHLY;BYMONTHDAY=28", jan31); !ok {
		t.Error("a day every month has must still import")
	}
}

// BYDAY=2MO is how most clients write the second Monday, and means exactly
// what BYDAY=MO;BYSETPOS=2 does.
func TestOrdinalWeekdayImportsAsTheSetPositionForm(t *testing.T) {
	monday := time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC) // the second Monday
	want, ok := ParseRRule("FREQ=MONTHLY;BYDAY=MO;BYSETPOS=2", monday)
	if !ok {
		t.Fatal("the set-position form must import")
	}
	for _, rrule := range []string{"FREQ=MONTHLY;BYDAY=2MO", "FREQ=MONTHLY;BYDAY=+2MO"} {
		got, ok := ParseRRule(rrule, monday)
		if !ok {
			t.Fatalf("ParseRRule(%q) refused", rrule)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ParseRRule(%q) = %+v, want %+v", rrule, got, want)
		}
	}
	for _, rrule := range []string{
		"FREQ=MONTHLY;BYDAY=-1FR",
		"FREQ=MONTHLY;BYDAY=1MO,3MO",
		"FREQ=MONTHLY;BYDAY=2MO;BYSETPOS=2",
		"FREQ=MONTHLY;BYDAY=6MO",
		"FREQ=WEEKLY;BYDAY=2MO",
		"FREQ=YEARLY;BYDAY=2MO",
	} {
		if _, ok := ParseRRule(rrule, monday); ok {
			t.Errorf("ParseRRule(%q) accepted an ordinal the expander cannot reproduce", rrule)
		}
	}
}

// RFC 5545 allows COUNT or UNTIL, never both. A rule stored before that was
// enforced is written with whichever one Expand stops at.
func TestRRuleCarriesOneEndCondition(t *testing.T) {
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	if _, ok := ParseRRule("FREQ=DAILY;COUNT=3;UNTIL=20260701T000000Z", start); ok {
		t.Error("an RRULE with COUNT and UNTIL must be refused")
	}
	far, near := "2026-07-01T00:00:00Z", "2026-06-03T09:00:00Z"
	countFirst := &Rule{Freq: "daily", Interval: 1, Count: 3, Until: &far}
	if got := FormatRRule(countFirst, start, false); got != "FREQ=DAILY;COUNT=3;WKST=SU" {
		t.Errorf("count reached first: got %q", got)
	}
	untilFirst := &Rule{Freq: "daily", Interval: 1, Count: 30, Until: &near}
	if got := FormatRRule(untilFirst, start, false); got != "FREQ=DAILY;UNTIL=20260603T090000Z;WKST=SU" {
		t.Errorf("until reached first: got %q", got)
	}
}
