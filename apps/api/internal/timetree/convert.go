package timetree

import (
	"database/sql"
	"slices"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/calimport"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// Why an event is not imported. A skip is a decision about the event; a
// rejection is data the importer refuses to guess about, and counts as a
// failure.
const (
	skipDeleted       = "deleted or deactivated"
	skipBirthday      = "birthday"
	skipMemo          = "memo"
	skipNoStart       = "no start time"
	skipUntitled      = "no title"
	skipUnsupported   = "unsupported repeat rule"
	skipOtherRepeat   = "RDATE, EXRULE or other recurrence row"
	rejectEndBefore   = "end before start"
	rejectBadExdate   = "unreadable EXDATE"
	rejectManyExdates = "too many EXDATE values"
	sourceTimeTree    = "timetree-import"
	noLabel           = "(no label)"
	timeTreeBirthday  = 1
	timeTreeMemo      = 2
)

const (
	dateForm    = "20060102"
	dateTimeUTC = "20060102T150405Z"
	dateTimeNZ  = "20060102T150405"
)

// converted is one TimeTree event after conversion.
type converted struct {
	Event calimport.Event
	// Owner is the normalized address the event is filed under.
	Owner string
	// Attendees are the normalized addresses of its participants.
	Attendees []string
	// Author is the normalized address recorded as the event's creator.
	Author string
	Label  string
	Skip   string
	// Reject is set when the event cannot be imported as given.
	Reject string
}

func (c converted) ok() bool { return c.Skip == "" && c.Reject == "" }

// converter holds what one calendar's events are converted against.
type converter struct {
	wsTZ  string
	wsLoc *time.Location
	// labels maps a TimeTree label id to its name.
	labels map[flexID]string
	// owners maps a label name to the normalized address that owns it.
	owners map[string]string
	// defaultOwner is the normalized address for events without a mapped label.
	defaultOwner string
	// defaultAttendees are the normalized participants of those events.
	defaultAttendees []string
	// authors maps a TimeTree user id to a normalized address.
	authors map[flexID]string
	// importer is the normalized address of the importer, and parties every
	// address that may be recorded as a creator on this calendar.
	importer string
	parties  map[string]bool
}

// Why an event's TimeTree author is not recorded as its creator.
const (
	authorNotMapped  = "not in authors"
	authorNotAMember = "not a member of the calendar"
	authorMissing    = "no author"
)

func newConverter(wsTZ string, labels []ttLabel, m *MappingCalendar, importer string, authors map[string]string) *converter {
	loc, _ := readZone(wsTZ)
	cv := &converter{
		wsTZ:         wsTZ,
		wsLoc:        loc,
		labels:       map[flexID]string{},
		owners:       map[string]string{},
		defaultOwner: norm(importer),
		authors:      map[flexID]string{},
		importer:     norm(importer),
		parties:      map[string]bool{norm(importer): true},
	}
	for _, l := range labels {
		cv.labels[l.ID] = l.Name
	}
	for id, email := range authors {
		cv.authors[flexID(id)] = norm(email)
	}
	if m != nil {
		for _, mem := range m.Members {
			cv.parties[norm(mem.Email)] = true
		}
		for name, email := range m.Labels {
			cv.owners[name] = norm(email)
		}
		if m.DefaultOwner != "" {
			cv.defaultOwner = norm(m.DefaultOwner)
		}
		for _, email := range m.DefaultAttendees {
			cv.defaultAttendees = append(cv.defaultAttendees, norm(email))
		}
	}
	return cv
}

// author is the address recorded as the creator of an event TimeTree says
// id created, and why it is the importer instead when it is.
func (cv *converter) author(id flexID) (email, unmapped string) {
	email, ok := cv.authors[id]
	switch {
	case id == "" || id == "0":
		return cv.importer, authorMissing
	case !ok:
		return cv.importer, authorNotMapped
	case !cv.parties[email]:
		return cv.importer, authorNotAMember
	}
	return email, ""
}

// readZone loads an IANA zone name. Empty and "Local" are not names.
func readZone(name string) (*time.Location, bool) {
	if name == "" || name == "Local" {
		return time.UTC, false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC, false
	}
	return loc, true
}

func isUTCName(name string) bool { return name == "UTC" || name == "Etc/UTC" }

// labelName is the TimeTree label name of an event, or noLabel.
func (cv *converter) labelName(e ttEvent) string {
	if name := cv.labels[e.LabelID]; e.LabelID != "" && name != "" {
		return name
	}
	return noLabel
}

// convert turns one TimeTree event into an importable event, or says why not.
func (cv *converter) convert(e ttEvent) converted {
	out := converted{Label: cv.labelName(e)}
	skip := func(reason string) converted { out.Skip = reason; return out }

	switch {
	case isSet(e.DeletedAt) || isSet(e.DeactivatedAt):
		return skip(skipDeleted)
	case e.Type == timeTreeBirthday:
		return skip(skipBirthday)
	case e.Category == timeTreeMemo:
		return skip(skipMemo)
	case e.StartAt <= 0:
		return skip(skipNoStart)
	case e.Title == "":
		return skip(skipUntitled)
	}
	rrule, exRows, other := splitRecurrences(e.Recurrences)
	if other {
		return skip(skipOtherRepeat)
	}

	if owner, ok := cv.owners[out.Label]; ok {
		out.Owner = owner
	} else {
		out.Owner, out.Attendees = cv.defaultOwner, cv.defaultAttendees
	}
	out.Author, _ = cv.author(e.AuthorID)

	ev := calimport.Event{
		SourceUID:  e.ID,
		Title:      e.Title,
		Location:   e.Location,
		Memo:       truncateRunes(e.Note, calimport.MaxMemoRunes),
		URL:        e.URL,
		AllDay:     e.AllDay,
		Visibility: generated.CalendarEventsVisibilityDefault,
		ShowAs:     generated.CalendarEventsShowAsBusy,
		// notification offset stays NULL: alerts belong to the logged-in account
		NotificationOffset: sql.NullInt32{},
	}
	startLoc, startTZReadable := readZone(e.StartTimezone)
	hasEnd := e.EndAt > 0
	if hasEnd && e.EndAt < e.StartAt {
		out.Reject = rejectEndBefore
	}

	if e.AllDay {
		// The dates TimeTree shows, as UTC midnights with the end exclusive.
		ev.Timezone = cv.wsTZ
		ev.Start = utcMidnight(time.UnixMilli(e.StartAt).In(startLoc), 0)
		if hasEnd {
			ev.End = utcMidnight(time.UnixMilli(e.EndAt).In(startLoc), 1)
		}
	} else {
		ev.Timezone = cv.wsTZ
		if startTZReadable && !isUTCName(e.StartTimezone) {
			ev.Timezone = e.StartTimezone
		}
		ev.Start = time.UnixMilli(e.StartAt).UTC()
		if hasEnd {
			ev.End = time.UnixMilli(e.EndAt).UTC()
		}
	}

	if rrule != "" {
		if e.AllDay {
			rrule = dateUntil(rrule, startLoc)
		}
		zone := recurrence.ExpansionZone(ev.AllDay, ev.Timezone)
		if _, ok := recurrence.ParseRRule(rrule, ev.Start.In(recurrence.LoadLocation(zone))); !ok {
			return skip(skipUnsupported)
		}
		ev.RRule = rrule
	}

	exdates, readable := cv.exdates(exRows, ev, startLoc)
	if !readable && out.Reject == "" {
		out.Reject = rejectBadExdate
	}
	if len(exdates) > calimport.MaxExdates {
		ev.TooManyExdates = true
		exdates = nil
		if out.Reject == "" {
			out.Reject = rejectManyExdates
		}
	}
	ev.Exdates = exdates

	out.Event = ev
	return out
}

// utcMidnight is 00:00 UTC of d's calendar day plus days.
func utcMidnight(d time.Time, days int) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day()+days, 0, 0, 0, 0, time.UTC)
}

// exdate is one EXDATE value with the zone its row named, if any.
type exdate struct{ tzid, value string }

// splitRecurrences separates the RRULE and EXDATE values from the rest.
// other is true when any row is neither, or a second RRULE: the series would
// silently change if they were dropped.
func splitRecurrences(rows []string) (rrule string, ex []exdate, other bool) {
	for _, row := range rows {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		head, rest, ok := strings.Cut(row, ":")
		if !ok {
			other = true
			continue
		}
		name, params, _ := strings.Cut(head, ";")
		switch strings.ToUpper(name) {
		case "RRULE":
			if rrule != "" {
				other = true
				continue
			}
			rrule = strings.TrimSpace(rest)
		case "EXDATE":
			tzid := paramTZID(params)
			if tzid == "" && len(rest) >= 5 && strings.EqualFold(rest[:5], "TZID=") {
				// The zone may also lead the value: EXDATE:TZID=Zone:20260101T090000
				zone, value, found := strings.Cut(rest[5:], ":")
				tzid, rest = zone, value
				if !found {
					rest = ""
				}
			}
			for _, v := range strings.Split(rest, ",") {
				ex = append(ex, exdate{tzid: tzid, value: strings.TrimSpace(v)})
			}
		default:
			other = true
		}
	}
	return rrule, ex, other
}

func paramTZID(params string) string {
	for _, p := range strings.Split(params, ";") {
		if len(p) >= 5 && strings.EqualFold(p[:5], "TZID=") {
			return p[5:]
		}
	}
	return ""
}

// dateUntil rewrites a date-time UNTIL as the date it falls on in loc, so the
// last day of an all-day series is kept whole.
func dateUntil(rrule string, loc *time.Location) string {
	parts := strings.Split(rrule, ";")
	for i, p := range parts {
		key, val, ok := strings.Cut(p, "=")
		if !ok || !strings.EqualFold(key, "UNTIL") {
			continue
		}
		if t, err := time.Parse(dateTimeUTC, val); err == nil {
			parts[i] = key + "=" + t.In(loc).Format(dateForm)
		} else if t, err := time.Parse(dateTimeNZ, val); err == nil {
			parts[i] = key + "=" + t.Format(dateForm)
		}
	}
	return strings.Join(parts, ";")
}

// exdates converts the rows to distinct UTC instants in ascending order.
// readable is false when any value is in none of the three accepted forms.
func (cv *converter) exdates(rows []exdate, ev calimport.Event, startLoc *time.Location) (out []time.Time, readable bool) {
	readable = true
	evLoc := recurrence.LoadLocation(ev.Timezone)
	for _, r := range rows {
		var t time.Time
		var day bool
		if d, err := time.Parse(dateTimeUTC, r.value); err == nil {
			t = d
		} else if r.tzid != "" {
			zone, ok := readZone(r.tzid)
			d, err := time.ParseInLocation(dateTimeNZ, r.value, zone)
			if !ok || err != nil {
				readable = false
				continue
			}
			t = d
		} else if d, err := time.Parse(dateForm, r.value); err == nil {
			t, day = d, true
		} else {
			readable = false
			continue
		}

		switch {
		case ev.AllDay && day:
			t = utcMidnight(t, 0)
		case ev.AllDay:
			t = utcMidnight(t.In(startLoc), 0)
		case day:
			// A date alone on a timed series names the occurrence at the start's clock time.
			s := ev.Start.In(evLoc)
			t = time.Date(t.Year(), t.Month(), t.Day(), s.Hour(), s.Minute(), s.Second(), 0, evLoc).UTC()
		default:
			t = t.UTC()
		}
		if !slices.ContainsFunc(out, t.Equal) {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out, readable
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// doubleDisplayCandidates counts changed occurrences that sit on a day where
// their parent series, after its EXDATEs, still has an occurrence of its own.
func doubleDisplayCandidates(events []ttEvent, conv []converted) int {
	byID := map[string]converted{}
	for i, e := range events {
		if conv[i].ok() && conv[i].Event.RRule != "" {
			byID[e.ID] = conv[i]
		}
	}
	n := 0
	for i, e := range events {
		if e.RecurringUUID == "" || !conv[i].ok() {
			continue
		}
		parent, ok := byID[e.RecurringUUID]
		if !ok || !seriesOccursOnDayOf(parent.Event, conv[i].Event) {
			continue
		}
		n++
	}
	return n
}

// seriesOccursOnDayOf reports whether series has a live occurrence on the day
// (in the changed event's expansion zone) that changed starts.
func seriesOccursOnDayOf(series, changed calimport.Event) bool {
	zone := recurrence.ExpansionZone(series.AllDay, series.Timezone)
	start := series.Start.In(recurrence.LoadLocation(zone))
	rule, ok := recurrence.ParseRRule(series.RRule, start)
	if !ok {
		return false
	}
	end := series.End
	if end.IsZero() {
		end = start.Add(time.Hour)
		if series.AllDay {
			end = start.AddDate(0, 0, 1)
		}
	}
	loc := recurrence.LoadLocation(recurrence.ExpansionZone(changed.AllDay, changed.Timezone))
	s := changed.Start.In(loc)
	from := time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 1)
	var skipped recurrence.ExceptionList
	for _, t := range series.Exdates {
		skipped = skipped.With(t, series.AllDay, series.Timezone)
	}
	for _, o := range recurrence.ExpandInZone(rule, start, end, from, to, zone) {
		if !o.StartAt.Before(from) && o.StartAt.Before(to) && !skipped.Cancels(o.StartAt, series.AllDay, series.Timezone) {
			return true
		}
	}
	return false
}
