package dav

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	goical "github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
	"github.com/libraz/nodate-time/apps/api/internal/ical"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// maxUIDLength is what dav_event_resources.uid holds.
const maxUIDLength = 255

// defaultUIDSuffix ends every UID the server derives; no client UID may.
const defaultUIDSuffix = "@nodate-time"

// refusal is a body the server will not store, with the precondition it
// answers with.
type refusal struct {
	precondition caldav.PreconditionType
	reason       string
}

func (r *refusal) Error() string { return string(r.precondition) + ": " + r.reason }

func invalidData(reason string) error {
	return &refusal{precondition: caldav.PreconditionValidCalendarData, reason: reason}
}

// alarmIntent is what a VEVENT's alarms ask for: a reminder offset, none, or
// to leave the stored one alone because none of them can be stored.
type alarmIntent struct {
	keep    bool
	minutes *int32
}

// vevent is one VEVENT reduced to what the calendar stores.
type vevent struct {
	title, location, description, url string
	// class, transp and status are as written, "" when absent.
	class, transp, status string
	start, end            time.Time
	allDay                bool
	// zone is the IANA zone of a timed event, "UTC" when the file named none.
	// An all-day event's is what the file named, possibly "".
	zone string
	// rule is the stored form of the RRULE, nil when there is none.
	rule    *json.RawMessage
	alarm   alarmIntent
	exdates []time.Time

	recurrenceID    time.Time
	hasRecurrenceID bool
	// recurrenceIDDate records a RECURRENCE-ID written as a DATE.
	recurrenceIDDate bool
}

// incoming is a PUT body: one UID, at most one master and the changed
// occurrences.
type incoming struct {
	uid       string
	master    *vevent
	overrides []vevent
}

// readIncoming checks the shape of a PUT body and reads its VEVENTs. text is
// the body as sent; cal is go-webdav's decoding of it, consulted for what the
// value parser does not keep.
func readIncoming(cal *goical.Calendar, text []byte) (incoming, error) {
	if cal.Props.Get("METHOD") != nil {
		return incoming{}, invalidData("METHOD")
	}
	var dateRIDs []bool
	for _, child := range cal.Children {
		switch child.Name {
		case goical.CompToDo, goical.CompJournal, goical.CompFreeBusy:
			return incoming{}, &refusal{precondition: caldav.PreconditionSupportedCalendarComponent, reason: child.Name}
		case goical.CompEvent:
		default:
			continue
		}
		if child.Props.Get("RDATE") != nil {
			return incoming{}, invalidData("RDATE")
		}
		if st := child.Props.Get("STATUS"); st != nil && strings.EqualFold(strings.TrimSpace(st.Value), "CANCELLED") {
			return incoming{}, invalidData("STATUS:CANCELLED")
		}
		isDate := false
		if rid := child.Props.Get("RECURRENCE-ID"); rid != nil {
			if strings.EqualFold(rid.Params.Get("RANGE"), "THISANDFUTURE") {
				return incoming{}, invalidData("RANGE")
			}
			isDate = strings.EqualFold(rid.Params.Get(goical.ParamValue), string(goical.ValueDate))
		}
		dateRIDs = append(dateRIDs, isDate)
	}
	if len(dateRIDs) == 0 {
		return incoming{}, invalidData("no VEVENT")
	}

	parsed, err := ical.Parse(text, ical.ParseOptions{Strict: true})
	if err != nil {
		return incoming{}, invalidData(err.Error())
	}
	if len(parsed) != len(dateRIDs) {
		return incoming{}, invalidData("VEVENT count")
	}
	var in incoming
	for i, p := range parsed {
		if p.UID == "" || utf8.RuneCountInString(p.UID) > maxUIDLength {
			return incoming{}, invalidData("UID")
		}
		if p.Summary == "" {
			return incoming{}, invalidData("SUMMARY")
		}
		if in.uid == "" {
			in.uid = p.UID
		} else if p.UID != in.uid {
			return incoming{}, invalidData("several UIDs")
		}
		v, err := fromParsed(p)
		if err != nil {
			return incoming{}, err
		}
		v.recurrenceIDDate = dateRIDs[i]
		if !v.hasRecurrenceID {
			if in.master != nil {
				return incoming{}, invalidData("several masters")
			}
			in.master = &v
			continue
		}
		in.overrides = append(in.overrides, v)
	}
	return in, nil
}

// fromParsed reduces one parsed VEVENT, refusing values the calendar cannot
// hold. The end is DTEND, else DTSTART plus DURATION, else the length an
// import gives an event without either.
func fromParsed(p ical.ParsedEvent) (vevent, error) {
	if p.Start.IsZero() {
		return vevent{}, invalidData("DTSTART")
	}
	if p.TooManyExdates {
		return vevent{}, invalidData("EXDATE count")
	}
	v := vevent{
		title: p.Summary, location: p.Location, description: p.Description, url: p.URL,
		class: p.Class, transp: p.Transp, status: p.Status,
		start: p.Start, allDay: p.AllDay,
		exdates:      p.Exdates,
		recurrenceID: p.RecurrenceID, hasRecurrenceID: p.HasRecurrenceID,
		alarm: alarmOf(p.Alarms),
	}
	v.zone = p.Zone()
	if !v.allDay && v.zone == "" {
		v.zone = "UTC"
	}
	switch {
	case !p.End.IsZero():
		v.end = p.End
	case p.HasDuration && v.allDay:
		if p.Duration%(24*time.Hour) != 0 {
			return vevent{}, invalidData("DURATION")
		}
		v.end = p.Start.AddDate(0, 0, int(p.Duration/(24*time.Hour)))
	case p.HasDuration:
		v.end = p.Start.Add(p.Duration)
	case v.allDay:
		v.end = p.Start.AddDate(0, 0, 1)
	default:
		v.end = p.Start.Add(time.Hour)
	}
	if v.allDay && v.end.Equal(v.start) {
		v.end = v.start.AddDate(0, 0, 1)
	}
	if v.end.Before(v.start) || (v.allDay && !v.end.After(v.start)) {
		return vevent{}, invalidData("DTEND before DTSTART")
	}
	if p.RRule != "" && !p.HasRecurrenceID {
		loc := recurrence.LoadLocation(recurrence.ExpansionZone(v.allDay, v.zone))
		rule, ok := ical.ConvertRRule(p.RRule, v.start.In(loc), v.allDay)
		if !ok {
			return vevent{}, invalidData("RRULE")
		}
		parsed, err := recurrence.ParseRule(*rule)
		if err != nil || parsed == nil || parsed.Validate(v.start.In(loc)) != nil {
			return vevent{}, invalidData("RRULE")
		}
		v.rule = rule
	}
	return v, nil
}

// alarmOf reads the alarms the way an import does, except that alarms whose
// only expressible meaning is "none" are told apart from alarms that say
// something the calendar cannot store.
func alarmOf(alarms []ical.Alarm) alarmIntent {
	var real []ical.Alarm
	for _, a := range alarms {
		if a.Action != "NONE" {
			real = append(real, a)
		}
	}
	if m, ok := ical.FirstExpressibleAlarm(real); ok {
		return alarmIntent{minutes: &m}
	}
	return alarmIntent{keep: len(real) > 0}
}

// parsedRule reads the stored form back; v.rule always parses.
func (v vevent) parsedRule() *recurrence.Rule {
	if v.rule == nil {
		return nil
	}
	r, _ := recurrence.ParseRule(*v.rule)
	return r
}

// rendered reads what the server draws for a series back through the strict
// parser, so both sides of a diff are read the same way.
func rendered(s ical.Series) ([]vevent, error) {
	text := ical.Render([]ical.Series{s}, ical.RenderOptions{Now: time.Now()})
	parsed, err := ical.Parse(text, ical.ParseOptions{Strict: true})
	if err != nil {
		return nil, err
	}
	out := make([]vevent, 0, len(parsed))
	for _, p := range parsed {
		v, err := fromParsed(p)
		if err != nil {
			var r *refusal
			if errors.As(err, &r) {
				// What the server stores always renders to something it
				// reads back; anything else is a defect, not a bad request.
				return nil, errors.New("dav: rendering does not read back: " + r.reason)
			}
			return nil, err
		}
		v.recurrenceIDDate = s.AllDay
		out = append(out, v)
	}
	return out, nil
}
