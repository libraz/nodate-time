package calresolve

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/stretchr/testify/assert"
)

type fakeQuerier struct {
	calendar  generated.Calendar
	member    generated.CalendarMember
	memberErr error
}

func (f fakeQuerier) GetCalendarByPublicID(context.Context, generated.GetCalendarByPublicIDParams) (generated.Calendar, error) {
	return f.calendar, nil
}

func (f fakeQuerier) GetCalendarMember(context.Context, generated.GetCalendarMemberParams) (generated.CalendarMember, error) {
	return f.member, f.memberErr
}

func TestMemberDistinguishesMissingMembershipFromDatabaseFailure(t *testing.T) {
	calendarID := uuid.NewString()
	const workspaceID = 1
	cal := generated.Calendar{ID: 42}

	_, _, err := Member(context.Background(), fakeQuerier{calendar: cal, memberErr: sql.ErrNoRows}, workspaceID, calendarID, 7)
	assert.ErrorIs(t, err, apierrors.CalendarAccessDenied)

	_, _, err = Member(context.Background(), fakeQuerier{calendar: cal, memberErr: errors.New("database offline")}, workspaceID, calendarID, 7)
	assert.ErrorIs(t, err, apierrors.InternalUnexpected)
}

// The role helpers decide who may write and who may administer. They are
// asserted here rather than left implicit because getting one wrong is not
// a visible bug: it silently widens access.
func TestRoleHelpersMatchTheHierarchy(t *testing.T) {
	for _, tc := range []struct {
		role     generated.CalendarMembersRole
		canWrite bool
		canAdmin bool
	}{
		{generated.CalendarMembersRoleOwner, true, true},
		{generated.CalendarMembersRoleManager, true, true},
		{generated.CalendarMembersRoleEditor, true, false},
		{generated.CalendarMembersRoleViewer, false, false},
	} {
		assert.Equal(t, tc.canWrite, CanWrite(tc.role), "CanWrite(%s)", tc.role)
		assert.Equal(t, tc.canAdmin, CanManage(tc.role), "CanManage(%s)", tc.role)
	}
	// An unrecognised role grants nothing. A future role added to the shared
	// contract must be classified deliberately, not admitted by default.
	assert.False(t, CanWrite(generated.CalendarMembersRole("something-new")))
	assert.False(t, CanManage(generated.CalendarMembersRole("something-new")))
}
