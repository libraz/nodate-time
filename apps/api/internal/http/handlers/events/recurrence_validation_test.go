package events

import (
	"encoding/json"
	"testing"
)

func rawRule(s string) *json.RawMessage {
	m := json.RawMessage(s)
	return &m
}

func TestValidateRecurrenceRule(t *testing.T) {
	tests := []struct {
		name    string
		rule    *json.RawMessage
		wantErr bool
	}{
		{"nil rule ok", nil, false},
		{"null rule ok", rawRule(`null`), false},
		{"valid daily", rawRule(`{"freq":"daily","interval":1}`), false},
		{"valid weekly byDay", rawRule(`{"freq":"weekly","interval":1,"byDay":["MO","WE"]}`), false},
		{"valid monthly bySetPos", rawRule(`{"freq":"monthly","interval":1,"byDay":["MO"],"bySetPos":2}`), false},
		{"unknown freq rejected", rawRule(`{"freq":"Daily","interval":1}`), true},
		{"empty freq rejected", rawRule(`{"freq":"","interval":1}`), true},
		{"interval zero rejected", rawRule(`{"freq":"daily","interval":0}`), false}, // ParseRule clamps interval<1 to 1
		{"interval too large rejected", rawRule(`{"freq":"daily","interval":1000}`), true},
		{"bad byDay rejected", rawRule(`{"freq":"weekly","byDay":["XX"]}`), true},
		{"byMonthDay out of range rejected", rawRule(`{"freq":"monthly","byMonthDay":40}`), true},
		{"monthly byDay without bySetPos rejected", rawRule(`{"freq":"monthly","byDay":["MO"]}`), true},
		{"unparseable until rejected", rawRule(`{"freq":"daily","until":"not-a-date"}`), true},
		{"valid until date", rawRule(`{"freq":"daily","until":"2025-12-31"}`), false},
		{"count too large rejected", rawRule(`{"freq":"daily","count":5000}`), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateRecurrenceRule(tt.rule)
			if (got != nil) != tt.wantErr {
				t.Fatalf("validateRecurrenceRule() = %v, wantErr = %v", got, tt.wantErr)
			}
		})
	}
}
