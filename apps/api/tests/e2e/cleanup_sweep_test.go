package e2e

import (
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The object sweep pages through the table. An object an attachment row still
// points at cannot be deleted — the foreign key is RESTRICT — so a page read
// from the head of the table would begin with that same object every time and
// the sweep would spend its whole per-tick budget on it, never reaching the
// backlog behind it. The cursor is what makes each page start after the last
// row seen.
func TestUnreferencedObjectListingAdvancesPastRowsItCannotDelete(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	q := generated.New(testDB)
	cutoff := time.Now().Add(time.Hour)

	first, err := q.ListUnreferencedStorageObjects(testCtx(), generated.ListUnreferencedStorageObjectsParams{
		CreatedAt: cutoff,
		ID:        0,
		Limit:     1,
	})
	require.NoError(t, err)
	if len(first) == 0 {
		t.Skip("no unreferenced objects to page through")
	}

	next, err := q.ListUnreferencedStorageObjects(testCtx(), generated.ListUnreferencedStorageObjectsParams{
		CreatedAt: cutoff,
		ID:        first[0].ID,
		Limit:     1,
	})
	require.NoError(t, err)
	for _, obj := range next {
		require.Greater(t, obj.ID, first[0].ID,
			"a page must start after the last row seen, or a blocked row repeats forever")
	}
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

	cleanup.RunOnce(testCtx(), generated.New(testDB), getTestStorage())

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

	cleanup.RunOnce(testCtx(), generated.New(testDB), getTestStorage())

	var rows int
	require.NoError(t, testDB.QueryRow(
		"SELECT COUNT(*) FROM storage_objects WHERE storage_key = ?", storageKey,
	).Scan(&rows))
	require.Zero(t, rows, "nothing names the object any more, so it must be collected")

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.False(t, exists, "and its bytes must be reclaimed")
}
