package e2e

import (
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A blob is only collectable once nothing names it. An attachment row retired
// with its event still names one and still holds its reference, so a sweep
// must leave it -- even when the count has drifted to zero, which is what the
// view's referrer check is for.
func TestSweepLeavesAnObjectARetiredAttachmentStillNames(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_STORAGE=1 with object storage running to enable storage tests")
	}

	body := uniqueAttachmentBody("named by a retired row")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	tt := helpers.NewTenant(t, testServerURL)
	eventID := attachEvent(t, tt, "Names an object", body)

	status, raw := helpers.DoJSONStatus(t, "DELETE",
		testServerURL+"/calendars/"+tt.CalendarID+"/events/"+eventID, tt.AccessToken, nil)
	require.Equal(t, 204, status, "delete event: %s", string(raw))
	requireRefCountIsReferrers(t, storageKey, 1, "the retired row keeps its reference")

	cleanup.RunOnce(testCtx(), testDB, getTestStorage())
	requireRefCountIsReferrers(t, storageKey, 1, "the sweep leaves a held object alone")

	_, err := testDB.Exec("UPDATE storage_objects SET ref_count = 0 WHERE storage_key = ?", storageKey)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = testDB.Exec("UPDATE storage_objects SET ref_count = 1 WHERE storage_key = ?", storageKey)
	})
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
		t.Skip("set TC_TEST_STORAGE=1 with object storage running to enable storage tests")
	}

	body := uniqueAttachmentBody("reclaimed after retention")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	tt := helpers.NewTenant(t, testServerURL)
	eventID := attachEvent(t, tt, "Names an object", body)
	status, _ := helpers.DoJSONStatus(t, "DELETE",
		testServerURL+"/calendars/"+tt.CalendarID+"/events/"+eventID, tt.AccessToken, nil)
	require.Equal(t, 204, status)

	old := time.Now().Add(-400 * 24 * time.Hour)
	_, err := testDB.Exec(`
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
	require.Zero(t, queuedRemovals(t, storageKey), "its queued removal is drained")

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.False(t, exists, "and its bytes must be reclaimed")
}
