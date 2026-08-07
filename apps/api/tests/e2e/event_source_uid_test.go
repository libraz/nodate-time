package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// Recognising a calendar file already imported rests on one unique key, and
// every property the import will lean on is a property of that key rather than
// of the code that will read it. These tests pin the key itself, so the shape
// is settled before anything is built on top of it -- and so that a later
// widening of the key, which would silently stop it catching duplicates, has
// to argue with a test rather than merely compile.
//
// They write rows directly. Going through the import endpoint would test the
// parser's use of the column, which is a separate question from whether the
// column can be misused at all.

// sourceUIDFixture is one tenant with two calendars, which is the smallest
// setup that can tell "unique per calendar" apart from "unique everywhere".
type sourceUIDFixture struct {
	workspaceID uint32
	userID      uint32
	calendarA   uint32
	calendarB   uint32
}

func newSourceUIDFixture(t *testing.T) sourceUIDFixture {
	t.Helper()
	srv := helpers.NewTestServer(t, testDB)
	tenant := helpers.NewTenant(t, srv.BaseURL)

	var second struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, srv.BaseURL+"/calendars", tenant.AccessToken,
		map[string]any{"name": "Second", "color": "#2ECC87"}, &second)
	require.NotEmpty(t, second.ID)

	return sourceUIDFixture{
		workspaceID: helpers.TestWorkspace(generated.New(testDB)).ID,
		userID:      internalIDByPublicID(t, "users", tenant.UserID),
		calendarA:   internalIDByPublicID(t, "calendars", tenant.CalendarID),
		calendarB:   internalIDByPublicID(t, "calendars", second.ID),
	}
}

// internalIDByPublicID resolves a public UUID to the internal key the row is
// joined on.
func internalIDByPublicID(t *testing.T, table, publicID string) uint32 {
	t.Helper()
	parsed, err := uuid.Parse(publicID)
	require.NoError(t, err)
	var id uint32
	require.NoError(t, testDB.QueryRow(
		"SELECT id FROM `"+table+"` WHERE public_id = ?", parsed[:]).Scan(&id))
	return id
}

// uidPtr makes a pointer to a UID literal. The column is nullable and NULL
// says something specific here, so the tests never pass a bare string.
func uidPtr(s string) *string { return &s }

// insertWithSourceUID writes one event carrying the UID a file gave it and
// returns whatever the database made of it. A nil uid writes NULL, which is
// what an event created in the app and what a recurrence override both carry.
func (f sourceUIDFixture) insertWithSourceUID(
	t *testing.T, calendarID uint32, sourceUID *string, enabled bool,
) error {
	t.Helper()
	pub, err := uuid.NewV7()
	require.NoError(t, err)
	_, execErr := testDB.Exec(
		`INSERT INTO calendar_events
		   (public_id, workspace_id, calendar_id, title,
		    owner_user_id, created_by_user_id, source_uid, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		pub[:], f.workspaceID, calendarID, "Imported", f.userID, f.userID, sourceUID, enabled)
	return execErr
}

// requireDuplicateSourceUID asserts the refusal came from this key and not
// from some other uniqueness the row happened to violate.
func requireDuplicateSourceUID(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err, "a second live row with the same UID on one calendar must be refused")
	require.Contains(t, err.Error(), "uniq_calendar_events_source_uid",
		"the refusal must come from the source UID key: %v", err)
}

// The whole point: one calendar cannot hold the same file event twice.
func TestOneCalendarHoldsASourceUIDOnce(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	f := newSourceUIDFixture(t)
	const uid = "meeting-1@example.com"

	require.NoError(t, f.insertWithSourceUID(t, f.calendarA, uidPtr(uid), true))
	requireDuplicateSourceUID(t, f.insertWithSourceUID(t, f.calendarA, uidPtr(uid), true))
}

// A UID is only unique inside the file that supplied it, so two calendars
// importing unrelated files that both call an event `1@example.com` must not
// collide. Scoping the key by calendar is what allows that; a key on the UID
// alone, or one scoped only by workspace, would reject the second calendar's
// event and it would never appear.
func TestTwoCalendarsMayShareASourceUID(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	f := newSourceUIDFixture(t)
	const uid = "1@example.com"

	require.NoError(t, f.insertWithSourceUID(t, f.calendarA, uidPtr(uid), true))
	require.NoError(t, f.insertWithSourceUID(t, f.calendarB, uidPtr(uid), true),
		"the same UID on another calendar is a different event")
}

// Deleting an event here has to release its UID. This table soft-deletes
// through enabled = FALSE, so a key over the raw column would keep the UID
// reserved by a row nobody can see: the user deletes an event, re-imports the
// file that has it, and it can never come back. Nothing in the app could
// explain that, and nothing in the app could undo it.
func TestSoftDeletingAnEventReleasesItsSourceUID(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	f := newSourceUIDFixture(t)
	const uid = "recurring-standup@example.com"

	require.NoError(t, f.insertWithSourceUID(t, f.calendarA, uidPtr(uid), true))
	_, err := testDB.Exec(
		`UPDATE calendar_events SET enabled = FALSE WHERE calendar_id = ? AND source_uid = ?`,
		f.calendarA, uid)
	require.NoError(t, err)

	require.NoError(t, f.insertWithSourceUID(t, f.calendarA, uidPtr(uid), true),
		"an event deleted here must be importable again")

	var total, live int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*), SUM(enabled) FROM calendar_events WHERE calendar_id = ? AND source_uid = ?`,
		f.calendarA, uid).Scan(&total, &live))
	require.Equal(t, 2, total, "the deleted row stays for its history")
	require.Equal(t, 1, live, "but only one of them is on the calendar")
}

// Deleting the same event over and over is ordinary: import, delete, import,
// delete. Every one of those rows keeps its UID, so the key has to tolerate
// any number of them side by side and constrain only the live one.
func TestManyDeletedRowsMayShareASourceUID(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	f := newSourceUIDFixture(t)
	const uid = "deleted-repeatedly@example.com"

	for range 3 {
		require.NoError(t, f.insertWithSourceUID(t, f.calendarA, uidPtr(uid), false))
	}
	require.NoError(t, f.insertWithSourceUID(t, f.calendarA, uidPtr(uid), true),
		"a live row alongside any number of deleted ones is the normal state")
	requireDuplicateSourceUID(t, f.insertWithSourceUID(t, f.calendarA, uidPtr(uid), true))
}

// A UID is opaque, so two that differ only in case are two events. Under the
// schema's usual utf8mb4_0900_ai_ci they would compare equal and the second
// event would be treated as one already imported -- meaning it would never
// arrive. This is why the column overrides the default collation.
func TestSourceUIDComparisonIsCaseSensitive(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	f := newSourceUIDFixture(t)

	require.NoError(t, f.insertWithSourceUID(t, f.calendarA, uidPtr("AbC@example.com"), true))
	require.NoError(t, f.insertWithSourceUID(t, f.calendarA, uidPtr("abc@example.com"), true),
		"UIDs differing only in case are different events")
}

// Events made in the app carry no UID, and neither does a recurrence override:
// it shares its series' UID by design, and is already made unique by its parent
// and the occurrence it replaces. Both are NULL here, and NULLs in a unique key
// are distinct, so neither is constrained by a key that has nothing to say
// about them.
func TestEventsWithoutASourceUIDAreUnconstrained(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	f := newSourceUIDFixture(t)
	for range 5 {
		require.NoError(t, f.insertWithSourceUID(t, f.calendarA, nil, true),
			"an event created here is not competing for an import identity")
	}
}

// The generated column exists only to scope the key, and writing it directly
// would let the two disagree. MySQL refusing the write is what keeps
// source_uid the single place the UID is recorded.
func TestSourceUIDKeyCannotBeWrittenDirectly(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	f := newSourceUIDFixture(t)
	require.NoError(t, f.insertWithSourceUID(t, f.calendarA, uidPtr("generated@example.com"), true))

	_, err := testDB.Exec(
		`UPDATE calendar_events SET source_uid_key = 'forced@example.com' WHERE calendar_id = ?`,
		f.calendarA)
	require.Error(t, err, "a generated column must not be assignable")
	require.True(t, strings.Contains(err.Error(), "source_uid_key"),
		"the refusal must name the generated column: %v", err)
}
