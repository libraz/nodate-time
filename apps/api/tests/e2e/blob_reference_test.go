package e2e

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A storage reference is taken by the transaction that enabled a row and given
// back by the one that retired it -- once, however many requests race for it,
// and never for a row whose parent is already gone.

// concurrently fires n identical requests at once and returns their statuses.
func concurrently(t *testing.T, n int, method, url, token string) []int {
	t.Helper()
	statuses := make([]int, n)
	var start, done sync.WaitGroup
	start.Add(1)
	for i := range n {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			statuses[i], _ = helpers.DoJSONStatus(t, method, url, token, nil)
		}()
	}
	start.Done()
	done.Wait()
	return statuses
}

// The same file is attached in four calendars, so its object is shared at four
// references. Racing deletes of one attachment, of another's whole event, and
// of a third's whole calendar must each give back exactly one.
func TestConcurrentDeletesReleaseEachAttachmentOnce(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	body := uniqueAttachmentBody("raced deletes")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))
	a := helpers.NewTenant(t, testServerURL)
	b := helpers.NewTenant(t, testServerURL)
	c := helpers.NewTenant(t, testServerURL)
	aEvent, aAttachment := attachEventWithAttachment(t, a, "A", body)
	bEvent := attachEvent(t, b, "B", body)
	attachEvent(t, c, "C", body)
	d := *c
	d.CalendarID = newCalendar(t, c, "Going away")
	attachEvent(t, &d, "D", body)
	require.Equal(t, 4, objectRefCount(t, storageKey))

	statuses := concurrently(t, 8, http.MethodDelete,
		testServerURL+"/calendars/"+a.CalendarID+"/events/"+aEvent+"/attachments/"+aAttachment, a.AccessToken)
	require.Contains(t, statuses, http.StatusNoContent)
	require.Equal(t, 3, objectRefCount(t, storageKey), "one attachment deleted eight times is one release: %v", statuses)

	statuses = concurrently(t, 8, http.MethodDelete,
		testServerURL+"/calendars/"+b.CalendarID+"/events/"+bEvent, b.AccessToken)
	require.Contains(t, statuses, http.StatusNoContent)
	require.Equal(t, 2, objectRefCount(t, storageKey), "one event deleted eight times is one release: %v", statuses)

	statuses = concurrently(t, 8, http.MethodDelete, testServerURL+"/calendars/"+d.CalendarID, d.AccessToken)
	require.Contains(t, statuses, http.StatusNoContent)
	require.Equal(t, 1, objectRefCount(t, storageKey), "one calendar deleted eight times is one release: %v", statuses)
}

// A deleted attachment is disabled the way an unconfirmed reservation is, but
// it is not one: confirming it again must not bring it back.
func TestConfirmAfterDeleteDoesNotRestoreTheAttachment(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	body := uniqueAttachmentBody("deleted then confirmed")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))
	eventID, attachmentID := attachEventWithAttachment(t, tt, "Moderated", body)
	attachmentURL := testServerURL + "/calendars/" + tt.CalendarID + "/events/" + eventID + "/attachments/" + attachmentID

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, attachmentURL, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	status, _ = helpers.DoJSONStatus(t, http.MethodPost, attachmentURL+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)

	var attachments []map[string]any
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+tt.CalendarID+"/events/"+eventID+"/attachments",
		tt.AccessToken, nil, &attachments)
	require.Empty(t, attachments)
	require.Equal(t, 0, objectRefCount(t, storageKey))
}

// A deleted attachment is kept for the activity history for its own retention,
// counted from when it was deleted. The sweep for uploads that never landed
// goes by upload age and must leave it alone.
func TestARetiredAttachmentOutlivesTheAbandonedUploadSweep(t *testing.T) {
	bootstrap(t)
	requireStorage(t)

	tt := helpers.NewTenant(t, testServerURL)
	eventID, attachmentID := attachEventWithAttachment(t, tt, "Old file", uniqueAttachmentBody("retired"))
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/calendars/"+tt.CalendarID+"/events/"+eventID+"/attachments/"+attachmentID, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	_, err := testDB.Exec(
		"UPDATE calendar_event_attachments SET created_at = ?, updated_at = updated_at WHERE public_id = UUID_TO_BIN(?)",
		time.Now().Add(-10*24*time.Hour), attachmentID)
	require.NoError(t, err)

	cleanup.RunOnce(testCtx(), generated.New(testDB), getTestStorage())

	var rows int
	require.NoError(t, testDB.QueryRow(
		"SELECT COUNT(*) FROM calendar_event_attachments WHERE public_id = UUID_TO_BIN(?)", attachmentID,
	).Scan(&rows))
	require.Equal(t, 1, rows, "a row deleted today is kept for its retention, whatever its upload date")
}

// A reservation confirmed after its event was deleted must not come alive
// under it: nothing would ever release the reference it took.
func TestConfirmingAfterTheEventIsDeletedTakesNoReference(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	body := uniqueAttachmentBody("orphaned reservation")
	calURL, eventID, pres := reserveAttachment(t, tt, body)
	helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", body)

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+eventID, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	// The handler resolves the event first and would already refuse; the
	// statement must refuse on its own too, for a delete that lands between.
	var attachmentRowID, userID uint32
	require.NoError(t, testDB.QueryRow(
		"SELECT id, uploader_id FROM calendar_event_attachments WHERE public_id = UUID_TO_BIN(?)", pres.AttachmentID,
	).Scan(&attachmentRowID, &userID))
	res, err := generated.New(testDB).ConfirmEventAttachment(testCtx(), generated.ConfirmEventAttachmentParams{
		ID: attachmentRowID, UploaderID: userID,
	})
	require.NoError(t, err)
	affected, err := res.RowsAffected()
	require.NoError(t, err)
	require.Zero(t, affected, "a reservation under a deleted event is never enabled")

	status, _ = helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+eventID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))
	require.Equal(t, 0, objectRefCount(t, storageKey))
}

// The same for an album photo whose calendar went while it was uploading.
func TestConfirmingAPhotoAfterItsCalendarIsDeletedFails(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calendarID := newCalendar(t, tt, "Going away")
	body := distinctPNG("calendar-gone-" + time.Now().Format(time.RFC3339Nano))
	var pres albumPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+calendarID+"/albums/presign",
		tt.AccessToken, map[string]any{"caption": "late", "contentType": "image/png", "byteSize": len(body)}, &pres)
	helpers.UploadToSignedURL(t, pres.UploadURL, "image/png", body)

	var photoRowID, userID uint32
	require.NoError(t, testDB.QueryRow(
		"SELECT id, uploaded_by_user_id FROM album_photos WHERE public_id = UUID_TO_BIN(?)", pres.PhotoID,
	).Scan(&photoRowID, &userID))

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, testServerURL+"/calendars/"+calendarID, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	res, err := generated.New(testDB).ConfirmAlbumPhoto(testCtx(), generated.ConfirmAlbumPhotoParams{
		ID: photoRowID, UploadedByUserID: userID,
	})
	require.NoError(t, err)
	affected, err := res.RowsAffected()
	require.NoError(t, err)
	require.Zero(t, affected, "a photo in a deleted calendar is never enabled")
}

// The album has the same two kinds of disabled row, and the same rule: a
// deleted photo is not a pending upload, and its uploader cannot confirm it
// back into the album.
func TestConfirmAfterDeleteDoesNotRestoreThePhoto(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	photoID := uploadPhotoBytes(t, tt, tt.CalendarID, distinctPNG("deleted-photo-"+time.Now().Format(time.RFC3339Nano)), "moderated")
	photoURL := testServerURL + "/calendars/" + tt.CalendarID + "/albums/" + photoID

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, photoURL, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	status, _ = helpers.DoJSONStatus(t, http.MethodPost, photoURL+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)

	var list albumListResp
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+tt.CalendarID+"/albums", tt.AccessToken, nil, &list)
	for _, p := range list.Items {
		require.NotEqual(t, photoID, p.ID, "the deleted photo must stay out of the album")
	}
}
