package events

import (
	"encoding/json"
	"testing"
	"time"
)

func rawRule(s string) *json.RawMessage {
	m := json.RawMessage(s)
	return &m
}

// The rule combinations themselves are covered by recurrence.Rule.Validate;
// this checks what the request boundary adds: an absent rule, a body that is
// not a rule at all, and the series start the rule is validated against.
func TestCheckRecurrenceRule(t *testing.T) {
	start := time.Date(2026, 4, 3, 6, 0, 0, 0, time.UTC) // 15:00 in Tokyo
	tests := []struct {
		name    string
		rule    *json.RawMessage
		wantErr bool
	}{
		{"nil rule ok", nil, false},
		{"null rule ok", rawRule(`null`), false},
		{"valid daily", rawRule(`{"freq":"daily","interval":1}`), false},
		{"absent interval read as one", rawRule(`{"freq":"daily"}`), false},
		{"interval zero refused", rawRule(`{"freq":"daily","interval":0}`), true},
		{"scalar byMonthDay refused", rawRule(`{"freq":"monthly","byMonthDay":5}`), true},
		{"not an object rejected", rawRule(`"daily"`), true},
		{"unknown freq rejected", rawRule(`{"freq":"Daily","interval":1}`), true},
		{"field the expander ignores rejected", rawRule(`{"freq":"daily","byDay":["MO"]}`), true},
		{"until on the start day in the event zone ok", rawRule(`{"freq":"daily","until":"2026-04-03"}`), false},
		{"until the day before the start rejected", rawRule(`{"freq":"daily","until":"2026-04-02"}`), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkRecurrenceRule(tt.rule, start, "Asia/Tokyo")
			if (got != nil) != tt.wantErr {
				t.Fatalf("checkRecurrenceRule() = %v, wantErr = %v", got, tt.wantErr)
			}
		})
	}
}
