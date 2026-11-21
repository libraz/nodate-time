package recurrence

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

const exceptionFormsPath = "../../../../sql/core/recurrence/exception-forms.json"

type exceptionForms struct {
	Series map[string]struct {
		AllDay  bool            `json:"allDay"`
		Rule    json.RawMessage `json:"rule"`
		DTStart string          `json:"dtstart"`
		TZ      string          `json:"tz"`
	} `json:"series"`
	Cases []struct {
		Element string             `json:"element"`
		Form    string             `json:"form"`
		Cancels map[string]*string `json:"cancels"`
	} `json:"cases"`
}

// Every element of the shared table cancels exactly the occurrence it names
// on the all-day and the timed sample series, and nothing when it names none.
func TestExceptionForms(t *testing.T) {
	raw, err := os.ReadFile(exceptionFormsPath)
	if err != nil {
		t.Fatal(err)
	}
	var forms exceptionForms
	if err := json.Unmarshal(raw, &forms); err != nil {
		t.Fatal(err)
	}
	for kind, s := range forms.Series {
		rule, err := ParseRule(s.Rule)
		if err != nil || rule == nil {
			t.Fatalf("%s rule: %v", kind, err)
		}
		start, err := time.Parse(time.RFC3339, s.DTStart)
		if err != nil {
			t.Fatal(err)
		}
		zone := ExpansionZone(s.AllDay, s.TZ)
		loc := LoadLocation(zone)
		start = start.In(loc)
		occ := ExpandInZone(rule, start, start.Add(time.Hour), start, start.AddDate(1, 0, 0), zone)
		if len(occ) == 0 {
			t.Fatalf("%s: no occurrences", kind)
		}
		for _, c := range forms.Cases {
			t.Run(kind+"/"+c.Element, func(t *testing.T) {
				msg, _ := json.Marshal([]string{c.Element})
				col := json.RawMessage(msg)
				list := ParseExceptionList(&col)
				want := c.Cancels[kind]
				var got []string
				for _, o := range occ {
					if list.Cancels(o.StartAt, s.AllDay, s.TZ) {
						got = append(got, o.StartAt.UTC().Format(time.RFC3339))
					}
				}
				switch {
				case want == nil && len(got) != 0:
					t.Errorf("cancels %v, want nothing", got)
				case want != nil && (len(got) != 1 || got[0] != *want):
					t.Errorf("cancels %v, want [%s]", got, *want)
				}
			})
		}
	}
}

func TestExceptionListKeepsUnreadableElements(t *testing.T) {
	col := json.RawMessage(`["garbage","2026-03-10T00:00:00z",7,"2026-03-10"]`)
	list := ParseExceptionList(&col)
	start := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

	out, err := list.Without(start, true, "UTC").MarshalColumn()
	if err != nil {
		t.Fatal(err)
	}
	if want := `["garbage","2026-03-10T00:00:00z",7]`; string(*out) != want {
		t.Errorf("after Without = %s, want %s", *out, want)
	}

	other := time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC)
	out, err = list.With(other, true, "UTC").MarshalColumn()
	if err != nil {
		t.Fatal(err)
	}
	if want := `["garbage","2026-03-10T00:00:00z",7,"2026-03-10","2026-03-11"]`; string(*out) != want {
		t.Errorf("after With = %s, want %s", *out, want)
	}
}

func TestExceptionListWritesSeriesForm(t *testing.T) {
	start := time.Date(2026, 3, 9, 23, 0, 0, 0, time.UTC)
	timed, _ := ExceptionList(nil).With(start, false, "Asia/Tokyo").MarshalColumn()
	if want := `["2026-03-09T23:00:00Z"]`; string(*timed) != want {
		t.Errorf("timed = %s, want %s", *timed, want)
	}
	allDay, _ := ExceptionList(nil).With(start.Add(time.Hour), true, "Asia/Tokyo").MarshalColumn()
	if want := `["2026-03-10"]`; string(*allDay) != want {
		t.Errorf("all-day = %s, want %s", *allDay, want)
	}
}

func TestExpansionZone(t *testing.T) {
	if got := ExpansionZone(true, "America/Los_Angeles"); got != "UTC" {
		t.Errorf("all-day = %q, want UTC", got)
	}
	if got := ExpansionZone(false, "Asia/Tokyo"); got != "Asia/Tokyo" {
		t.Errorf("timed = %q, want Asia/Tokyo", got)
	}
}

// A list edited down to nothing is stored as [], the form the other writer of
// the column leaves; only a series that never had a cancellation is NULL.
func TestExceptionListEmptiedByAnEditIsAnEmptyArray(t *testing.T) {
	col := json.RawMessage(`["2026-03-10"]`)
	start := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	out, err := ParseExceptionList(&col).Without(start, true, "UTC").MarshalColumn()
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || string(*out) != `[]` {
		t.Errorf("emptied list = %v, want []", out)
	}
}
