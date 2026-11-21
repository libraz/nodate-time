package recurrence

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

const fixturesPath = "../../../../sql/core/recurrence/fixtures.json"

type fixture struct {
	Name    string          `json:"name"`
	Rule    json.RawMessage `json:"rule"`
	DTStart string          `json:"dtstart"`
	TZ      string          `json:"tz"`
	AllDay  bool            `json:"allDay"`
	Expect  []string        `json:"expect"`
}

// Every case in the shared fixtures must expand to exactly its expect list,
// wall clock and offset included. A rule with neither count nor until is
// unbounded, so only its first len(expect) occurrences are compared. An
// all-day series expands in UTC whatever tz says (PROTOCOL.md §Recurrence).
func TestFixtures(t *testing.T) {
	raw, err := os.ReadFile(fixturesPath)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var cases []fixture
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	if len(cases) != 19 {
		t.Fatalf("fixtures hold %d cases, want 19", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			zone := ExpansionZone(c.AllDay, c.TZ)
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Skipf("tz database unavailable: %v", err)
			}
			rule, err := ParseRule(c.Rule)
			if err != nil || rule == nil {
				t.Fatalf("ParseRule: %v", err)
			}
			start, err := time.Parse(time.RFC3339, c.DTStart)
			if err != nil {
				t.Fatalf("dtstart: %v", err)
			}
			start = start.In(loc)
			if err := rule.Validate(start); err != nil {
				t.Fatalf("Validate: %v", err)
			}

			windowEnd := start.AddDate(30, 0, 0)
			occ := ExpandInZone(rule, start, start.Add(time.Hour), start, windowEnd, zone)
			bounded := rule.Count > 0 || rule.Until != nil
			if bounded && len(occ) != len(c.Expect) {
				t.Fatalf("expanded %d occurrences, want %d", len(occ), len(c.Expect))
			}
			if len(occ) < len(c.Expect) {
				t.Fatalf("expanded %d occurrences, want at least %d", len(occ), len(c.Expect))
			}
			for i, want := range c.Expect {
				if got := occ[i].StartAt.In(loc).Format(time.RFC3339); got != want {
					t.Errorf("occurrence %d = %s, want %s", i+1, got, want)
				}
			}
		})
	}
}
