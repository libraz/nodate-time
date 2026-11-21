package timetree

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/libraz/nodate-time/apps/api/internal/calimport"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// fakeLookup is an in-memory deployment.
type fakeLookup struct {
	users    map[string]uint32 // normalized email → id
	disabled map[uint32]bool   // ids of disabled accounts
	outside  map[uint32]bool   // ids removed from the workspace
	owned    map[uint32][]generated.ListCalendarsByUserRow
}

func (f fakeLookup) user(_ context.Context, email string) (generated.User, bool, error) {
	id, ok := f.users[norm(email)]
	return generated.User{ID: id, Enabled: !f.disabled[id]}, ok, nil
}

func (f fakeLookup) workspaceMember(_ context.Context, id uint32) (bool, error) {
	return !f.outside[id], nil
}

func (f fakeLookup) calendars(_ context.Context, id uint32) ([]generated.ListCalendarsByUserRow, error) {
	return f.owned[id], nil
}

func cal(id uint32, name string, personal bool) generated.ListCalendarsByUserRow {
	return generated.ListCalendarsByUserRow{
		ID: id, Name: name, OwnerUserID: sql.NullInt32{Int32: 7, Valid: personal},
		MemberRole: generated.CalendarMembersRoleOwner,
	}
}

var (
	ttCals = []ttCalendar{
		{ID: "1", Name: "Band", AliasCode: "alias-one"},
		{ID: "2", Name: "Second", AliasCode: "Band2"},
		{ID: "3", Name: "Dup", AliasCode: "x"},
		{ID: "4", Name: "Dup", AliasCode: "y"},
	}
	ttLabels = map[flexID][]ttLabel{"1": {{ID: "10", Name: "花子"}, {ID: "11", Name: "花子"}}}
)

func resolveWith(t *testing.T, m *Mapping, lk fakeLookup) resolution {
	t.Helper()
	res, err := resolve(context.Background(), m, ttCals, ttLabels, lk)
	require.NoError(t, err)
	return res
}

func existing() fakeLookup {
	return fakeLookup{users: map[string]uint32{"ops@example.com": 1, "hanako@example.com": 2, "mgr@example.com": 3}}
}

func TestResolveAcceptsAConsistentMapping(t *testing.T) {
	res := resolveWith(t, mustParse(t, goodMapping), existing())
	assert.Empty(t, res.Errors)
	require.Len(t, res.Plans, 1)
	assert.Equal(t, "Team", res.Plans[0].Name)
	assert.Equal(t, flexID("1"), res.Plans[0].TT.ID)
	assert.Zero(t, res.Plans[0].ExistingID)
	assert.Empty(t, res.NewUsers)
}

func TestResolveTimeTreeMatchesNameOrAliasExactlyOnce(t *testing.T) {
	for in, want := range map[string]flexID{"Band": "1", "alias-one": "1", "Second": "2", "Band2": "2"} {
		m := mustParse(t, goodMapping)
		m.Calendars[0].TimeTree = in
		m.Calendars[0].Labels = nil
		res := resolveWith(t, m, existing())
		require.Empty(t, res.Errors, in)
		assert.Equal(t, want, res.Plans[0].TT.ID, in)
	}
	for in, msg := range map[string]string{"nothing": "no active TimeTree calendar", "Dup": "2 TimeTree calendars match"} {
		m := mustParse(t, goodMapping)
		m.Calendars[0].TimeTree = in
		res := resolveWith(t, m, existing())
		require.Len(t, res.Errors, 1, in)
		assert.Contains(t, res.Errors[0], msg)
	}
}

func TestResolveNameDefaultsToTheTimeTreeName(t *testing.T) {
	m := mustParse(t, goodMapping)
	m.Calendars[0].Name = ""
	assert.Equal(t, "Band", resolveWith(t, m, existing()).Plans[0].Name)
}

func TestResolveLabelKeysMustMatchATimeTreeLabel(t *testing.T) {
	m := mustParse(t, goodMapping)
	m.Calendars[0].Labels["はなこ"] = "hanako@example.com"
	res := resolveWith(t, m, existing())
	require.Len(t, res.Errors, 1)
	assert.Contains(t, res.Errors[0], "はなこ")

	m = mustParse(t, goodMapping)
	delete(m.Calendars[0].Labels, "花子")
	m.Calendars[0].Labels["ﾊﾅｺ"] = "hanako@example.com" // not normalized to anything
	assert.Len(t, resolveWith(t, m, existing()).Errors, 1)
}

func TestResolveReferencedAddressesMustExistOrBeCreated(t *testing.T) {
	m := mustParse(t, goodMapping)
	res := resolveWith(t, m, fakeLookup{users: map[string]uint32{}})
	require.Len(t, res.Errors, 1, "%v", res.Errors)
	assert.Contains(t, res.Errors[0], "importer")
	assert.Equal(t, []string{"hanako@example.com", "mgr@example.com"}, res.NewUsers)

	// The importer may be created by the same run.
	m.Users = append(m.Users, MappingUser{Email: "ops@example.com"})
	res = resolveWith(t, m, fakeLookup{users: map[string]uint32{}})
	assert.Empty(t, res.Errors)
	assert.Len(t, res.NewUsers, 3)

	// A member who is neither listed nor existing is an error.
	m = mustParse(t, goodMapping)
	m.Users = m.Users[:1]
	res = resolveWith(t, m, fakeLookup{users: map[string]uint32{"ops@example.com": 1}})
	require.Len(t, res.Errors, 1)
	assert.Contains(t, res.Errors[0], "mgr@example.com")
}

func TestResolveRefusesAccountsRemovedFromTheWorkspace(t *testing.T) {
	lk := existing()
	lk.outside = map[uint32]bool{2: true}
	res := resolveWith(t, mustParse(t, goodMapping), lk)
	require.NotEmpty(t, res.Errors)
	assert.Contains(t, res.Errors[0], "hanako@example.com")
	assert.Contains(t, res.Errors[0], "not a member of the workspace")

	// The importer too.
	lk = existing()
	lk.outside = map[uint32]bool{1: true}
	res = resolveWith(t, mustParse(t, goodMapping), lk)
	require.NotEmpty(t, res.Errors)
	assert.Contains(t, res.Errors[0], "importer")
}

func TestResolveReusesExactlyOneSharedCalendarOfTheSameName(t *testing.T) {
	m := mustParse(t, goodMapping)

	lk := existing()
	res := resolveWith(t, m, lk)
	assert.Zero(t, res.Plans[0].ExistingID, "none: create")

	lk.owned = map[uint32][]generated.ListCalendarsByUserRow{1: {cal(50, "Team", false), cal(51, "team", false), cal(52, "Team ", false)}}
	res = resolveWith(t, m, lk)
	assert.Equal(t, uint32(50), res.Plans[0].ExistingID, "one exact match: reuse")

	lk.owned = map[uint32][]generated.ListCalendarsByUserRow{1: {cal(50, "Team", false), cal(53, "Team", false)}}
	res = resolveWith(t, m, lk)
	require.Len(t, res.Errors, 1)
	assert.Contains(t, res.Errors[0], "2 shared calendars")

	lk.owned = map[uint32][]generated.ListCalendarsByUserRow{1: {cal(50, "Team", true), cal(53, "Team", false)}}
	res = resolveWith(t, m, lk)
	assert.Equal(t, uint32(53), res.Plans[0].ExistingID, "a personal calendar is not a candidate")

	lk.owned = map[uint32][]generated.ListCalendarsByUserRow{1: {cal(50, "Team", true)}}
	assert.Zero(t, resolveWith(t, m, lk).Plans[0].ExistingID)
}

func TestResolveCalendarNamesMustBeDistinctAfterDefaulting(t *testing.T) {
	m := mustParse(t, goodMapping)
	m.Calendars[0].Name = "Second"
	m.Calendars = append(m.Calendars, MappingCalendar{TimeTree: "Second"}) // defaults to "Second"
	res := resolveWith(t, m, existing())
	require.Len(t, res.Errors, 1)
	assert.Contains(t, res.Errors[0], "same name")
}

func TestResolveRefusesAnExistingCalendarTheImporterCannotManage(t *testing.T) {
	for _, role := range []generated.CalendarMembersRole{generated.CalendarMembersRoleEditor, generated.CalendarMembersRoleViewer} {
		lk := existing()
		c := cal(50, "Team", false)
		c.MemberRole = role
		lk.owned = map[uint32][]generated.ListCalendarsByUserRow{1: {c}}
		res := resolveWith(t, mustParse(t, goodMapping), lk)
		require.Len(t, res.Errors, 1, string(role))
		assert.Contains(t, res.Errors[0], "cannot manage")
		assert.Zero(t, res.Plans[0].ExistingID, string(role))
	}

	lk := existing()
	c := cal(50, "Team", false)
	c.MemberRole = generated.CalendarMembersRoleManager
	lk.owned = map[uint32][]generated.ListCalendarsByUserRow{1: {c}}
	res := resolveWith(t, mustParse(t, goodMapping), lk)
	assert.Empty(t, res.Errors)
	assert.Equal(t, uint32(50), res.Plans[0].ExistingID)
}

func TestResolveRefusesAddressesHeldByADisabledAccount(t *testing.T) {
	for id, who := range map[uint32]string{1: "ops@example.com", 2: "hanako@example.com"} {
		lk := existing()
		lk.disabled = map[uint32]bool{id: true}
		res := resolveWith(t, mustParse(t, goodMapping), lk)
		require.NotEmpty(t, res.Errors, who)
		assert.Contains(t, res.Errors[0], who)
		assert.Contains(t, res.Errors[0], "belongs to a disabled account")
		assert.Empty(t, res.NewUsers, "a disabled holder is not an absent address")
	}
}

func ids(list ...string) map[string]bool {
	out := map[string]bool{}
	for _, id := range list {
		out[id] = true
	}
	return out
}

func TestCountMovedFindsDeletionsAnotherMappedCalendarHas(t *testing.T) {
	plans := []calendarPlan{{TT: ttCalendar{ID: "1"}}, {TT: ttCalendar{ID: "2"}}}
	syncs := map[flexID]*calendarSync{
		"1": {present: ids("x"), plan: calimport.SyncPlan{Delete: []uint32{1, 2, 3}, DeleteUIDs: []string{"m1", "m2", "gone"}}},
		"2": {present: ids("m1", "m2")},
	}
	countMoved(plans, syncs)
	assert.Equal(t, 2, syncs["1"].moved)
	assert.Equal(t, 0, syncs["2"].moved)
}

func TestRefusals(t *testing.T) {
	sync := func(present map[string]bool, synced, deletes, moved int) *calendarSync {
		return &calendarSync{
			present: present, moved: moved,
			plan: calimport.SyncPlan{Synced: synced, Delete: make([]uint32, deletes)},
		}
	}
	plans := []calendarPlan{{TT: ttCalendar{ID: "1"}, Name: "A"}}
	cases := map[string]struct {
		s    *calendarSync
		want string
	}{
		"11 of 12":           {sync(ids("a"), 12, 11, 0), `calendar "A" would delete 11 of 12 synced events`},
		"11 of 22":           {sync(ids("a"), 22, 11, 0), ""},
		"10 of 10":           {sync(ids("a"), 11, 10, 0), ""},
		"11 of 12, 1 moved":  {sync(ids("a"), 12, 11, 1), ""},
		"empty feed":         {sync(ids(), 3, 3, 0), `TimeTree returned no events for calendar "A" but 3 are synced`},
		"empty, all moved":   {sync(ids(), 11, 11, 11), ""},
		"empty, none synced": {sync(ids(), 0, 0, 0), ""},
	}
	for name, tc := range cases {
		got := refusals(plans, map[flexID]*calendarSync{"1": tc.s})
		if tc.want == "" {
			assert.Empty(t, got, name)
		} else {
			assert.Equal(t, []string{tc.want}, got, name)
		}
	}
}

func TestImportableForcesEventsNamingSomeoneNotYetCreated(t *testing.T) {
	cv := testConverter(&MappingCalendar{
		DefaultOwner: "mgr@example.com", DefaultAttendees: []string{"new@example.com"},
		Labels: map[string]string{"花子": "h@example.com"},
	})
	labelled := timed(jst(2026, 5, 1, 10), jst(2026, 5, 1, 11))
	labelled.ID, labelled.LabelID = "labelled", "1"
	plain := timed(jst(2026, 5, 1, 10), jst(2026, 5, 1, 11))
	plain.ID = "plain"
	a := analyze(calData{events: []ttEvent{labelled, plain}}, cv)
	users := map[string]generated.User{"h@example.com": {ID: 5}, "mgr@example.com": {ID: 6}}
	events, force := importable(a, users)
	require.Len(t, events, 2)
	assert.Equal(t, uint32(5), events[0].OwnerUserID)
	assert.Equal(t, uint32(6), events[1].OwnerUserID)
	assert.Empty(t, events[1].Attendees)
	assert.Equal(t, ids("plain"), force)
}
