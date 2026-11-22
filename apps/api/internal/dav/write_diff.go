package dav

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// change is one REST edit: the members a patch sends. A nil or unset member
// is left out of the patch and keeps its stored value.
type change struct {
	title, location, memo, url *string
	visibility, showAs         string
	// alarmSet sends notificationOffset: alarm, or null when alarm is nil.
	alarmSet bool
	alarm    *int32
	dates    *dates
	// ruleSet sends recurrenceRule: rule, or null when rule is nil.
	ruleSet bool
	rule    *json.RawMessage
}

// dates are both ends of an event in the form it takes.
type dates struct {
	allDay     bool
	start, end time.Time
	zone       string
}

func (c change) empty() bool {
	return c.title == nil && c.location == nil && c.memo == nil && c.url == nil &&
		c.visibility == "" && c.showAs == "" && !c.alarmSet && c.dates == nil && !c.ruleSet
}

// movesGrid reports whether the edit changes when the series' occurrences
// fall, which moves its changed and cancelled occurrences with it.
func (c change) movesGrid() bool { return c.dates != nil || c.ruleSet }

// diffEvent is the edit that turns what the server draws, cur, into what the
// client sent, in. Only members that differ are sent, so what the reader was
// not shown is never written back. withRule compares the recurrence rule,
// which only a master carries.
func diffEvent(cur, in vevent, withRule bool) change {
	var c change
	text := func(cur, in string) *string {
		if cur == in {
			return nil
		}
		return &in
	}
	c.title = text(cur.title, in.title)
	c.location = text(cur.location, in.location)
	c.memo = text(cur.description, in.description)
	c.url = text(cur.url, in.url)
	// CLASS and TRANSP left out of a body say nothing; a client that drops
	// them has not asked for the default.
	if v, ok := visibilityOf(in.class); ok {
		if cv, _ := visibilityOf(cur.class); cv != v {
			c.visibility = v
		}
	}
	if s, ok := showAsOf(in.transp); ok {
		if cs, _ := showAsOf(cur.transp); cs != s {
			c.showAs = s
		}
	}
	if !in.alarm.keep && !sameOffset(cur.alarm.minutes, in.alarm.minutes) {
		c.alarmSet, c.alarm = true, in.alarm.minutes
	}
	if !sameDates(cur, in) {
		c.dates = &dates{allDay: in.allDay, start: in.start, end: in.end, zone: in.zone}
	}
	if withRule && !sameRule(cur.rule, in.rule) {
		c.ruleSet, c.rule = true, in.rule
	}
	return c
}

// visibilityOf reads CLASS onto the visibility axis. The server writes no
// CLASS for an event that follows the calendar, and that reads as PUBLIC: a
// client echoing PUBLIC back has not changed it. ok is false for a value that
// says nothing the axis holds.
func visibilityOf(class string) (string, bool) {
	switch class {
	case "", "PUBLIC":
		return "public", class != ""
	case "PRIVATE":
		return "private", true
	case "CONFIDENTIAL":
		return "confidential", true
	}
	return "", false
}

// showAsOf reads TRANSP. It tells only free from taken, so a stored value the
// server draws as OPAQUE (tentative, out of office) is not reset by a client
// sending OPAQUE back.
func showAsOf(transp string) (string, bool) {
	switch transp {
	case "TRANSPARENT":
		return "free", true
	case "OPAQUE":
		return "busy", true
	}
	return "", false
}

func sameOffset(a, b *int32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameDates(a, b vevent) bool {
	if a.allDay != b.allDay {
		return false
	}
	if a.allDay {
		return day(a.start) == day(b.start) && day(a.end) == day(b.end)
	}
	return a.start.Equal(b.start) && a.end.Equal(b.end) && a.zone == b.zone
}

func sameRule(a, b *json.RawMessage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return bytes.Equal(*a, *b)
}

// day is an all-day bound as the date it stands for.
func day(t time.Time) string { return t.UTC().Format(dateOnly) }

const (
	dateOnly = "2006-01-02"
	// keyLayout is how an occurrence is named in a composite event id.
	keyLayout = "20060102"
)

// seriesForm is what names a series' occurrences: its value type and zone.
type seriesForm struct {
	allDay bool
	zone   string
}

// key names the occurrence starting at t by its date in the zone the series
// expands in, the way a composite event id does.
func (f seriesForm) key(t time.Time) string {
	return t.In(recurrence.LoadLocation(recurrence.ExpansionZone(f.allDay, f.zone))).Format(keyLayout)
}

// grid is what generates a series' occurrences.
type grid struct {
	rule       *recurrence.Rule
	start, end time.Time
	form       seriesForm
}

// occurrence returns the occurrence the rule generates on the date key names.
func (g grid) occurrence(key string) (recurrence.Occurrence, bool) {
	if g.rule == nil {
		return recurrence.Occurrence{}, false
	}
	zone := recurrence.ExpansionZone(g.form.allDay, g.form.zone)
	dayStart, err := time.ParseInLocation(keyLayout, key, recurrence.LoadLocation(zone))
	if err != nil {
		return recurrence.Occurrence{}, false
	}
	for _, occ := range recurrence.ExpandInZone(g.rule, g.start, g.end, dayStart, dayStart.AddDate(0, 0, 1), zone) {
		if g.form.key(occ.StartAt) == key {
			return occ, true
		}
	}
	return recurrence.Occurrence{}, false
}

// view is a series by occurrence: the master, the changed occurrences and the
// cancelled ones, each named by its date key.
type view struct {
	master    *vevent
	overrides map[string]vevent
	exdates   map[string]bool
}

// currentView indexes what the server draws for a series of form f.
func currentView(events []vevent, f seriesForm) view {
	v := view{overrides: map[string]vevent{}, exdates: map[string]bool{}}
	for i := range events {
		e := events[i]
		if e.hasRecurrenceID {
			v.overrides[f.key(e.recurrenceID)] = e
			continue
		}
		v.master = &e
		for _, x := range e.exdates {
			v.exdates[f.key(x)] = true
		}
	}
	return v
}

// incomingView indexes a PUT body against g, the series as the body would
// leave it. A changed occurrence must name an occurrence g generates, in the
// value type its DTSTART takes; a cancellation of one g does not generate
// means nothing and is dropped.
func incomingView(in incoming, g grid) (view, error) {
	v := view{master: in.master, overrides: map[string]vevent{}, exdates: map[string]bool{}}
	if in.master != nil {
		for _, x := range in.master.exdates {
			k := g.form.key(x)
			if _, ok := g.occurrence(k); ok {
				v.exdates[k] = true
			}
		}
	}
	for _, o := range in.overrides {
		if o.recurrenceIDDate != g.form.allDay {
			return view{}, invalidData("RECURRENCE-ID value type")
		}
		k := g.form.key(o.recurrenceID)
		if _, ok := g.occurrence(k); !ok {
			return view{}, invalidData("RECURRENCE-ID names no occurrence")
		}
		if _, dup := v.overrides[k]; dup {
			return view{}, invalidData("RECURRENCE-ID repeated")
		}
		v.overrides[k] = o
	}
	return v, nil
}

// overrideEdit is an edit confined to one occurrence.
type overrideEdit struct {
	key    string
	change change
}

// occurrencePlan is what turns cur's occurrences into in's, in the order it
// is applied: cancellations undone, occurrences cancelled, occurrences
// changed. Cancellations are compared only when both sides carry the master
// that lists them. base gives the occurrence on a date as the series now
// generates it, and false where the reader may not write it: a date with a
// changed occurrence the reader is not shown, or a series whose master they
// are not shown. A cancellation wins over a changed occurrence on its date;
// a changed occurrence the body leaves out is left as stored.
func occurrencePlan(cur, in view, base func(key string) (vevent, bool)) (undo, cancel []string, edits []overrideEdit) {
	undone := map[string]bool{}
	if cur.master != nil && in.master != nil {
		for _, k := range sortedKeys(cur.exdates) {
			if !in.exdates[k] {
				undo = append(undo, k)
				undone[k] = true
			}
		}
		for _, k := range sortedKeys(in.exdates) {
			if !cur.exdates[k] {
				cancel = append(cancel, k)
			}
		}
	}
	keys := make([]string, 0, len(in.overrides))
	for k := range in.overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if in.exdates[k] || (cur.exdates[k] && !undone[k]) {
			continue
		}
		o := in.overrides[k]
		from, ok := cur.overrides[k]
		if !ok {
			if from, ok = base(k); !ok {
				continue
			}
		}
		if c := diffEvent(from, o, false); !c.empty() {
			edits = append(edits, overrideEdit{key: k, change: c})
		}
	}
	return undo, cancel, edits
}

// asGenerated is the occurrence occ of a series drawn as master, the base a new
// changed occurrence is compared against.
func asGenerated(master vevent, occ recurrence.Occurrence) vevent {
	v := master
	v.start, v.end = occ.StartAt, occ.EndAt
	v.rule, v.exdates = nil, nil
	return v
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// occurrenceDate is a date key in the form UndoException takes.
func occurrenceDate(key string) string {
	if len(key) != len(keyLayout) {
		return key
	}
	return strings.Join([]string{key[:4], key[4:6], key[6:]}, "-")
}
