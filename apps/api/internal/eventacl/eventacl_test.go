package eventacl

import (
	"reflect"
	"testing"
)

const (
	uOwner   = 1
	uCreator = 2
	uAtt     = 3
	uMember  = 4
	uOutside = 5
)

func TestEffective(t *testing.T) {
	cases := []struct{ v, def, want Visibility }{
		{VisibilityPublic, VisibilityPrivate, VisibilityPublic},
		{VisibilityPrivate, VisibilityPublic, VisibilityPrivate},
		{VisibilityConfidential, "", VisibilityConfidential},
		{VisibilityDefault, VisibilityPrivate, VisibilityPrivate},
		{VisibilityDefault, VisibilityDefault, VisibilityPublic},
		{VisibilityDefault, "", VisibilityPublic},
		{"", "", VisibilityPublic},
		{"secret", VisibilityPublic, VisibilityConfidential},
		{VisibilityDefault, "weird", VisibilityConfidential},
	}
	for _, c := range cases {
		if got := Effective(c.v, c.def); got != c.want {
			t.Errorf("Effective(%q,%q)=%q want %q", c.v, c.def, got, c.want)
		}
	}
}

func TestDecide(t *testing.T) {
	readers := map[string]Reader{
		"owner":     {UserID: uOwner, IsMember: true},
		"creator":   {UserID: uCreator, IsMember: true},
		"attendee":  {UserID: uAtt, IsMember: true},
		"member":    {UserID: uMember, IsMember: true},
		"nonmember": {UserID: uOutside},
		"share":     {Anonymous: true},
	}
	fields := func(d Decision) map[string]bool {
		return map[string]bool{
			"title": d.Title, "times": d.Times, "allDay": d.AllDay, "location": d.Location,
			"memo": d.Memo, "url": d.URL, "kind": d.Kind, "blockLabel": d.BlockLabel,
			"showAs": d.ShowAs, "owner": d.Owner, "attendees": d.Attendees, "sub": d.Subresource,
		}
	}
	all := []string{"title", "times", "allDay", "location", "memo", "url", "kind", "blockLabel", "showAs", "owner", "attendees", "sub"}
	set := func(names ...string) map[string]bool {
		m := map[string]bool{}
		for _, n := range all {
			m[n] = false
		}
		for _, n := range names {
			m[n] = true
		}
		return m
	}
	full := set(all...)
	memberPrivate := set("title", "times", "allDay", "kind", "blockLabel", "showAs", "owner", "attendees")
	sharePublic := set("title", "times", "allDay", "location")
	sharePrivate := set("times", "allDay")
	none := set()

	type want struct {
		exists, hidden bool
		f              map[string]bool
	}
	table := map[Visibility]map[string]want{
		VisibilityPublic: {
			"owner": {true, false, full}, "creator": {true, false, full}, "attendee": {true, false, full},
			"member": {true, false, full}, "nonmember": {false, false, none}, "share": {true, true, sharePublic},
		},
		VisibilityPrivate: {
			"owner": {true, false, full}, "creator": {true, false, full}, "attendee": {true, false, full},
			"member": {true, true, memberPrivate}, "nonmember": {false, false, none}, "share": {true, true, sharePrivate},
		},
		VisibilityConfidential: {
			"owner": {true, false, full}, "creator": {true, false, full}, "attendee": {true, false, full},
			"member": {false, false, none}, "nonmember": {false, false, none}, "share": {false, false, none},
		},
		"unknown": {
			"owner": {true, false, full}, "member": {false, false, none}, "share": {false, false, none},
		},
	}
	for vis, row := range table {
		for name, w := range row {
			ev := Event{Visibility: vis, OwnerUserID: uOwner, CreatedByUserID: uCreator}
			d := Decide(ev, readers[name], []uint32{uAtt})
			if d.Exists != w.exists || d.DetailsHidden != w.hidden {
				t.Errorf("%s/%s: exists=%v hidden=%v want %v %v", vis, name, d.Exists, d.DetailsHidden, w.exists, w.hidden)
			}
			if !reflect.DeepEqual(fields(d), w.f) {
				t.Errorf("%s/%s: fields %v want %v", vis, name, fields(d), w.f)
			}
		}
	}
}

func TestDecideCalendarDefault(t *testing.T) {
	ev := Event{Visibility: VisibilityDefault, CalendarDefault: VisibilityPrivate, OwnerUserID: uOwner}
	if d := Decide(ev, Reader{UserID: uMember, IsMember: true}, nil); !d.DetailsHidden || d.Memo {
		t.Errorf("default over private calendar should hide details: %+v", d)
	}
}

func TestOccurrenceAttendees(t *testing.T) {
	series := []Attendee{{UserID: 10, Enabled: true}, {UserID: 11, Enabled: true}, {UserID: 12, Enabled: false}}
	cases := []struct {
		name     string
		override []Attendee
		want     []uint32
	}{
		{"no override rows inherits", nil, []uint32{10, 11}},
		{"override rows replace", []Attendee{{UserID: 20, Enabled: true}}, []uint32{20}},
		{"all disabled removes everyone", []Attendee{{UserID: 10, Enabled: false}, {UserID: 11, Enabled: false}}, nil},
		{"mixed", []Attendee{{UserID: 10, Enabled: true}, {UserID: 11, Enabled: false}}, []uint32{10}},
	}
	for _, c := range cases {
		if got := OccurrenceAttendees(series, c.override); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestOverrideAttendeeVisibility(t *testing.T) {
	ev := Event{Visibility: VisibilityConfidential, OwnerUserID: uOwner}
	series := []Attendee{{UserID: uAtt, Enabled: true}}
	r := Reader{UserID: uAtt, IsMember: true}
	if !Decide(ev, r, OccurrenceAttendees(series, nil)).Exists {
		t.Error("series attendee should see inherited occurrence")
	}
	off := []Attendee{{UserID: uAtt, Enabled: false}}
	if Decide(ev, r, OccurrenceAttendees(series, off)).Exists {
		t.Error("attendee removed from the override must not see it")
	}
}
