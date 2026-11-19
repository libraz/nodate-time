package e2e

import (
	"database/sql"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/availability"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// One transaction exercises the same sqlc queries used during finalization,
// including the case where only a moved child matches the requested members.
func TestMySQLAvailabilityUsesEffectiveOccurrenceAndSharedCalendarConsent(t *testing.T) {
	bootstrap(t)
	ctx := t.Context()
	tx, err := testDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	q := generated.New(tx)
	insert := func(statement string, args ...any) uint32 {
		t.Helper()
		result, err := tx.ExecContext(ctx, statement, args...)
		require.NoError(t, err)
		id, err := result.LastInsertId()
		require.NoError(t, err)
		return uint32(id)
	}
	workspace := insert("INSERT INTO workspaces (public_id, slug, name) VALUES(UUID_TO_BIN(UUID()), UUID(), 'Availability test')")
	newUser := func() uint32 {
		t.Helper()
		id := insert("INSERT INTO users(public_id,email,display_name) VALUES(UUID_TO_BIN(UUID()),CONCAT(UUID(),'@example.test'),'Member')")
		insert("INSERT INTO workspace_members(public_id,workspace_id,user_id) VALUES(UUID_TO_BIN(UUID()),?,?)", workspace, id)
		return id
	}
	owner, guest, other := newUser(), newUser(), newUser()
	calendar := func(ws uint32, members ...uint32) uint32 {
		t.Helper()
		id := insert("INSERT INTO calendars(public_id,workspace_id,name) VALUES(UUID_TO_BIN(UUID()),?,'Calendar')", ws)
		for _, m := range members {
			insert("INSERT INTO calendar_members(public_id,workspace_id,calendar_id,user_id) VALUES(UUID_TO_BIN(UUID()),?,?,?)", ws, id, m)
		}
		return id
	}
	target := calendar(workspace, owner, guest, other)
	source := calendar(workspace, owner, guest)
	guestOnly := calendar(workspace, guest)
	foreignWorkspace := insert("INSERT INTO workspaces(public_id,slug,name) VALUES(UUID_TO_BIN(UUID()), UUID(), 'Other workspace')")
	for _, member := range []uint32{owner, guest} {
		insert("INSERT INTO workspace_members(public_id,workspace_id,user_id) VALUES(UUID_TO_BIN(UUID()),?,?)", foreignWorkspace, member)
	}
	foreignSource := calendar(foreignWorkspace, owner, guest)
	start := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	slot := func(day int) availability.Slot {
		s := start.AddDate(0, 0, day)
		return availability.Slot{Start: s, End: s.Add(time.Hour)}
	}
	event := func(ws, cal, user uint32, day int) uint32 {
		t.Helper()
		span := slot(day)
		return insert(`INSERT INTO calendar_events(public_id,workspace_id,calendar_id,title,owner_user_id,created_by_user_id,start_at,end_at)
			VALUES(UUID_TO_BIN(UUID()),?,?,'Confidential title',?,?,?,?)`, ws, cal, user, user, span.Start, span.End)
	}
	attendee := func(eventID, person uint32, status string) {
		t.Helper()
		insert(`INSERT INTO calendar_event_attendees(public_id,workspace_id,event_id,user_id,rsvp)
			VALUES(UUID_TO_BIN(UUID()),?,?,?,?)`, workspace, eventID, person, status)
	}
	series := event(workspace, target, owner, 0)
	insert(`UPDATE calendar_events SET recurrence_rule='{"freq":"daily","count":3}',
		recurrence_end=?, recurrence_exceptions=JSON_ARRAY(?) WHERE id=?`, slot(2).End, slot(2).Start.Format(time.RFC3339), series)
	attendee(series, guest, "accepted")
	moved := event(workspace, target, owner, 6)
	insert("UPDATE calendar_events SET recurrence_parent_id=?,recurrence_original_start=?,flexibility='conditional' WHERE id=?", series, slot(1).Start, moved)
	attendee(moved, guest, "declined")
	attendee(moved, other, "pending")
	standalone := event(workspace, target, owner, 5)
	attendee(standalone, guest, "declined")
	away := event(workspace, source, guest, 4)
	insert("UPDATE calendar_events SET visibility='confidential' WHERE id=?", away)
	event(workspace, guestOnly, guest, 3)
	event(foreignWorkspace, foreignSource, guest, 3)
	for _, calendarID := range []uint32{guestOnly, foreignSource} {
		require.NoError(t, q.SetScheduleAvailabilitySharing(ctx, generated.SetScheduleAvailabilitySharingParams{CalendarID: calendarID, Enabled: true}))
	}

	const open, soft, hard = availability.Open, availability.Soft, availability.Hard
	check := func(person uint32, slots []availability.Slot, expected ...availability.State) {
		t.Helper()
		got, err := availability.Check(ctx, q, workspace, target, []uint32{person}, slots)
		require.NoError(t, err)
		require.Equal(t, expected, got)
	}
	slots := []availability.Slot{slot(0), slot(1), slot(2), slot(3), slot(4), slot(5), slot(6)}
	check(guest, slots, hard, open, open, open, open, open, open)
	check(other, []availability.Slot{slot(6)}, soft)
	check(owner, slots, hard, open, open, open, open, hard, soft)
	require.NoError(t, q.SetScheduleAvailabilitySharing(ctx, generated.SetScheduleAvailabilitySharingParams{CalendarID: source, Enabled: true}))
	check(guest, slots, hard, open, open, open, hard, open, open)
	// A series' stored recurrence_end was computed from the original 24h
	// duration. Its final fall-back Sunday has 25h in this zone.
	fall := event(workspace, target, owner, 0)
	insert(`UPDATE calendar_events SET start_at='2026-10-25 04:00', end_at='2026-10-26 04:00',
		recurrence_end='2026-11-02 04:00', timezone='America/New_York', all_day=TRUE,
		recurrence_rule='{"freq":"weekly","count":2}' WHERE id=?`, fall)
	lastHour := time.Date(2026, 11, 2, 4, 30, 0, 0, time.UTC)
	check(owner, []availability.Slot{{Start: lastHour, End: lastHour.Add(time.Minute)}}, hard)
	insert("UPDATE calendar_event_attendees SET enabled=FALSE WHERE event_id=? AND user_id=?", series, guest)
	check(guest, []availability.Slot{slot(0)}, open)
	insert("UPDATE calendar_members SET enabled=FALSE WHERE calendar_id=? AND user_id=?", source, guest)
	check(guest, []availability.Slot{slot(4)}, open)
	insert("UPDATE workspace_members SET enabled=FALSE WHERE workspace_id=? AND user_id=?", workspace, guest)
	_, err = availability.Check(ctx, q, workspace, target, []uint32{guest}, slots)
	require.ErrorIs(t, err, availability.ErrMembership)
}

// Hold a finalizer's availability read open on one connection. A second
// connection proves the database protects the empty event range, including
// the case where there was no existing event row there to take a lock on.
func TestScheduleAvailabilitySerializableCheckLocksNewNormalEvent(t *testing.T) {
	bootstrap(t)
	tenant := helpers.NewTenant(t, testServerURL)
	ctx := t.Context()
	var workspaceID, calendarID, userID uint32
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT c.workspace_id, c.id, u.id
		FROM calendars c JOIN users u ON u.public_id = UUID_TO_BIN(?) WHERE c.public_id = UUID_TO_BIN(?)`,
		tenant.UserID, tenant.CalendarID).Scan(&workspaceID, &calendarID, &userID))
	start := time.Now().UTC().Truncate(time.Hour).AddDate(0, 1, 0)
	slots := []availability.Slot{{Start: start, End: start.Add(time.Hour)}}
	members := []uint32{userID}
	reader, err := testDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	require.NoError(t, err)
	defer reader.Rollback()
	states, err := availability.Check(ctx, generated.New(reader), workspaceID, calendarID, members, slots)
	require.NoError(t, err)
	require.Equal(t, []availability.State{availability.Open}, states)

	conn, err := testDB.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(ctx, "SET SESSION innodb_lock_wait_timeout=1")
	require.NoError(t, err)
	defer conn.ExecContext(ctx, "SET SESSION innodb_lock_wait_timeout=DEFAULT")
	writer, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	addEvent := func() error {
		_, err := writer.ExecContext(ctx, `INSERT INTO calendar_events
			(public_id, workspace_id, calendar_id, title, start_at, end_at, owner_user_id, created_by_user_id)
			VALUES(UUID_TO_BIN(UUID()), ?, ?, 'Normal event', ?, ?, ?, ?)`,
			workspaceID, calendarID, slots[0].Start, slots[0].End, userID, userID)
		return err
	}
	err = addEvent()
	var lockErr *mysql.MySQLError
	require.ErrorAs(t, err, &lockErr)
	require.EqualValues(t, 1205, lockErr.Number, "INSERT waited on the open availability check")
	require.NoError(t, reader.Commit())
	require.NoError(t, addEvent())
	states, err = availability.Check(ctx, generated.New(writer), workspaceID, calendarID, members, slots)
	require.NoError(t, err)
	require.Equal(t, []availability.State{availability.Hard}, states)
}
