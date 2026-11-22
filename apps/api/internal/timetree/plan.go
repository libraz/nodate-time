package timetree

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/libraz/nodate-time/apps/api/internal/attendees"
	"github.com/libraz/nodate-time/apps/api/internal/calimport"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
)

// lookup reads the deployment's current state for the pre-write checks.
type lookup interface {
	// user finds an account by address, disabled ones included.
	user(ctx context.Context, email string) (generated.User, bool, error)
	// workspaceMember reports whether the account belongs to the workspace.
	workspaceMember(ctx context.Context, userID uint32) (bool, error)
	// calendars lists the calendars the account is an active member of.
	calendars(ctx context.Context, userID uint32) ([]generated.ListCalendarsByUserRow, error)
}

// dbLookup is lookup over the database. Without a workspace nobody exists yet.
type dbLookup struct {
	q           *generated.Queries
	workspaceID uint32
	found       bool
}

func (d dbLookup) user(ctx context.Context, email string) (generated.User, bool, error) {
	if !d.found {
		return generated.User{}, false, nil
	}
	u, err := d.q.GetUserByEmailAnyState(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return generated.User{}, false, nil
	}
	return u, err == nil, err
}

func (d dbLookup) workspaceMember(ctx context.Context, userID uint32) (bool, error) {
	_, err := d.q.GetWorkspaceMember(ctx, generated.GetWorkspaceMemberParams{WorkspaceID: d.workspaceID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (d dbLookup) calendars(ctx context.Context, userID uint32) ([]generated.ListCalendarsByUserRow, error) {
	return d.q.ListCalendarsByUser(ctx, generated.ListCalendarsByUserParams{UserID: userID, WorkspaceID: d.workspaceID})
}

// calendarPlan is one mapped calendar after it met TimeTree and the database.
type calendarPlan struct {
	Mapping MappingCalendar
	// Index is the position in the mapping's calendars, for error paths.
	Index int
	TT    ttCalendar
	// Name is the calendar's name here.
	Name string
	// ExistingID is the calendar that is reused; 0 means it is created.
	ExistingID uint32
}

// resolution is the outcome of the pre-write checks.
type resolution struct {
	Plans []calendarPlan
	// NewUsers are the mapping's users that do not exist yet.
	NewUsers []string
	Errors   []string
}

// resolve matches the mapping against TimeTree and the database. Every
// problem is collected so the operator fixes the mapping once.
func resolve(ctx context.Context, m *Mapping, tt []ttCalendar, labels map[flexID][]ttLabel, lk lookup) (resolution, error) {
	var res resolution
	add := func(format string, a ...any) { res.Errors = append(res.Errors, fmt.Sprintf(format, a...)) }

	for i, c := range m.Calendars {
		where := fmt.Sprintf("calendars[%d]", i)
		var hits []ttCalendar
		for _, t := range tt {
			if t.Name == c.Source || t.AliasCode == c.Source {
				hits = append(hits, t)
			}
		}
		switch len(hits) {
		case 0:
			add("%s.source: no active TimeTree calendar has that name or alias_code", where)
			continue
		case 1:
		default:
			add("%s.source: %d TimeTree calendars match that name or alias_code", where, len(hits))
			continue
		}
		plan := calendarPlan{Mapping: c, Index: i, TT: hits[0], Name: c.Name}
		if plan.Name == "" {
			plan.Name = hits[0].Name
		}
		known := map[string]bool{}
		for _, l := range labels[hits[0].ID] {
			known[l.Name] = true
		}
		for _, label := range sortedKeys(c.Labels) {
			if !known[label] {
				add("%s.labels[%q]: matches no label of the TimeTree calendar", where, label)
			}
		}
		res.Plans = append(res.Plans, plan)
	}

	// An author id no mapped calendar has is a typo or a person of another one.
	people := map[flexID]bool{}
	for _, p := range res.Plans {
		for _, u := range p.TT.Users {
			people[u.UserID] = true
		}
	}
	for _, id := range sortedKeys(m.SourceUsers) {
		if !people[flexID(id)] {
			add("source_users[%q]: no mapped TimeTree calendar has that user", id)
		}
	}

	// Nothing else can be said about names until every entry has one.
	names := map[string]int{}
	for _, p := range res.Plans {
		if prev, dup := names[p.Name]; dup {
			add("calendars[%d].source: resolves to the same name %q as calendars[%d]", p.Index, p.Name, prev)
		}
		names[p.Name] = p.Index
	}

	inUsers := map[string]bool{}
	for _, u := range m.Users {
		inUsers[norm(u.Email)] = true
	}
	seen := map[string]bool{}
	var importerID uint32
	importerExists := false
	for _, r := range m.refs() {
		key := norm(r.email)
		if seen[key] {
			continue
		}
		seen[key] = true
		u, exists, err := lk.user(ctx, r.email)
		if err != nil {
			return res, fmt.Errorf("look up %s: %w", r.email, err)
		}
		if key == norm(m.Importer) {
			importerID, importerExists = u.ID, exists
		}
		if exists && !u.Enabled {
			add("%s: %q belongs to a disabled account", r.path, r.email)
			if key == norm(m.Importer) {
				importerExists = false
			}
			continue
		}
		if !exists {
			if !inUsers[key] {
				add("%s: %q is not in users and is not an existing account", r.path, r.email)
			} else {
				res.NewUsers = append(res.NewUsers, r.email)
			}
			continue
		}
		member, err := lk.workspaceMember(ctx, u.ID)
		if err != nil {
			return res, fmt.Errorf("look up workspace membership of %s: %w", r.email, err)
		}
		if !member {
			add("%s: %q is not a member of the workspace", r.path, r.email)
		}
	}

	// A calendar is reused when the importer already belongs to exactly one
	// shared calendar of that name.
	if !importerExists {
		return res, nil
	}
	owned, err := lk.calendars(ctx, importerID)
	if err != nil {
		return res, fmt.Errorf("list calendars of the importer: %w", err)
	}
	for i := range res.Plans {
		p := &res.Plans[i]
		var hits []generated.ListCalendarsByUserRow
		for _, c := range owned {
			if c.Name == p.Name && !c.OwnerUserID.Valid {
				hits = append(hits, c)
			}
		}
		switch len(hits) {
		case 0:
		case 1:
			if !calresolve.CanManage(hits[0].MemberRole) {
				add("calendars[%d].source: the importer cannot manage the existing shared calendar %q", p.Index, p.Name)
				continue
			}
			p.ExistingID = hits[0].ID
		default:
			add("calendars[%d].source: the importer is a member of %d shared calendars named %q", p.Index, len(hits), p.Name)
		}
	}
	return res, nil
}

// maxUnguardedDeletes is how many deletions one calendar may take regardless
// of its size: small losses are accepted, as a soft delete can be undone.
const maxUnguardedDeletes = 10

// calendarSync is what a sync knows about one mapped calendar before writing.
type calendarSync struct {
	// present is every event ID TimeTree still has, deleted ones excluded.
	present map[string]bool
	plan    calimport.SyncPlan
	// moved is the deletions whose ID another mapped calendar now has.
	moved int
}

// presentIDs is the IDs of a calendar's events that TimeTree has not deleted.
func presentIDs(a *analysis) map[string]bool {
	present := map[string]bool{}
	for i, e := range a.data.events {
		if a.conv[i].Skip != skipDeleted {
			present[e.ID] = true
		}
	}
	return present
}

// importable turns a calendar's converted events into calimport events filed
// under users. force collects the IDs naming someone users does not hold.
func importable(a *analysis, users map[string]generated.User) (events []calimport.Event, force map[string]bool) {
	force = map[string]bool{}
	for _, cv := range a.conv {
		if !cv.ok() {
			continue
		}
		ev := cv.Event
		owner, ok := users[cv.Owner]
		if !ok {
			force[ev.SourceUID] = true
		}
		ev.OwnerUserID = owner.ID
		author, ok := users[cv.Author]
		if !ok {
			force[ev.SourceUID] = true
		}
		ev.CreatedByUserID = author.ID
		for _, email := range cv.Attendees {
			u, ok := users[email]
			if !ok {
				force[ev.SourceUID] = true
				continue
			}
			ev.Attendees = append(ev.Attendees, attendees.Person{
				UserID: u.ID, PublicID: calresolve.PublicIDString(u.PublicID), Name: u.DisplayName,
			})
		}
		events = append(events, ev)
	}
	return events, force
}

// planSync reads what a sync would do to every mapped calendar. A calendar
// that does not exist yet would receive every importable event.
func planSync(
	ctx context.Context, q *generated.Queries, m *Mapping, res resolution, analyses []*analysis, lk lookup,
) (map[flexID]*calendarSync, error) {
	users := map[string]generated.User{}
	for _, r := range m.refs() {
		u, exists, err := lk.user(ctx, r.email)
		if err != nil {
			return nil, fmt.Errorf("look up %s: %w", r.email, err)
		}
		if exists {
			users[norm(r.email)] = u
		}
	}
	analysisOf := map[flexID]*analysis{}
	for _, a := range analyses {
		analysisOf[a.data.cal.ID] = a
	}

	syncs := map[flexID]*calendarSync{}
	for _, p := range res.Plans {
		a := analysisOf[p.TT.ID]
		s := &calendarSync{present: presentIDs(a)}
		syncs[p.TT.ID] = s
		events, force := importable(a, users)
		if p.ExistingID == 0 {
			s.plan.Create = len(events)
			continue
		}
		cal, err := q.GetCalendarByID(ctx, p.ExistingID)
		if err != nil {
			return nil, fmt.Errorf("calendar %q: %w", p.Name, err)
		}
		s.plan, err = calimport.PlanSync(ctx, q, cal, users[norm(m.Importer)].ID, events, s.present, force, sourceTimeTree)
		if err != nil {
			return nil, fmt.Errorf("calendar %q: plan sync: %w", p.Name, err)
		}
	}
	countMoved(res.Plans, syncs)
	return syncs, nil
}

// countMoved marks each calendar's deletions that another mapped calendar
// still has: the event moved rather than disappeared.
func countMoved(plans []calendarPlan, syncs map[flexID]*calendarSync) {
	for _, p := range plans {
		s := syncs[p.TT.ID]
		for _, uid := range s.plan.DeleteUIDs {
			for _, other := range plans {
				if other.TT.ID != p.TT.ID && syncs[other.TT.ID].present[uid] {
					s.moved++
					break
				}
			}
		}
	}
}

// Reasons the deletion guard refuses a calendar.
const (
	refuseEmptySource = "empty-source"
	refuseMassDelete  = "mass-delete"
)

// guard decides the deletion guard for one calendar: candidates is the deletion
// candidate count and moved the candidates another mapped calendar now has. It returns
// the refusal reason, or "", and the deletions that would be lost.
func guard(present, synced, candidates, moved int) (reason string, lost int) {
	lost = candidates - moved
	switch {
	case present == 0 && synced > 0 && lost > 0:
		return refuseEmptySource, lost
	case lost > maxUnguardedDeletes && lost*2 > synced:
		return refuseMassDelete, lost
	}
	return "", lost
}

// refusals lists the calendars a sync would empty or mostly empty. A fetch
// that came back partial, or from another account, looks exactly like that.
func refusals(plans []calendarPlan, syncs map[flexID]*calendarSync) []string {
	var out []string
	for _, p := range plans {
		s := syncs[p.TT.ID]
		reason, lost := guard(len(s.present), s.plan.Synced, len(s.plan.Delete), s.moved)
		switch reason {
		case refuseEmptySource:
			out = append(out, fmt.Sprintf("TimeTree returned no events for calendar %q but %d are synced", p.Name, s.plan.Synced))
		case refuseMassDelete:
			out = append(out, fmt.Sprintf("calendar %q would delete %d of %d synced events", p.Name, lost, s.plan.Synced))
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
