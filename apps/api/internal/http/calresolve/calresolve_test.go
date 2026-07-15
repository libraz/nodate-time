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

func (f fakeQuerier) GetCalendarByPublicID(context.Context, []byte) (generated.Calendar, error) {
	return f.calendar, nil
}

func (f fakeQuerier) GetCalendarMember(context.Context, generated.GetCalendarMemberParams) (generated.CalendarMember, error) {
	return f.member, f.memberErr
}

func TestMemberDistinguishesMissingMembershipFromDatabaseFailure(t *testing.T) {
	calendarID := uuid.NewString()
	cal := generated.Calendar{ID: 42}

	_, _, err := Member(context.Background(), fakeQuerier{calendar: cal, memberErr: sql.ErrNoRows}, calendarID, 7)
	assert.ErrorIs(t, err, apierrors.CalendarAccessDenied)

	_, _, err = Member(context.Background(), fakeQuerier{calendar: cal, memberErr: errors.New("database offline")}, calendarID, 7)
	assert.ErrorIs(t, err, apierrors.InternalUnexpected)
}
