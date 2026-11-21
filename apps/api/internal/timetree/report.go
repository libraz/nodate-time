package timetree

import (
	"fmt"
	"io"
	"time"
)

// calData is one TimeTree calendar as fetched.
type calData struct {
	cal    ttCalendar
	labels []ttLabel
	events []ttEvent
	// repeated is how many copies of an already-seen ID the feed sent.
	repeated int
}

// analysis is what the dry-run report says about one TimeTree calendar.
type analysis struct {
	data    calData
	conv    []converted
	oldest  time.Time
	byLabel map[string]int
	// byAuthor counts the events by TimeTree author, id and name.
	byAuthor map[string]int
	skips    map[string]int
	rejects  map[string]int
	owners   map[string]int
	authors  map[string]int
	// unmappedAuthors counts the events whose TimeTree author is not recorded
	// as their creator, by author and reason.
	unmappedAuthors map[string]int
	attendees       int
	// withDefaultAttendees counts the events default_attendees will join.
	withDefaultAttendees int
	utcTimed             int
	doubleDisplay        int
	allDay               []string
}

// maxAllDaySamples is how many raw/converted all-day pairs the report shows.
const maxAllDaySamples = 3

// analyze converts every event of one calendar and tallies the findings.
func analyze(d calData, cv *converter) *analysis {
	a := &analysis{
		data:            d,
		conv:            make([]converted, len(d.events)),
		byLabel:         map[string]int{},
		byAuthor:        map[string]int{},
		skips:           map[string]int{},
		rejects:         map[string]int{},
		owners:          map[string]int{},
		authors:         map[string]int{},
		unmappedAuthors: map[string]int{},
	}
	names := map[flexID]string{}
	for _, u := range d.cal.Users {
		names[u.UserID] = u.Name
	}
	for i, e := range d.events {
		c := cv.convert(e)
		a.conv[i] = c
		a.byLabel[c.Label]++
		a.byAuthor[authorText(e.AuthorID, names)]++
		if len(e.Attendees) > 0 {
			a.attendees++
		}
		if !e.AllDay && isUTCName(e.StartTimezone) {
			a.utcTimed++
		}
		if e.StartAt > 0 {
			if t := time.UnixMilli(e.StartAt).In(cv.wsLoc); a.oldest.IsZero() || t.Before(a.oldest) {
				a.oldest = t
			}
		}
		switch {
		case c.Skip != "":
			a.skips[c.Skip]++
		case c.Reject != "":
			a.rejects[c.Reject]++
		default:
			a.owners[c.Owner]++
			a.authors[c.Author]++
			if _, why := cv.author(e.AuthorID); why != "" {
				a.unmappedAuthors[fmt.Sprintf("%s (%s)", authorText(e.AuthorID, names), why)]++
			}
			if len(c.Attendees) > 0 {
				a.withDefaultAttendees++
			}
		}
		if e.AllDay && c.ok() && len(a.allDay) < maxAllDaySamples {
			a.allDay = append(a.allDay, fmt.Sprintf("%s start_at=%d end_at=%d tz=%q -> %s .. %s",
				e.ID, e.StartAt, e.EndAt, e.StartTimezone,
				c.Event.Start.UTC().Format(time.RFC3339), endText(c.Event.End, time.UTC)))
		}
	}
	a.doubleDisplay = doubleDisplayCandidates(d.events, a.conv)
	return a
}

// authorText names a TimeTree author by id and the name the calendar shows.
func authorText(id flexID, names map[flexID]string) string {
	if id == "" || id == "0" {
		return "(no author)"
	}
	return fmt.Sprintf("%s %q", id, names[id])
}

func endText(end time.Time, loc *time.Location) string {
	if end.IsZero() {
		return "(default length)"
	}
	return end.In(loc).Format(time.RFC3339)
}

// tally formats a count map as "key=n" pairs in key order.
func tally(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	s := ""
	for _, k := range sortedKeys(m) {
		if s != "" {
			s += ", "
		}
		s += fmt.Sprintf("%s=%d", k, m[k])
	}
	return s
}

// writeReport prints the dry-run report: every TimeTree calendar, and for the
// mapped ones what would be done.
func writeReport(w io.Writer, all []*analysis, res resolution) {
	plans := map[flexID]calendarPlan{}
	for _, p := range res.Plans {
		plans[p.TT.ID] = p
	}
	for _, a := range all {
		cal := a.data.cal
		fmt.Fprintf(w, "TimeTree calendar %s name=%q alias_code=%q\n", cal.ID, cal.Name, cal.AliasCode)
		plan, mapped := plans[cal.ID]
		if mapped {
			fmt.Fprintf(w, "  mapping: mapped (timetree: %s)\n", plan.Mapping.TimeTree)
		} else {
			fmt.Fprintln(w, "  mapping: not mapped")
		}
		oldest := "none"
		if !a.oldest.IsZero() {
			oldest = a.oldest.Format("2006-01-02")
		}
		fmt.Fprintf(w, "  events: %d, oldest start: %s\n", len(a.data.events), oldest)
		fmt.Fprintf(w, "  by label: %s\n", tally(a.byLabel))
		fmt.Fprintf(w, "  by TimeTree author: %s\n", tally(a.byAuthor))
		fmt.Fprintf(w, "  skipped: %s\n", tally(a.skips))
		fmt.Fprintf(w, "  rejected: %s\n", tally(a.rejects))
		fmt.Fprintf(w, "  with attendees: %d, timed events in UTC: %d, possible double display: %d\n",
			a.attendees, a.utcTimed, a.doubleDisplay)
		for _, s := range a.allDay {
			fmt.Fprintf(w, "  all-day sample: %s\n", s)
		}
		if !mapped {
			continue
		}
		fmt.Fprintf(w, "  by owner: %s\n", tally(a.owners))
		fmt.Fprintf(w, "  by creator: %s\n", tally(a.authors))
		fmt.Fprintf(w, "  authors recorded as the importer: %s\n", tally(a.unmappedAuthors))
		fmt.Fprintf(w, "  default attendees: %d people on %d events\n",
			len(plan.Mapping.DefaultAttendees), a.withDefaultAttendees)
		if plan.ExistingID != 0 {
			fmt.Fprintf(w, "  calendar %q: reuse existing\n", plan.Name)
		} else {
			fmt.Fprintf(w, "  calendar %q: create\n", plan.Name)
		}
	}
	fmt.Fprintf(w, "users to create: %s\n", listOrNone(res.NewUsers))
	writeErrors(w, res.Errors)
}

// writeSyncPlans prints what a sync would do to each mapped calendar.
func writeSyncPlans(w io.Writer, all []*analysis, res resolution, syncs map[flexID]*calendarSync) {
	repeated := map[flexID]int{}
	for _, a := range all {
		repeated[a.data.cal.ID] = a.data.repeated
	}
	for _, p := range res.Plans {
		s := syncs[p.TT.ID]
		fmt.Fprintf(w, "calendar %q: sync plan create=%d update=%d unchanged=%d delete=%d moved=%d kept=%d synced=%d orphaned=%d repeated=%d\n",
			p.Name, s.plan.Create, s.plan.Update, s.plan.Unchanged, len(s.plan.Delete), s.moved, s.plan.Kept,
			s.plan.Synced, s.plan.Orphaned, repeated[p.TT.ID])
	}
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return fmt.Sprint(items)
}

// writeErrors lists every validation error.
func writeErrors(w io.Writer, errs []string) {
	fmt.Fprintf(w, "validation errors: %d\n", len(errs))
	for _, e := range errs {
		fmt.Fprintf(w, "  - %s\n", e)
	}
}
