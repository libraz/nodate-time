package recurrence

import (
	"testing"
	"time"
)

func TestRuleValidate(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	// Friday 2026-04-03 15:00 in the event's zone.
	start := time.Date(2026, 4, 3, 15, 0, 0, 0, tokyo)
	until := func(s string) *string { return &s }

	tests := []struct {
		name    string
		rule    Rule
		wantErr bool
	}{
		{"daily", Rule{Freq: "daily", Interval: 1}, false},
		{"weekly byDay", Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO", "WE"}}, false},
		{"monthly nth weekday", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"MO"}, BySetPos: []int{2}}, false},
		{"monthly day of month", Rule{Freq: "monthly", Interval: 1, ByMonthDay: []int{15}}, false},
		{"yearly", Rule{Freq: "yearly", Interval: 1}, false},
		{"unknown freq", Rule{Freq: "Daily", Interval: 1}, true},
		{"empty freq", Rule{Freq: "", Interval: 1}, true},
		{"interval above 999", Rule{Freq: "daily", Interval: 1000}, true},
		{"count above 1000", Rule{Freq: "daily", Interval: 1, Count: 5000}, true},
		{"wkst SU", Rule{Freq: "weekly", Interval: 2, ByDay: []string{"TU"}, Wkst: "SU"}, false},
		{"wkst other than MO or SU", Rule{Freq: "weekly", Interval: 2, ByDay: []string{"TU"}, Wkst: "WE"}, true},
		{"interval zero", Rule{Freq: "daily"}, true},
		{"repeated byDay", Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO", "MO"}}, true},
		{"repeated byMonthDay", Rule{Freq: "monthly", Interval: 1, ByMonthDay: []int{3, 3}}, true},
		{"monthly several days of the month", Rule{Freq: "monthly", Interval: 1, ByMonthDay: []int{5, 20}}, false},
		{"bySetPos last", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"FR"}, BySetPos: []int{-1}}, false},
		{"bySetPos two positions", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"FR"}, BySetPos: []int{1, 2}}, true},
		{"bySetPos zero", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"FR"}, BySetPos: []int{0}}, true},
		{"unknown weekday", Rule{Freq: "weekly", Interval: 1, ByDay: []string{"XX"}}, true},
		{"byMonthDay out of range", Rule{Freq: "monthly", Interval: 1, ByMonthDay: []int{40}}, true},
		{"monthly byDay without bySetPos", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"MO"}}, true},
		{"monthly bySetPos without byDay", Rule{Freq: "monthly", Interval: 1, BySetPos: []int{2}}, true},
		{"monthly bySetPos with two byDay", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"MO", "TU"}, BySetPos: []int{2}}, true},
		{"monthly bySetPos with byMonthDay", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"MO"}, BySetPos: []int{2}, ByMonthDay: []int{3}}, true},
		{"daily byDay", Rule{Freq: "daily", Interval: 1, ByDay: []string{"MO"}}, true},
		{"daily byMonthDay", Rule{Freq: "daily", Interval: 1, ByMonthDay: []int{3}}, true},
		{"yearly byDay", Rule{Freq: "yearly", Interval: 1, ByDay: []string{"MO"}}, true},
		{"yearly bySetPos", Rule{Freq: "yearly", Interval: 1, BySetPos: []int{1}}, true},
		{"weekly byMonthDay", Rule{Freq: "weekly", Interval: 1, ByMonthDay: []int{3}}, true},
		{"weekly bySetPos", Rule{Freq: "weekly", Interval: 1, BySetPos: []int{1}}, true},
		{"unparseable until", Rule{Freq: "daily", Interval: 1, Until: until("not-a-date")}, true},
		{"until date after start", Rule{Freq: "daily", Interval: 1, Until: until("2026-12-31")}, false},
		{"until date on the start day", Rule{Freq: "daily", Interval: 1, Until: until("2026-04-03")}, false},
		{"until date before the start day", Rule{Freq: "daily", Interval: 1, Until: until("2026-04-02")}, true},
		{"until instant equal to start", Rule{Freq: "daily", Interval: 1, Until: until("2026-04-03T15:00:00+09:00")}, false},
		{"until instant earlier the same day", Rule{Freq: "daily", Interval: 1, Until: until("2026-04-03T14:59:00+09:00")}, true},
		{"count and until together", Rule{Freq: "daily", Interval: 1, Count: 3, Until: until("2026-12-31")}, true},
		// The start is a Friday, the 3rd, and the first Friday of April. It is
		// occurrence #1 whether or not the rule's own days would produce it.
		{"weekly byDay start off the rule, until before the rule's first day", Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO"}, Until: until("2026-04-05")}, false},
		{"weekly byDay reached before until", Rule{Freq: "weekly", Interval: 1, ByDay: []string{"MO"}, Until: until("2026-04-06")}, false},
		{"monthly day already past, until this month", Rule{Freq: "monthly", Interval: 1, ByMonthDay: []int{2}, Until: until("2026-04-30")}, false},
		{"monthly day already past, until next month", Rule{Freq: "monthly", Interval: 1, ByMonthDay: []int{2}, Until: until("2026-05-02")}, false},
		{"monthly nth weekday already past, until this month", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"WE"}, BySetPos: []int{1}, Until: until("2026-04-30")}, false},
		{"monthly nth weekday later this month", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"FR"}, BySetPos: []int{2}, Until: until("2026-04-10")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate(start)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// An absent interval reads as one; an interval that is present is kept as
// written, so a zero reaches Validate and is refused rather than stored for the
// table's CHECK to reject.
func TestParseRuleIntervalDefaultsOnlyWhenAbsent(t *testing.T) {
	start := time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)
	r, err := ParseRule([]byte(`{"freq":"daily"}`))
	if err != nil || r == nil || r.Interval != 1 || r.Validate(start) != nil {
		t.Fatalf("absent interval = %+v, %v; want interval 1 and valid", r, err)
	}
	r, err = ParseRule([]byte(`{"freq":"daily","interval":0}`))
	if err != nil || r == nil || r.Validate(start) == nil {
		t.Fatalf("interval 0 = %+v, %v; want a rule Validate refuses", r, err)
	}
}

// Every rule Validate accepts expands to at least its first occurrence.
func TestValidatedRuleProducesItsFirstOccurrence(t *testing.T) {
	start := time.Date(2026, 4, 3, 15, 0, 0, 0, time.UTC)
	until := "2026-04-03T15:00:00Z"
	r := &Rule{Freq: "daily", Interval: 1, Until: &until}
	if err := r.Validate(start); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !OccursAt(r, start, start.Add(time.Hour), "UTC", start) {
		t.Fatal("a series whose until equals its start must still occur at its start")
	}
}

// Whatever rule Validate accepts expands to at least one occurrence between
// its start and the end ComputeEnd records for it, so the event shows up
// somewhere.
func TestEveryValidatedRuleHasAnOccurrence(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	until := func(s string) *string { return &s }
	var rules []Rule
	for _, interval := range []int{1, 2} {
		rules = append(rules,
			Rule{Freq: "daily", Interval: interval},
			Rule{Freq: "yearly", Interval: interval},
			Rule{Freq: "monthly", Interval: interval},
		)
		for _, day := range []string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"} {
			rules = append(rules, Rule{Freq: "weekly", Interval: interval, ByDay: []string{day}})
			for pos := 1; pos <= 5; pos++ {
				rules = append(rules, Rule{Freq: "monthly", Interval: interval, ByDay: []string{day}, BySetPos: []int{pos}})
			}
		}
		for day := 1; day <= 31; day++ {
			rules = append(rules, Rule{Freq: "monthly", Interval: interval, ByMonthDay: []int{day}})
		}
	}
	ends := []func(Rule) Rule{
		func(r Rule) Rule { return r },
		func(r Rule) Rule { r.Count = 1; return r },
		func(r Rule) Rule { r.Until = until("2026-04-03"); return r },
		func(r Rule) Rule { r.Until = until("2026-04-09"); return r },
		func(r Rule) Rule { r.Until = until("2026-04-30"); return r },
		func(r Rule) Rule { r.Until = until("2026-05-20T00:00:00Z"); return r },
	}
	start := time.Date(2026, 4, 3, 15, 0, 0, 0, tokyo)
	end := start.Add(time.Hour)
	accepted := 0
	for _, base := range rules {
		for _, withEnd := range ends {
			r := withEnd(base)
			if r.Validate(start) != nil {
				continue
			}
			accepted++
			last := ComputeEnd(&r, start, end)
			if len(Expand(&r, start, end, start, last.Add(time.Second))) == 0 {
				t.Errorf("validated rule %+v (until %v) expands to nothing", r, r.Until)
			}
		}
	}
	if accepted == 0 {
		t.Fatal("no rule was accepted, so nothing was checked")
	}
}
