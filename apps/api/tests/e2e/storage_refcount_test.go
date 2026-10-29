package e2e

import (
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// objectReferrers counts the rows pointing at the object stored at storageKey,
// through every foreign key that references storage_objects(id), whatever its
// table, layer or delete rule -- the count conformance 60 compares ref_count
// against.
func objectReferrers(t *testing.T, storageKey string) int {
	t.Helper()
	rows, err := testDB.Query(`
		SELECT TABLE_NAME, COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE
		 WHERE TABLE_SCHEMA = DATABASE()
		   AND REFERENCED_TABLE_NAME = 'storage_objects' AND REFERENCED_COLUMN_NAME = 'id'`)
	require.NoError(t, err)
	type column struct{ table, name string }
	var columns []column
	for rows.Next() {
		var c column
		require.NoError(t, rows.Scan(&c.table, &c.name))
		columns = append(columns, c)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.NotEmpty(t, columns, "the schema has foreign keys onto storage_objects")

	total := 0
	for _, c := range columns {
		var n int
		require.NoError(t, testDB.QueryRow(fmt.Sprintf(
			"SELECT COUNT(*) FROM `%s` r INNER JOIN storage_objects so ON so.id = r.`%s` WHERE so.storage_key = ?",
			c.table, c.name), storageKey).Scan(&n))
		total += n
	}
	return total
}

// requireRefCountIsReferrers asserts ref_count is want and is the number of
// rows pointing at the object (PROTOCOL.md section Storage).
func requireRefCountIsReferrers(t *testing.T, storageKey string, want int, msg string) {
	t.Helper()
	require.Equal(t, want, objectRefCount(t, storageKey), msg)
	require.Equal(t, want, objectReferrers(t, storageKey),
		"%s: ref_count is the number of rows pointing at the object", msg)
}

// sweepRetiredAttachments ages the retired attachments of the object past
// their retention and runs a cleanup pass, which removes them.
func sweepRetiredAttachments(t *testing.T, storageKey string) {
	t.Helper()
	_, err := testDB.Exec(`
		UPDATE calendar_event_attachments a
		 INNER JOIN storage_objects so ON so.id = a.storage_object_id
		   SET a.updated_at = ?
		 WHERE so.storage_key = ? AND a.retired_at IS NOT NULL`,
		time.Now().Add(-31*24*time.Hour), storageKey)
	require.NoError(t, err)
	cleanup.RunOnce(testCtx(), testDB, getTestStorage())
}

func queuedRemovals(t *testing.T, key string) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(
		"SELECT COUNT(*) FROM storage_blob_removals WHERE storage_key = ?", key).Scan(&n))
	return n
}

// A reservation counts from the moment it is created, keeps its count through
// confirm and soft delete, and gives it back only when the row is removed --
// after which the object and its blob go through the removal queue.
func TestStorageRefcountFollowsTheRowsThroughEveryState(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	body := uniqueAttachmentBody("every state")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))
	calURL, eventID, pres := reserveAttachment(t, tt, body)
	requireRefCountIsReferrers(t, storageKey, 1, "a reservation holds one reference")

	helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", body)
	attachmentURL := calURL + "/events/" + eventID + "/attachments/" + pres.AttachmentID
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, attachmentURL+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "confirm: %s", raw)
	requireRefCountIsReferrers(t, storageKey, 1, "confirming takes no second reference")

	status, _ = helpers.DoJSONStatus(t, http.MethodDelete, attachmentURL, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)
	requireRefCountIsReferrers(t, storageKey, 1, "a retired row keeps its reference")

	sweepRetiredAttachments(t, storageKey)
	var objects int
	require.NoError(t, testDB.QueryRow(
		"SELECT COUNT(*) FROM storage_objects WHERE storage_key = ?", storageKey).Scan(&objects))
	require.Zero(t, objects, "the object nothing points at is collected with the row's release")
	require.Zero(t, queuedRemovals(t, storageKey), "the queued removal is drained")
	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.False(t, exists, "and the blob is gone")
}

// A reservation whose upload never arrived is reclaimed by age alone, together
// with its reservations and the bytes they may have sent.
func TestStorageRefcountUnconfirmedSweepTakesTheReservationWithTheObject(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	body := uniqueAttachmentBody("never confirmed")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))
	_, _, pres := reserveAttachment(t, tt, body)
	helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", body)
	requireRefCountIsReferrers(t, storageKey, 1, "a reservation holds one reference")

	// Within the cutoff the reservation is left alone.
	cleanup.RunOnce(testCtx(), testDB, getTestStorage())
	requireRefCountIsReferrers(t, storageKey, 1, "an upload in progress is never taken")

	_, err := testDB.Exec("UPDATE storage_objects SET updated_at = ? WHERE storage_key = ?",
		time.Now().Add(-time.Hour), storageKey)
	require.NoError(t, err)
	cleanup.RunOnce(testCtx(), testDB, getTestStorage())

	var objects, reservations int
	require.NoError(t, testDB.QueryRow(
		"SELECT COUNT(*) FROM storage_objects WHERE storage_key = ?", storageKey).Scan(&objects))
	require.Zero(t, objects, "the abandoned reservation's object is reclaimed")
	require.NoError(t, testDB.QueryRow(
		"SELECT COUNT(*) FROM calendar_event_attachments WHERE public_id = UUID_TO_BIN(?)",
		pres.AttachmentID).Scan(&reservations))
	require.Zero(t, reservations, "and the reservation goes with it")
	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.False(t, exists, "the bytes it sent are removed through the queue")
}

// Confirming records the verified size and type but never moves the object to
// another key: another product may have handed out an upload URL for it.
func TestStorageRefcountConfirmKeepsTheStorageKey(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	body := uniqueAttachmentBody("key stays")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))
	calURL, eventID, pres := reserveAttachment(t, tt, body)
	var before string
	var uploaded sql.NullTime
	require.NoError(t, testDB.QueryRow(`
		SELECT so.storage_key, so.uploaded_at FROM calendar_event_attachments a
		  JOIN storage_objects so ON so.id = a.storage_object_id
		 WHERE a.public_id = UUID_TO_BIN(?)`, pres.AttachmentID).Scan(&before, &uploaded))
	require.Equal(t, storageKey, before)
	require.False(t, uploaded.Valid)

	helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", body)
	status, raw := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+eventID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "confirm: %s", raw)

	var after string
	require.NoError(t, testDB.QueryRow(`
		SELECT so.storage_key, so.uploaded_at FROM calendar_event_attachments a
		  JOIN storage_objects so ON so.id = a.storage_object_id
		 WHERE a.public_id = UUID_TO_BIN(?)`, pres.AttachmentID).Scan(&after, &uploaded))
	require.Equal(t, before, after)
	require.True(t, uploaded.Valid)
}

// The drainer runs only against the bucket the database records: the first
// one is recorded, the same one matches, and another is refused.
func TestStorageRefcountLocationCheckRefusesAnotherBucket(t *testing.T) {
	bootstrap(t)

	// Rolled back, so the recorded location of the shared database is never
	// changed by the test.
	tx, err := testDB.BeginTx(testCtx(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.Exec("DELETE FROM instance_settings WHERE setting_key = 'storage.location'")
	require.NoError(t, err)

	q := generated.New(tx)
	require.NoError(t, blobref.CheckLocation(testCtx(), q, "storage:7070/first"), "recorded when absent")
	require.NoError(t, blobref.CheckLocation(testCtx(), q, "storage:7070/first"), "the same bucket matches")
	require.ErrorIs(t, blobref.CheckLocation(testCtx(), q, "storage:7070/second"),
		blobref.ErrLocationMismatch)
}
