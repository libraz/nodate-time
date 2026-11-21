package calimport

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/attendees"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

const syncActor = 7

// syncEvent is a weekly series with cancellations, an owner and attendees.
func syncEvent() Event {
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	return Event{
		SourceUID: "tt-1", Title: "standup", Location: "room", Memo: "notes", URL: "https://example.com",
		Start: start, End: start.Add(time.Hour), Timezone: "Asia/Tokyo",
		Visibility: generated.CalendarEventsVisibilityDefault, ShowAs: generated.CalendarEventsShowAsBusy,
		RRule:       "FREQ=WEEKLY;COUNT=5",
		Exdates:     []time.Time{start.AddDate(0, 0, 7), start.AddDate(0, 0, 14)},
		OwnerUserID: 3,
		Attendees:   []attendees.Person{{UserID: 3}, {UserID: 4}},
	}
}

// stored is the row an insert of h writes, read back.
func stored(t *testing.T, h head) generated.CalendarEvent {
	t.Helper()
	p := h.createParams([]byte("0123456789abcdef"), 1, 2, syncActor, sql.NullString{String: "tt-1", Valid: true})
	return generated.CalendarEvent{
		ID: 10, PublicID: p.PublicID, WorkspaceID: p.WorkspaceID, CalendarID: p.CalendarID,
		Kind: p.Kind, Visibility: p.Visibility, ShowAs: p.ShowAs, Flexibility: p.Flexibility,
		Title: p.Title, AllDay: p.AllDay, StartAt: p.StartAt, EndAt: p.EndAt, Timezone: p.Timezone,
		Location: p.Location, Memo: p.Memo, URL: p.URL, SourceUid: p.SourceUid, SourceUidKey: p.SourceUid,
		OwnerUserID: p.OwnerUserID, CreatedByUserID: p.CreatedByUserID,
		NotificationOffset: p.NotificationOffset, RecurrenceRule: p.RecurrenceRule,
		RecurrenceEnd: p.RecurrenceEnd, RecurrenceExceptions: h.exceptions,
	}
}

func mustHead(t *testing.T, e Event) head {
	t.Helper()
	h, outcome := buildHead(e, syncActor)
	if outcome != importCreated {
		t.Fatalf("buildHead outcome = %d", outcome)
	}
	return h
}

func raw(s string) *json.RawMessage {
	r := json.RawMessage(s)
	return &r
}

func TestSyncDiff(t *testing.T) {
	base := syncEvent()
	h := mustHead(t, base)
	ids := userIDs(h.attendees)
	if diff(stored(t, h), h, ids, 0) {
		t.Fatal("a row read back from its own insert must not differ")
	}

	t.Run("one column differs", func(t *testing.T) {
		cases := map[string]func(*Event){
			"title":    func(e *Event) { e.Title = "retro" },
			"all_day":  func(e *Event) { e.AllDay = true },
			"location": func(e *Event) { e.Location = "hall" },
			"memo":     func(e *Event) { e.Memo = "other" },
			"url":      func(e *Event) { e.URL = "https://example.org" },
			"tz":       func(e *Event) { e.Timezone = "Europe/Paris" },
			"owner":    func(e *Event) { e.OwnerUserID = 5 },
			"start":    func(e *Event) { e.Start = e.Start.Add(-time.Minute) },
			"end":      func(e *Event) { e.End = e.End.Add(time.Minute) },
			"rule":     func(e *Event) { e.RRule = "FREQ=DAILY;COUNT=5" },
			"exceptions": func(e *Event) {
				e.Exdates = e.Exdates[:1]
			},
			"attendees": func(e *Event) { e.Attendees = e.Attendees[:1] },
		}
		for name, change := range cases {
			t.Run(name, func(t *testing.T) {
				e := syncEvent()
				change(&e)
				if !diff(stored(t, h), mustHead(t, e), ids, 0) {
					t.Errorf("a changed %s must differ", name)
				}
			})
		}
		t.Run("recEnd", func(t *testing.T) {
			row := stored(t, h)
			row.RecurrenceEnd.Time = row.RecurrenceEnd.Time.AddDate(0, 0, 7)
			if !diff(row, h, ids, 0) {
				t.Error("a changed recurrence end must differ")
			}
		})
		t.Run("overrides", func(t *testing.T) {
			if !diff(stored(t, h), h, ids, 1) {
				t.Error("a row with an override must differ")
			}
		})
	})

	t.Run("normalised", func(t *testing.T) {
		cases := map[string]func(*generated.CalendarEvent){
			"sub-millisecond": func(r *generated.CalendarEvent) {
				r.StartAt.Time = r.StartAt.Time.Add(400 * time.Microsecond)
				r.EndAt.Time = r.EndAt.Time.In(time.FixedZone("x", 3600))
			},
			"rule key order": func(r *generated.CalendarEvent) {
				// A map marshals in sorted key order, unlike the struct.
				var m map[string]any
				if err := json.Unmarshal(*r.RecurrenceRule, &m); err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				if string(b) == string(*r.RecurrenceRule) {
					t.Fatal("reordering left the rule unchanged")
				}
				r.RecurrenceRule = raw(string(b))
			},
			"exception order": func(r *generated.CalendarEvent) {
				var list []string
				if err := json.Unmarshal(*r.RecurrenceExceptions, &list); err != nil {
					t.Fatal(err)
				}
				list[0], list[1] = list[1], list[0]
				b, _ := json.Marshal(list)
				r.RecurrenceExceptions = raw(string(b))
			},
		}
		for name, change := range cases {
			t.Run(name, func(t *testing.T) {
				row := stored(t, h)
				change(&row)
				if diff(row, h, ids, 0) {
					t.Errorf("%s must not differ", name)
				}
			})
		}
		t.Run("empty exceptions", func(t *testing.T) {
			e := syncEvent()
			e.Exdates = nil
			bare := mustHead(t, e)
			row := stored(t, bare)
			row.RecurrenceExceptions = raw("[]")
			if diff(row, bare, ids, 0) {
				t.Error("NULL and [] exceptions must not differ")
			}
		})
		t.Run("empty location", func(t *testing.T) {
			e := syncEvent()
			e.Location = ""
			bare := mustHead(t, e)
			row := stored(t, bare)
			row.Location = sql.NullString{Valid: true}
			if diff(row, bare, ids, 0) {
				t.Error("NULL and empty location must not differ")
			}
		})
	})

	t.Run("rule on to off", func(t *testing.T) {
		e := syncEvent()
		e.RRule = ""
		if !diff(stored(t, h), mustHead(t, e), ids, 0) {
			t.Error("dropping the rule must differ")
		}
	})
	t.Run("rule off to on", func(t *testing.T) {
		e := syncEvent()
		e.RRule, e.Exdates = "", nil
		if !diff(stored(t, mustHead(t, e)), h, ids, 0) {
			t.Error("adding the rule must differ")
		}
	})
	t.Run("one-off with exdates", func(t *testing.T) {
		e := syncEvent()
		e.RRule = ""
		single := mustHead(t, e)
		if single.exceptions != nil {
			t.Fatal("a one-off event must carry no exceptions column")
		}
		if diff(stored(t, single), single, ids, 0) {
			t.Error("exdates on a one-off event must not differ")
		}
	})
}
