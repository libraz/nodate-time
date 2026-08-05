package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// attachEventWithAttachment creates an event on the tenant's calendar with one
// confirmed attachment carrying the given bytes.
func attachEventWithAttachment(
	t *testing.T, tt *helpers.TestTenant, title string, body []byte,
) (eventID, attachmentID string) {
	t.Helper()
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   title,
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		}, &evt)

	var pres struct {
		AttachmentID string `json:"attachmentId"`
		UploadURL    string `json:"uploadUrl"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/attachments/presign", tt.AccessToken,
		map[string]any{
			"filename":    "contract.pdf",
			"contentType": "application/pdf",
			"byteSize":    len(body),
			"sha256":      helpers.SHA256Hex(body),
		}, &pres)
	// Bytes something already stands behind come back with no upload URL: the
	// object is there and re-uploading would only be a chance to replace it.
	if pres.UploadURL != "" {
		helpers.UploadToPresignedURL(t, pres.UploadURL, "application/pdf", body)
	}
	helpers.DoJSON(t, http.MethodPost,
		calURL+"/events/"+evt.ID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil, nil)

	return evt.ID, pres.AttachmentID
}

// attachEvent is attachEventWithAttachment for callers that only need the event.
func attachEvent(t *testing.T, tt *helpers.TestTenant, title string, body []byte) string {
	t.Helper()
	id, _ := attachEventWithAttachment(t, tt, title, body)
	return id
}

// uniqueAttachmentBody returns bytes no other test or earlier run has used.
// Objects are content-addressed and shared across the whole deployment, so a
// fixed body would accumulate references run after run and make any absolute
// count assertion depend on how often the suite has been run.
func uniqueAttachmentBody(note string) []byte {
	return fmt.Appendf(nil, "%%PDF-1.4 %s %d", note, time.Now().UnixNano())
}

func objectRefCount(t *testing.T, storageKey string) int {
	t.Helper()
	var refs int
	require.NoError(t, testDB.QueryRow(
		"SELECT ref_count FROM storage_objects WHERE storage_key = ?", storageKey,
	).Scan(&refs))
	return refs
}

// Blobs are content-addressed and shared across the whole deployment, so the
// same file attached in two calendars is one object with two references.
// Releasing more than was taken drives that object to zero and hands a blob a
// live attachment still needs to the sweep.
func TestDeletingOneEventLeavesAnotherCalendarsCopyReferenced(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_MINIO=1 with MinIO running to enable storage tests")
	}
	t.Parallel()

	body := uniqueAttachmentBody("shared across two calendars")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	keeper := helpers.NewTenant(t, testServerURL)
	deleter := helpers.NewTenant(t, testServerURL)

	attachEvent(t, keeper, "Keeps the file", body)
	victimEvent := attachEvent(t, deleter, "Loses the file", body)

	require.Equal(t, 2, objectRefCount(t, storageKey))

	deleterCal := testServerURL + "/calendars/" + deleter.CalendarID
	status, raw := helpers.DoJSONStatus(t, http.MethodDelete,
		deleterCal+"/events/"+victimEvent, deleter.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "delete event: %s", string(raw))

	require.Equal(t, 1, objectRefCount(t, storageKey),
		"only the deleted event's own reference may be released")

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.True(t, exists, "a blob another calendar still references must survive")
}

// Deleting the attachment and then the event that held it releases one
// reference, not two: the row released its own when it was removed.
func TestDeletingAnAttachmentThenItsEventReleasesOneReference(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_MINIO=1 with MinIO running to enable storage tests")
	}
	t.Parallel()

	body := uniqueAttachmentBody("released exactly once")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	keeper := helpers.NewTenant(t, testServerURL)
	deleter := helpers.NewTenant(t, testServerURL)
	attachEvent(t, keeper, "Keeps the file", body)
	victimEvent, victimAttachment := attachEventWithAttachment(t, deleter, "Loses the file", body)
	require.Equal(t, 2, objectRefCount(t, storageKey))

	deleterCal := testServerURL + "/calendars/" + deleter.CalendarID
	status, raw := helpers.DoJSONStatus(t, http.MethodDelete,
		deleterCal+"/events/"+victimEvent+"/attachments/"+victimAttachment, deleter.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "delete attachment: %s", string(raw))
	require.Equal(t, 1, objectRefCount(t, storageKey))

	status, raw = helpers.DoJSONStatus(t, http.MethodDelete,
		deleterCal+"/events/"+victimEvent, deleter.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "delete event: %s", string(raw))

	require.Equal(t, 1, objectRefCount(t, storageKey),
		"an attachment already removed must not release its reference a second time")
}

// A reservation whose upload never landed never took a reference, so deleting
// the event it was reserved on must not release one.
func TestDeletingAnEventWithAnUnconfirmedReservationTakesNoReference(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_MINIO=1 with MinIO running to enable storage tests")
	}
	t.Parallel()

	body := uniqueAttachmentBody("never uploaded")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	keeper := helpers.NewTenant(t, testServerURL)
	reserver := helpers.NewTenant(t, testServerURL)
	attachEvent(t, keeper, "Keeps the file", body)
	require.Equal(t, 1, objectRefCount(t, storageKey))

	reserverCal := testServerURL + "/calendars/" + reserver.CalendarID
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, reserverCal+"/events", reserver.AccessToken,
		map[string]any{
			"title": "Reserved but never uploaded", "allDay": false,
			"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
		}, &evt)
	helpers.DoJSON(t, http.MethodPost, reserverCal+"/events/"+evt.ID+"/attachments/presign",
		reserver.AccessToken, map[string]any{
			"filename": "contract.pdf", "contentType": "application/pdf",
			"byteSize": len(body), "sha256": helpers.SHA256Hex(body),
		}, nil)

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete,
		reserverCal+"/events/"+evt.ID, reserver.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "delete event: %s", string(raw))

	require.Equal(t, 1, objectRefCount(t, storageKey),
		"an unconfirmed reservation never incremented, so it must not decrement")
}

// Deleting a calendar releases what its own attachments hold, and nothing more.
func TestDeletingACalendarLeavesSharedBlobsAlone(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_MINIO=1 with MinIO running to enable storage tests")
	}
	t.Parallel()

	body := uniqueAttachmentBody("shared with a calendar that goes away")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	keeper := helpers.NewTenant(t, testServerURL)
	deleter := helpers.NewTenant(t, testServerURL)
	attachEvent(t, keeper, "Keeps the file", body)
	attachEvent(t, deleter, "Goes away with the calendar", body)
	require.Equal(t, 2, objectRefCount(t, storageKey))

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/calendars/"+deleter.CalendarID, deleter.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "delete calendar: %s", string(raw))

	require.Equal(t, 1, objectRefCount(t, storageKey))

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.True(t, exists)
}
