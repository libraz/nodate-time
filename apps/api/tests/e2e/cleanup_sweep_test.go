package e2e

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The object sweep pages through the table by id. A listed object the delete
// does not remove -- it gained a referrer since the listing, or the statement
// failed -- would head a page read from the start of the table every time, and
// the sweep would spend its whole per-tick budget on it without reaching the
// backlog behind. The cursor is what makes each page start after the last row
// seen, whatever happened to that row.
func TestObjectSweepReachesCollectableRowsBehindAFullBatchItCannotDelete(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_MINIO=1 with MinIO running to enable storage tests")
	}

	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID
	batch := uuid.NewString()
	old := time.Now().Add(-30 * 24 * time.Hour)

	// More than one listing page of stuck rows, then one the sweep can take.
	const stuck = 520
	insertObject := func(i int) {
		digest := sha256.Sum256([]byte(batch + "/" + strconv.Itoa(i)))
		pub := uuid.New()
		_, err := testDB.Exec(
			`INSERT INTO storage_objects (public_id, workspace_id, sha256, byte_size, content_type, storage_key, ref_count, created_at)
			 VALUES (?, ?, ?, 1, 'application/pdf', ?, 0, ?)`,
			pub[:], workspaceID, digest[:], "sweep-cursor/"+batch+"/"+strconv.Itoa(i), old)
		require.NoError(t, err)
	}
	for i := range stuck {
		insertObject(i)
	}
	insertObject(stuck)
	t.Cleanup(func() {
		_, _ = testDB.Exec(`DELETE FROM storage_objects WHERE storage_key LIKE ?`, "sweep-cursor/"+batch+"/%")
	})

	stuckIDs := make(map[uint32]int, stuck)
	rows, err := testDB.Query(
		`SELECT id FROM storage_objects WHERE storage_key LIKE ? ORDER BY id`, "sweep-cursor/"+batch+"/%")
	require.NoError(t, err)
	var ids []uint32
	for rows.Next() {
		var id uint32
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Close())
	require.Len(t, ids, stuck+1)
	collectable := ids[stuck]
	for n, id := range ids[:stuck] {
		stuckIDs[id] = n
	}

	// Refuse the delete of every stuck row, alternating between reporting no
	// affected rows and failing outright: the sweep must advance past both.
	db := &execInterceptDB{DB: testDB}
	db.exec = func(_ context.Context, query string, args []any) (sql.Result, error, bool) {
		if !strings.Contains(query, "DELETE so FROM storage_objects so") || len(args) != 1 {
			return nil, nil, false
		}
		id, ok := args[0].(uint32)
		if !ok {
			return nil, nil, false
		}
		n, isStuck := stuckIDs[id]
		if !isStuck {
			return nil, nil, false
		}
		if n%2 == 0 {
			return affectedNothing{}, nil, true
		}
		return nil, errors.New("delete refused"), true
	}

	cleanup.RunOnce(testCtx(), db, getTestStorage())

	var left int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM storage_objects WHERE id = ?`, collectable).Scan(&left))
	require.Zero(t, left,
		"the sweep stayed on the rows it could not delete and never reached the collectable one behind them")
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM storage_objects WHERE storage_key LIKE ?`, "sweep-cursor/"+batch+"/%").Scan(&left))
	require.Equal(t, stuck, left, "the rows the delete refused are still there")
}

// A blob is only collectable once nothing names it. An attachment row retired
// with its event still names one, so a sweep must leave it — and must not fail
// trying.
func TestSweepLeavesAnObjectARetiredAttachmentStillNames(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_MINIO=1 with MinIO running to enable storage tests")
	}

	body := uniqueAttachmentBody("named by a retired row")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	tt := helpers.NewTenant(t, testServerURL)
	eventID := attachEvent(t, tt, "Names an object", body)

	status, raw := helpers.DoJSONStatus(t, "DELETE",
		testServerURL+"/calendars/"+tt.CalendarID+"/events/"+eventID, tt.AccessToken, nil)
	require.Equal(t, 204, status, "delete event: %s", string(raw))
	require.Equal(t, 0, objectRefCount(t, storageKey))

	// Age the object past the sweep's grace period.
	_, err := testDB.Exec(
		"UPDATE storage_objects SET created_at = ? WHERE storage_key = ?",
		time.Now().Add(-30*24*time.Hour), storageKey)
	require.NoError(t, err)

	cleanup.RunOnce(testCtx(), testDB, getTestStorage())

	var rows int
	require.NoError(t, testDB.QueryRow(
		"SELECT COUNT(*) FROM storage_objects WHERE storage_key = ?", storageKey,
	).Scan(&rows))
	require.Equal(t, 1, rows, "the row an attachment still names must survive the sweep")

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.True(t, exists, "and so must its bytes")
}

// Once the retention window has passed, the retired attachment row goes and the
// object it was pinning becomes collectable. Without that second stage the
// bytes are held forever by a row nothing reads.
func TestSweepReclaimsAnObjectOnceItsRetiredAttachmentAges(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_MINIO=1 with MinIO running to enable storage tests")
	}

	body := uniqueAttachmentBody("reclaimed after retention")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	tt := helpers.NewTenant(t, testServerURL)
	eventID := attachEvent(t, tt, "Names an object", body)
	status, _ := helpers.DoJSONStatus(t, "DELETE",
		testServerURL+"/calendars/"+tt.CalendarID+"/events/"+eventID, tt.AccessToken, nil)
	require.Equal(t, 204, status)

	old := time.Now().Add(-400 * 24 * time.Hour)
	_, err := testDB.Exec(
		"UPDATE storage_objects SET created_at = ? WHERE storage_key = ?", old, storageKey)
	require.NoError(t, err)
	_, err = testDB.Exec(`
		UPDATE calendar_event_attachments a
		INNER JOIN storage_objects so ON so.id = a.storage_object_id
		SET a.updated_at = ?
		WHERE so.storage_key = ?`, old, storageKey)
	require.NoError(t, err)

	cleanup.RunOnce(testCtx(), testDB, getTestStorage())

	var rows int
	require.NoError(t, testDB.QueryRow(
		"SELECT COUNT(*) FROM storage_objects WHERE storage_key = ?", storageKey,
	).Scan(&rows))
	require.Zero(t, rows, "nothing names the object any more, so it must be collected")

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.False(t, exists, "and its bytes must be reclaimed")
}
