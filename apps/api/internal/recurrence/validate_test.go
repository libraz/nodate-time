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
		{"monthly nth weekday", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"MO"}, BySetPos: 2}, false},
		{"monthly day of month", Rule{Freq: "monthly", Interval: 1, ByMonthDay: 15}, false},
		{"yearly", Rule{Freq: "yearly", Interval: 1}, false},
		{"unknown freq", Rule{Freq: "Daily", Interval: 1}, true},
		{"empty freq", Rule{Freq: "", Interval: 1}, true},
		{"interval above 999", Rule{Freq: "daily", Interval: 1000}, true},
		{"count above 1000", Rule{Freq: "daily", Interval: 1, Count: 5000}, true},
		{"unknown weekday", Rule{Freq: "weekly", Interval: 1, ByDay: []string{"XX"}}, true},
		{"byMonthDay out of range", Rule{Freq: "monthly", Interval: 1, ByMonthDay: 40}, true},
		{"monthly byDay without bySetPos", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"MO"}}, true},
		{"monthly bySetPos without byDay", Rule{Freq: "monthly", Interval: 1, BySetPos: 2}, true},
		{"monthly bySetPos with two byDay", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"MO", "TU"}, BySetPos: 2}, true},
		{"monthly bySetPos with byMonthDay", Rule{Freq: "monthly", Interval: 1, ByDay: []string{"MO"}, BySetPos: 2, ByMonthDay: 3}, true},
		{"daily byDay", Rule{Freq: "daily", Interval: 1, ByDay: []string{"MO"}}, true},
		{"daily byMonthDay", Rule{Freq: "daily", Interval: 1, ByMonthDay: 3}, true},
		{"yearly byDay", Rule{Freq: "yearly", Interval: 1, ByDay: []string{"MO"}}, true},
		{"yearly bySetPos", Rule{Freq: "yearly", Interval: 1, BySetPos: 1}, true},
		{"weekly byMonthDay", Rule{Freq: "weekly", Interval: 1, ByMonthDay: 3}, true},
		{"weekly bySetPos", Rule{Freq: "weekly", Interval: 1, BySetPos: 1}, true},
		{"unparseable until", Rule{Freq: "daily", Interval: 1, Until: until("not-a-date")}, true},
		{"until date after start", Rule{Freq: "daily", Interval: 1, Until: until("2026-12-31")}, false},
		{"until date on the start day", Rule{Freq: "daily", Interval: 1, Until: until("2026-04-03")}, false},
		{"until date before the start day", Rule{Freq: "daily", Interval: 1, Until: until("2026-04-02")}, true},
		{"until instant equal to start", Rule{Freq: "daily", Interval: 1, Until: until("2026-04-03T15:00:00+09:00")}, false},
		{"until instant earlier the same day", Rule{Freq: "daily", Interval: 1, Until: until("2026-04-03T14:59:00+09:00")}, true},
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

// An interval below one is read as one rather than refused: ParseRule clamps
// it before anything validates the rule.
func TestParseRuleClampsIntervalBelowOne(t *testing.T) {
	r := ParseRule([]byte(`{"freq":"daily","interval":0}`))
	if r == nil || r.Interval != 1 {
		t.Fatalf("ParseRule interval 0 = %+v, want interval 1", r)
	}
	if err := r.Validate(time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("clamped rule should validate: %v", err)
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
