package timetree

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// fakeLookup is an in-memory deployment.
type fakeLookup struct {
	users   map[string]uint32 // normalized email → id
	outside map[uint32]bool   // ids removed from the workspace
	owned   map[uint32][]generated.ListCalendarsByUserRow
}

func (f fakeLookup) user(_ context.Context, email string) (generated.User, bool, error) {
	id, ok := f.users[norm(email)]
	return generated.User{ID: id}, ok, nil
}

func (f fakeLookup) workspaceMember(_ context.Context, id uint32) (bool, error) {
	return !f.outside[id], nil
}

func (f fakeLookup) calendars(_ context.Context, id uint32) ([]generated.ListCalendarsByUserRow, error) {
	return f.owned[id], nil
}

func cal(id uint32, name string, personal bool) generated.ListCalendarsByUserRow {
	return generated.ListCalendarsByUserRow{ID: id, Name: name, OwnerUserID: sql.NullInt32{Int32: 7, Valid: personal}}
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
