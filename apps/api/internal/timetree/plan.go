package timetree

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// lookup reads the deployment's current state for the pre-write checks.
type lookup interface {
	// user finds an enabled account by address.
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
	u, err := d.q.GetUserByEmail(ctx, email)
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
	TT      ttCalendar
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
		where := fmt.Sprintf("calendars[%d] (%s)", i, c.TimeTree)
		var hits []ttCalendar
		for _, t := range tt {
			if t.Name == c.TimeTree || t.AliasCode == c.TimeTree {
				hits = append(hits, t)
			}
		}
		switch len(hits) {
		case 0:
			add("%s: no active TimeTree calendar has that name or alias_code", where)
			continue
		case 1:
		default:
			add("%s: %d TimeTree calendars match that name or alias_code", where, len(hits))
			continue
		}
		plan := calendarPlan{Mapping: c, TT: hits[0], Name: c.Name}
		if plan.Name == "" {
			plan.Name = hits[0].Name
		}
		known := map[string]bool{}
		for _, l := range labels[hits[0].ID] {
			known[l.Name] = true
		}
		for _, label := range sortedKeys(c.Labels) {
			if !known[label] {
				add("%s: labels[%q] matches no label of the TimeTree calendar", where, label)
			}
		}
		res.Plans = append(res.Plans, plan)
	}

	// Nothing else can be said about names until every entry has one.
	names := map[string]string{}
	for _, p := range res.Plans {
		if prev, dup := names[p.Name]; dup {
			add("calendars %s and %s resolve to the same name %q", prev, p.Mapping.TimeTree, p.Name)
		}
		names[p.Name] = p.Mapping.TimeTree
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
		if !exists {
			if !inUsers[key] {
				add("%s %q is not in users and is not an existing account", r.what, r.email)
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
			add("%s %q is not a member of the workspace", r.what, r.email)
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
		var ids []uint32
		for _, c := range owned {
			if c.Name == p.Name && !c.OwnerUserID.Valid {
				ids = append(ids, c.ID)
			}
		}
		switch len(ids) {
		case 0:
		case 1:
			p.ExistingID = ids[0]
		default:
			add("calendar %q: the importer is a member of %d shared calendars with that name", p.Name, len(ids))
		}
	}
	return res, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
