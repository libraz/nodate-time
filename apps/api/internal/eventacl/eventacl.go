// Package eventacl decides what a reader may see of a calendar event. It is
// pure: callers pass the reader, the event fields and the occurrence's
// attendees, and get a [Decision] back. Membership and role checks happen
// before this package is reached.
package eventacl

// Visibility is the calendar_events.visibility value.
type Visibility string

// Visibility values.
const (
	VisibilityDefault      Visibility = "default"
	VisibilityPublic       Visibility = "public"
	VisibilityPrivate      Visibility = "private"
	VisibilityConfidential Visibility = "confidential"
)

// Class is the reader's relationship to one event or occurrence.
type Class int

// Reader classes.
const (
	// ClassNone is a reader with no access path to the calendar.
	ClassNone Class = iota
	// ClassParty is the owner, the creator or an enabled attendee.
	ClassParty
	// ClassMember is any other calendar member, whatever the role.
	ClassMember
	// ClassAnonymous reaches the event through a public share or embed.
	ClassAnonymous
)

// Event holds the event fields the decision depends on.
type Event struct {
	Visibility      Visibility
	CalendarDefault Visibility
	OwnerUserID     uint32
	CreatedByUserID uint32
}

// Reader identifies who is reading.
type Reader struct {
	UserID uint32
	// IsMember is true when the reader is a member of the calendar.
	IsMember bool
	// Anonymous is true for a public share or embed reader.
	Anonymous bool
}

// Attendee is one calendar_event_attendees row.
type Attendee struct {
	UserID  uint32
	Enabled bool
}

// Effective resolves a visibility to public, private or confidential.
// default takes the calendar's setting, and public when that is also
// default or absent; a value outside the enum is confidential.
func Effective(v, calendarDefault Visibility) Visibility {
	switch v {
	case VisibilityPublic, VisibilityPrivate, VisibilityConfidential:
		return v
	case VisibilityDefault, "":
		switch calendarDefault {
		case VisibilityPrivate, VisibilityConfidential:
			return calendarDefault
		case VisibilityPublic, VisibilityDefault, "":
			return VisibilityPublic
		}
		return VisibilityConfidential
	}
	return VisibilityConfidential
}

// OccurrenceAttendees returns the user ids attending one occurrence. An
// override row with any attendee row, enabled or not, replaces the series'
// attendees; with none, the series' attendees apply. A series row passes
// nil for override.
func OccurrenceAttendees(series, override []Attendee) []uint32 {
	src := series
	if len(override) > 0 {
		src = override
	}
	var ids []uint32
	for _, a := range src {
		if a.Enabled {
			ids = append(ids, a.UserID)
		}
	}
	return ids
}

// Decision is the field exposure for one reader and one event.
type Decision struct {
	// Exists is false when the event must be omitted from lists and
	// answered as not found.
	Exists bool
	Class  Class

	Title       bool
	Times       bool // startAt/endAt and startDate/endDate
	AllDay      bool
	Location    bool
	Memo        bool
	URL         bool
	Kind        bool
	BlockLabel  bool
	ShowAs      bool
	Owner       bool
	Attendees   bool // attendee list and their answers
	Subresource bool // comments, checklist, attachments

	// DetailsHidden is true when the reader sees the event with fields
	// withheld. A write that carries a withheld field is refused.
	DetailsHidden bool
}

// Classify returns the reader's class for the event. attendees are the
// occurrence's attendees as returned by [OccurrenceAttendees].
func Classify(ev Event, r Reader, attendees []uint32) Class {
	if r.Anonymous {
		return ClassAnonymous
	}
	if r.UserID != 0 {
		if r.UserID == ev.OwnerUserID || r.UserID == ev.CreatedByUserID {
			return ClassParty
		}
		for _, id := range attendees {
			if id == r.UserID {
				return ClassParty
			}
		}
	}
	if r.IsMember {
		return ClassMember
	}
	return ClassNone
}

// Decide returns what the reader may see of the event.
func Decide(ev Event, r Reader, attendees []uint32) Decision {
	class := Classify(ev, r, attendees)
	vis := Effective(ev.Visibility, ev.CalendarDefault)
	d := Decision{Class: class}
	switch class {
	case ClassNone:
		return d
	case ClassParty:
		return full(d)
	}
	switch vis {
	case VisibilityPublic:
		if class == ClassMember {
			return full(d)
		}
		d.Exists, d.Title, d.Times, d.AllDay, d.Location = true, true, true, true, true
		d.DetailsHidden = true
	case VisibilityPrivate:
		d.Exists, d.Times, d.AllDay = true, true, true
		d.DetailsHidden = true
		if class == ClassMember {
			d.Title, d.Kind, d.BlockLabel, d.ShowAs = true, true, true, true
			d.Owner, d.Attendees = true, true
		}
	}
	return d
}

func full(d Decision) Decision {
	d.Exists = true
	d.Title, d.Times, d.AllDay, d.Location, d.Memo, d.URL = true, true, true, true, true, true
	d.Kind, d.BlockLabel, d.ShowAs, d.Owner, d.Attendees, d.Subresource = true, true, true, true, true, true
	return d
}
