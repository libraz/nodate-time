package dav

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	goical "github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/libraz/nodate-time/apps/api/internal/ical"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// Pairwise model for the body-to-edit conversion (coverwise, strength 2,
// seed 7; 23 rows cover all 204 pairs):
//
//	form      timed | allday
//	recurring no | yes
//	override  none | changed | same | new
//	exdate    none | add | remove
//	valarm    expressible | unexpressible | actionNone | absent
//	class     absent | same | changed
//	grid      same | moved
//	masked    no | yes
//
//	constraint: IF recurring = no THEN override = none AND exdate = none
var diffRows = [][8]string{
	{"allday", "yes", "same", "add", "expressible", "absent", "same", "no"},
	{"timed", "no", "none", "none", "expressible", "changed", "moved", "yes"},
	{"allday", "no", "none", "none", "unexpressible", "absent", "same", "yes"},
	{"timed", "yes", "new", "remove", "actionNone", "same", "moved", "no"},
	{"allday", "no", "none", "none", "absent", "same", "same", "yes"},
	{"allday", "yes", "changed", "add", "unexpressible", "changed", "moved", "yes"},
	{"allday", "yes", "same", "remove", "absent", "absent", "moved", "yes"},
	{"allday", "yes", "none", "remove", "actionNone", "changed", "same", "no"},
	{"timed", "yes", "new", "add", "absent", "changed", "moved", "no"},
	{"timed", "yes", "new", "none", "actionNone", "absent", "same", "yes"},
	{"timed", "yes", "same", "none", "unexpressible", "same", "moved", "yes"},
	{"timed", "yes", "same", "none", "unexpressible", "changed", "moved", "no"},
	{"timed", "no", "none", "none", "actionNone", "absent", "same", "no"},
	{"timed", "yes", "changed", "remove", "absent", "absent", "same", "yes"},
	{"timed", "yes", "changed", "none", "absent", "changed", "moved", "no"},
	{"allday", "yes", "changed", "add", "actionNone", "same", "same", "no"},
	{"allday", "yes", "none", "add", "unexpressible", "absent", "moved", "yes"},
	{"allday", "yes", "new", "add", "absent", "absent", "same", "no"},
	{"timed", "yes", "new", "remove", "unexpressible", "changed", "moved", "no"},
	{"timed", "yes", "same", "none", "actionNone", "changed", "same", "yes"},
	{"allday", "yes", "same", "none", "expressible", "same", "moved", "yes"},
	{"timed", "yes", "new", "remove", "expressible", "same", "same", "yes"},
	{"timed", "yes", "changed", "none", "expressible", "same", "same", "no"},
}

var tokyo = recurrence.LoadLocation("Asia/Tokyo")

func weeklyFriday(t *testing.T) *json.RawMessage {
	t.Helper()
	raw := json.RawMessage(`{"freq":"weekly","interval":1,"byDay":["FR"]}`)
	return &raw
}

func offset(m int32) *int32 { return &m }

// diffEvents builds the master and an occurrence on date d (a Friday of June
// 2026) in the given form, one hour (timed) or one day (all-day) long.
func formEvent(allDay bool, d int, hour int) vevent {
	if allDay {
		start := time.Date(2026, 6, d, 0, 0, 0, 0, time.UTC)
		return vevent{allDay: true, start: start, end: start.AddDate(0, 0, 1)}
	}
	start := time.Date(2026, 6, d, hour, 0, 0, 0, tokyo)
	return vevent{start: start, end: start.Add(time.Hour), zone: "Asia/Tokyo"}
}

// lengthen moves an event's end on, the smallest move of its dates.
func lengthen(v vevent) vevent {
	if v.allDay {
		v.end = v.end.AddDate(0, 0, 1)
	} else {
		v.end = v.end.Add(time.Hour)
	}
	return v
}

func TestDiffPairwise(t *testing.T) {
	alarms := map[string][]ical.Alarm{
		"expressible":   {{Action: "DISPLAY", Minutes: 30, HasOffset: true}},
		"unexpressible": {{Action: "DISPLAY"}},
		"actionNone":    {{Action: "NONE"}},
		"absent":        nil,
	}
	classes := map[string]string{"absent": "", "same": "PUBLIC", "changed": "PRIVATE"}

	for i, row := range diffRows {
		form, recurring, override, exdate, valarm, class, gridMove, masked := row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7]
		allDay := form == "allday"
		f := seriesForm{allDay: allDay, zone: "Asia/Tokyo"}

		cur := formEvent(allDay, 5, 10)
		cur.title, cur.transp, cur.alarm = "t", "OPAQUE", alarmIntent{minutes: offset(15)}
		if masked == "no" {
			cur.location, cur.description = "room", "notes"
		}
		in := cur
		in.class, in.transp = classes[class], ""
		in.alarm = alarmOf(alarms[valarm])
		if gridMove == "moved" {
			in = lengthen(in)
		}
		if recurring == "yes" {
			cur.rule, in.rule = weeklyFriday(t), weeklyFriday(t)
		}

		occ := formEvent(allDay, 19, 10)
		curOverride := cur
		curOverride.start, curOverride.end, curOverride.rule = occ.start, occ.end, nil
		curOverride.title, curOverride.hasRecurrenceID, curOverride.recurrenceID = "o", true, occ.start
		curOverride.recurrenceIDDate = allDay
		inOverride := curOverride
		inOverride.alarm = alarmIntent{minutes: offset(15)}
		inOverride.class = ""
		twelfth := formEvent(allDay, 12, 10).start

		curEvents := []vevent{cur}
		body := incoming{uid: "u", master: &in}
		switch override {
		case "changed":
			curEvents = append(curEvents, curOverride)
			inOverride.title = "o2"
			body.overrides = []vevent{inOverride}
		case "same":
			curEvents = append(curEvents, curOverride)
			body.overrides = []vevent{inOverride}
		case "new":
			inOverride = lengthen(inOverride)
			body.overrides = []vevent{inOverride}
		}
		switch exdate {
		case "add":
			in.exdates = []time.Time{twelfth}
		case "remove":
			curEvents[0].exdates = []time.Time{twelfth}
		}

		curView := currentView(curEvents, f)
		g := grid{rule: in.parsedRule(), start: in.start, end: in.end, form: f}
		inView, err := incomingView(body, g)
		if err != nil {
			t.Fatalf("row %d: incomingView: %v", i, err)
		}
		curGrid := grid{rule: cur.parsedRule(), start: cur.start, end: cur.end, form: f}
		base := func(k string) (vevent, bool) {
			o, ok := curGrid.occurrence(k)
			if !ok {
				return vevent{}, false
			}
			return asGenerated(*curView.master, o), true
		}

		c := diffEvent(*curView.master, *inView.master, true)
		if c.title != nil || c.location != nil || c.memo != nil || c.url != nil || c.showAs != "" || c.ruleSet {
			t.Errorf("row %d: unsent or unchanged members written: %+v", i, c)
		}
		if want := map[string]string{"changed": "private"}[class]; c.visibility != want {
			t.Errorf("row %d: visibility = %q, want %q", i, c.visibility, want)
		}
		switch valarm {
		case "unexpressible":
			if c.alarmSet {
				t.Errorf("row %d: unexpressible alarm changed the stored one", i)
			}
		case "expressible":
			if !c.alarmSet || c.alarm == nil || *c.alarm != 30 {
				t.Errorf("row %d: alarm = %v %v, want 30", i, c.alarmSet, c.alarm)
			}
		default:
			if !c.alarmSet || c.alarm != nil {
				t.Errorf("row %d: alarm = %v %v, want cleared", i, c.alarmSet, c.alarm)
			}
		}
		if (c.dates != nil) != (gridMove == "moved") {
			t.Errorf("row %d: dates = %+v for grid %s", i, c.dates, gridMove)
		}

		undo, cancel, edits := occurrencePlan(curView, inView, base)
		k12 := f.key(twelfth)
		wantUndo, wantCancel := []string(nil), []string(nil)
		switch exdate {
		case "add":
			wantCancel = []string{k12}
		case "remove":
			wantUndo = []string{k12}
		}
		if strings.Join(undo, ",") != strings.Join(wantUndo, ",") || strings.Join(cancel, ",") != strings.Join(wantCancel, ",") {
			t.Errorf("row %d: undo %v cancel %v, want %v %v", i, undo, cancel, wantUndo, wantCancel)
		}
		switch override {
		case "changed":
			if len(edits) != 1 || edits[0].change.title == nil || *edits[0].change.title != "o2" || edits[0].change.dates != nil {
				t.Errorf("row %d: edits = %+v, want the title alone", i, edits)
			}
		case "new":
			if len(edits) != 1 || edits[0].change.title == nil || edits[0].change.dates == nil || edits[0].change.alarmSet {
				t.Errorf("row %d: edits = %+v, want title and dates against the generated occurrence", i, edits)
			}
		default:
			if len(edits) != 0 {
				t.Errorf("row %d: edits = %+v, want none", i, edits)
			}
		}
	}
}

func TestOccurrencePlanLeavesWhatItMayNotWrite(t *testing.T) {
	f := seriesForm{zone: "Asia/Tokyo"}
	master := formEvent(false, 5, 10)
	master.title, master.rule = "t", weeklyFriday(t)
	k19 := f.key(formEvent(false, 19, 10).start)
	k26 := f.key(formEvent(false, 26, 10).start)
	cur := view{master: &master, overrides: map[string]vevent{}, exdates: map[string]bool{k26: true}}

	moved := lengthen(formEvent(false, 19, 10))
	moved.title = "x"
	onCancelled := lengthen(formEvent(false, 26, 10))
	onCancelled.title = "y"
	in := view{
		master:    &master,
		overrides: map[string]vevent{k19: moved, k26: onCancelled},
		exdates:   map[string]bool{k19: true, k26: true},
	}
	undo, cancel, edits := occurrencePlan(cur, in, func(string) (vevent, bool) { return master, true })
	if len(undo) != 0 || strings.Join(cancel, ",") != k19 || len(edits) != 0 {
		t.Fatalf("cancellation must win over a change on its date: undo %v cancel %v edits %+v", undo, cancel, edits)
	}

	// A date with a changed occurrence the reader is not shown is left alone.
	in.exdates = map[string]bool{k26: true}
	_, _, edits = occurrencePlan(cur, in, func(string) (vevent, bool) { return vevent{}, false })
	if len(edits) != 0 {
		t.Fatalf("edits = %+v for an occurrence the reader may not write", edits)
	}

	// Without a master on both sides cancellations are not compared.
	noMaster := in
	noMaster.master, noMaster.exdates = nil, map[string]bool{}
	undo, cancel, _ = occurrencePlan(cur, noMaster, func(string) (vevent, bool) { return master, true })
	if len(undo) != 0 || len(cancel) != 0 {
		t.Fatalf("undo %v cancel %v from a body with no master", undo, cancel)
	}
}

func TestIncomingViewChecksOccurrences(t *testing.T) {
	f := seriesForm{zone: "Asia/Tokyo"}
	master := formEvent(false, 5, 10)
	master.rule = weeklyFriday(t)
	g := grid{rule: master.parsedRule(), start: master.start, end: master.end, form: f}

	saturday := formEvent(false, 6, 10)
	master.exdates = []time.Time{saturday.start}
	v, err := incomingView(incoming{master: &master}, g)
	if err != nil || len(v.exdates) != 0 {
		t.Fatalf("an EXDATE the rule does not generate must be ignored: %v %v", v.exdates, err)
	}

	for name, o := range map[string]vevent{
		"not an occurrence": {hasRecurrenceID: true, recurrenceID: saturday.start},
		"value type":        {hasRecurrenceID: true, recurrenceID: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC), recurrenceIDDate: true},
	} {
		if _, err := incomingView(incoming{master: &master, overrides: []vevent{o}}, g); !isRefusal(err, caldav.PreconditionValidCalendarData) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	twice := vevent{hasRecurrenceID: true, recurrenceID: formEvent(false, 12, 10).start}
	if _, err := incomingView(incoming{master: &master, overrides: []vevent{twice, twice}}, g); !isRefusal(err, caldav.PreconditionValidCalendarData) {
		t.Errorf("repeated RECURRENCE-ID: err = %v", err)
	}
	if _, err := incomingView(incoming{overrides: []vevent{twice}}, grid{form: f}); !isRefusal(err, caldav.PreconditionValidCalendarData) {
		t.Errorf("RECURRENCE-ID on a series with no rule: err = %v", err)
	}
}

func TestDiffEventReadsClassTranspAndDates(t *testing.T) {
	cur := formEvent(false, 5, 10)
	cur.class, cur.transp = "", "OPAQUE"
	cases := []struct {
		name               string
		edit               func(v *vevent)
		visibility, showAs string
		dates              bool
	}{
		{name: "absent says nothing", edit: func(v *vevent) { v.class, v.transp = "", "" }},
		{name: "PUBLIC matches a calendar-default event", edit: func(v *vevent) { v.class = "PUBLIC" }},
		{name: "unknown CLASS says nothing", edit: func(v *vevent) { v.class = "X-SECRET" }},
		{name: "CONFIDENTIAL", edit: func(v *vevent) { v.class = "CONFIDENTIAL" }, visibility: "confidential"},
		{name: "TRANSPARENT", edit: func(v *vevent) { v.transp = "TRANSPARENT" }, showAs: "free"},
		{name: "zone alone", edit: func(v *vevent) { v.zone = "UTC" }, dates: true},
		{name: "same instants", edit: func(v *vevent) { v.start = v.start.UTC() }},
	}
	for _, c := range cases {
		in := cur
		c.edit(&in)
		got := diffEvent(cur, in, true)
		if got.visibility != c.visibility || got.showAs != c.showAs || (got.dates != nil) != c.dates {
			t.Errorf("%s: %+v", c.name, got)
		}
	}

	busyAllDay := formEvent(true, 5, 0)
	other := busyAllDay
	other.zone = "America/New_York"
	if got := diffEvent(busyAllDay, other, true); got.dates != nil {
		t.Errorf("an all-day event's zone is not part of its dates: %+v", got)
	}
}

func TestAlarmOf(t *testing.T) {
	expressible := ical.Alarm{Action: "DISPLAY", Minutes: 10, HasOffset: true}
	tooLong := ical.Alarm{Action: "DISPLAY", Minutes: ical.MaxAlarmMinutes + 1, HasOffset: true}
	none := ical.Alarm{Action: "NONE"}
	cases := []struct {
		name   string
		alarms []ical.Alarm
		keep   bool
		want   *int32
	}{
		{name: "absent"},
		{name: "ACTION:NONE only", alarms: []ical.Alarm{none}},
		{name: "NONE then expressible", alarms: []ical.Alarm{none, expressible}, want: offset(10)},
		{name: "relative to the end", alarms: []ical.Alarm{{Action: "DISPLAY"}}, keep: true},
		{name: "too long", alarms: []ical.Alarm{tooLong, expressible}, keep: true},
	}
	for _, c := range cases {
		got := alarmOf(c.alarms)
		if got.keep != c.keep || !sameOffset(got.minutes, c.want) {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

func isRefusal(err error, p caldav.PreconditionType) bool {
	var r *refusal
	return errors.As(err, &r) && r.precondition == p
}

const icsHead = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n"

func icsEvent(lines ...string) string {
	return "BEGIN:VEVENT\r\nUID:u@client\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260605T010000Z\r\nDTEND:20260605T020000Z\r\n" +
		strings.Join(append(lines, ""), "\r\n") + "END:VEVENT\r\n"
}

func readText(t *testing.T, text string) (incoming, error) {
	t.Helper()
	cal, err := goical.NewDecoder(bytes.NewReader([]byte(text))).Decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return readIncoming(cal, []byte(text))
}

func TestReadIncomingRefuses(t *testing.T) {
	cases := map[string]struct {
		text string
		want caldav.PreconditionType
	}{
		"METHOD":            {icsHead + "METHOD:REQUEST\r\n" + icsEvent("SUMMARY:a") + "END:VCALENDAR\r\n", caldav.PreconditionValidCalendarData},
		"VTODO":             {icsHead + "BEGIN:VTODO\r\nUID:x\r\nDTSTAMP:20260101T000000Z\r\nEND:VTODO\r\n" + "END:VCALENDAR\r\n", caldav.PreconditionSupportedCalendarComponent},
		"RDATE":             {icsHead + icsEvent("SUMMARY:a", "RDATE:20260606T010000Z") + "END:VCALENDAR\r\n", caldav.PreconditionValidCalendarData},
		"STATUS:CANCELLED":  {icsHead + icsEvent("SUMMARY:a", "STATUS:CANCELLED") + "END:VCALENDAR\r\n", caldav.PreconditionValidCalendarData},
		"THISANDFUTURE":     {icsHead + icsEvent("SUMMARY:a", "RECURRENCE-ID;RANGE=THISANDFUTURE:20260605T010000Z") + "END:VCALENDAR\r\n", caldav.PreconditionValidCalendarData},
		"no SUMMARY":        {icsHead + icsEvent() + "END:VCALENDAR\r\n", caldav.PreconditionValidCalendarData},
		"two masters":       {icsHead + icsEvent("SUMMARY:a") + icsEvent("SUMMARY:b") + "END:VCALENDAR\r\n", caldav.PreconditionValidCalendarData},
		"DTEND before":      {icsHead + strings.Replace(icsEvent("SUMMARY:a"), "DTEND:20260605T020000Z", "DTEND:20260605T000000Z", 1) + "END:VCALENDAR\r\n", caldav.PreconditionValidCalendarData},
		"unstorable RRULE":  {icsHead + icsEvent("SUMMARY:a", "RRULE:FREQ=YEARLY;BYMONTH=1,2") + "END:VCALENDAR\r\n", caldav.PreconditionValidCalendarData},
		"unreadable TRIGGE": {icsHead + icsEvent("SUMMARY:a", "BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:soon", "END:VALARM") + "END:VCALENDAR\r\n", caldav.PreconditionValidCalendarData},
	}
	for name, c := range cases {
		if _, err := readText(t, c.text); !isRefusal(err, c.want) {
			t.Errorf("%s: err = %v, want %s", name, err, c.want)
		}
	}

	in, err := readText(t, icsHead+icsEvent("SUMMARY:a", "X-APPLE-THING:1", "SEQUENCE:3", "GEO:1;2")+
		icsEvent("SUMMARY:b", "RECURRENCE-ID:20260612T010000Z")+"END:VCALENDAR\r\n")
	if err != nil || in.uid != "u@client" || in.master == nil || len(in.overrides) != 1 {
		t.Fatalf("readIncoming = %+v, %v", in, err)
	}
}

func TestValidateHoldsBodiesToTheRESTSchema(t *testing.T) {
	ok := formEvent(false, 5, 10)
	ok.title = "fine"
	if err := validate(bodySchemas.create, createBody(ok).Body); err != nil {
		t.Fatalf("a valid create was refused: %v", err)
	}
	long := ok
	long.title = strings.Repeat("a", 501)
	if err := validate(bodySchemas.create, createBody(long).Body); !isRefusal(err, caldav.PreconditionValidCalendarData) {
		t.Fatalf("create with a 501-character title: %v", err)
	}
	url := strings.Repeat("u", 2001)
	if err := validate(bodySchemas.patch, patchBody(change{url: &url}).Body); !isRefusal(err, caldav.PreconditionValidCalendarData) {
		t.Fatalf("patch with a 2001-character URL: %v", err)
	}
	if err := validate(bodySchemas.patch, patchBody(change{alarmSet: true}).Body); err != nil {
		t.Fatalf("clearing the alarm was refused: %v", err)
	}
	if err := validate(bodySchemas.patch, patchBody(everything(ok)).Body); err != nil {
		t.Fatalf("a full patch was refused: %v", err)
	}
}

func TestMatchesIsStrong(t *testing.T) {
	for cond, want := range map[string]bool{
		"*": true, `"abc"`: true, ` "abc" `: true, `W/"abc"`: false, `"abc", "def"`: false, `"abd"`: false,
	} {
		if got := matches(webdavMatch(cond), "abc"); got != want {
			t.Errorf("matches(%q) = %v", cond, got)
		}
	}
}

func webdavMatch(s string) webdav.ConditionalMatch { return webdav.ConditionalMatch(s) }
