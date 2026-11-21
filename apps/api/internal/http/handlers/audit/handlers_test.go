package audit

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A cursor is a value handed to a client, so what matters is what it says
// rather than that it round-trips: it names the row and nothing else.
func TestActivityCursorNamesTheRowAndNothingElse(t *testing.T) {
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("new uuid: %v", err)
	}
	cursor := encodeActivityCursor(id[:])
	if cursor != id.String() {
		t.Fatalf("cursor = %q, want %q", cursor, id.String())
	}
	if _, err := uuid.Parse(cursor); err != nil {
		t.Fatalf("cursor is not a public id: %v", err)
	}
}

// The internal id is a single deployment-wide sequence. A cursor that is a
// decimal number, however it is wrapped, tells its holder how much the whole
// instance has written.
func TestActivityCursorCarriesNoInternalNumber(t *testing.T) {
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("new uuid: %v", err)
	}
	cursor := encodeActivityCursor(id[:])
	if _, err := strconv.ParseUint(cursor, 10, 64); err == nil {
		t.Fatalf("cursor %q reads as a number", cursor)
	}
}

func TestEntityTypeOf(t *testing.T) {
	cases := map[string]string{
		"calendar.created":                  "calendar",
		"calendar.reminder":                 "calendar",
		"calendar.event.created":            "event",
		"calendar.event.comment.created":    "comment",
		"calendar.event.checklist.updated":  "checklist",
		"calendar.event.attachment.deleted": "attachment",
		"calendar.event.attendee.added":     "attendee",
		"calendar.event.rsvp.updated":       "rsvp",
		"calendar.event.invite.rotated":     "invite",
		"calendar.member.role_changed":      "member",
		"calendar.subscription.updated":     "subscription",
		"workspace.member.added":            "member",
	}
	for kind, want := range cases {
		if got := entityTypeOf(kind); got != want {
			t.Errorf("entityTypeOf(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestParseOriginalStart(t *testing.T) {
	got, ok := parseOriginalStart("2026-07-06T01:00:00+09:00")
	if !ok || !got.Equal(time.Date(2026, 7, 5, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("instant = %v, %v", got, ok)
	}
	got, ok = parseOriginalStart("2026-07-06")
	if !ok || !got.Equal(time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("date = %v, %v", got, ok)
	}
	for _, bad := range []string{"", "yesterday", "20260706"} {
		if _, ok := parseOriginalStart(bad); ok {
			t.Errorf("parseOriginalStart(%q) accepted", bad)
		}
	}
}

// A row the reader sees in part keeps the event's title and nothing else the
// writer labelled it with, since the label can carry what was changed.
func TestMaskedRowKeepsOnlyTheTitle(t *testing.T) {
	payload := json.RawMessage(`{"title":"Standup","summary":"location: Room 4","originalStart":"2026-07-06T01:00:00Z"}`)
	row := shownEntry{logEntry: logEntry{PublicID: make([]byte, 16), Type: "calendar.event.updated", Payload: payload}}

	full := historyItem(context.Background(), nil, row)
	if full.Summary != "location: Room 4" || full.OriginalStart != "2026-07-06T01:00:00Z" {
		t.Fatalf("unmasked = %+v", full)
	}
	row.masked = true
	masked := historyItem(context.Background(), nil, row)
	if masked.Summary != "Standup" {
		t.Fatalf("masked summary = %q, want the title", masked.Summary)
	}
}

func TestPageLimitAndBound(t *testing.T) {
	for in, want := range map[int]int{0: defaultActivityLimit, -3: defaultActivityLimit, 1: 1, 200: 200, 201: maxActivityLimit} {
		if got := pageLimit(in); got != want {
			t.Errorf("pageLimit(%d) = %d, want %d", in, got, want)
		}
	}
	if beforeBound(0) != math.MaxUint64 || beforeBound(7) != 7 {
		t.Fatalf("beforeBound does not map the first page to the top of the log")
	}
}

func TestOnlyEventKindsPassTheVisibilityRules(t *testing.T) {
	subject := make([]byte, 16)
	cases := []struct {
		row  logEntry
		want bool
	}{
		{logEntry{Type: "calendar.event.updated", SubjectPublicID: subject}, true},
		{logEntry{Type: "calendar.event.comment.created", SubjectPublicID: subject}, true},
		{logEntry{Type: "calendar.memo.updated", SubjectPublicID: subject}, false},
		{logEntry{Type: "calendar.member.added"}, false},
		{logEntry{Type: "calendar.event.updated"}, false},
	}
	for _, c := range cases {
		if got := isEventRow(c.row); got != c.want {
			t.Errorf("isEventRow(%q, subject %d bytes) = %v, want %v", c.row.Type, len(c.row.SubjectPublicID), got, c.want)
		}
	}
}
