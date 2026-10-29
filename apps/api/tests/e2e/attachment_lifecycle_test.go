package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
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
		helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", body)
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
// Deleting an event retires its attachment, and a retired row keeps its
// reference until the retention sweep removes it; only then is it given back,
// and only its own.
func TestDeletingOneEventLeavesAnotherCalendarsCopyReferenced(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_STORAGE=1 with object storage running to enable storage tests")
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

	requireRefCountIsReferrers(t, storageKey, 2, "the retired attachment still points at the object")

	sweepRetiredAttachments(t, storageKey)
	requireRefCountIsReferrers(t, storageKey, 1, "only the deleted event's own reference may be released")

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.True(t, exists, "a blob another calendar still references must survive")
}

// Deleting the attachment and then the event that held it releases one
// reference, not two, and only when the retired row is removed.
func TestDeletingAnAttachmentThenItsEventReleasesOneReference(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_STORAGE=1 with object storage running to enable storage tests")
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
	requireRefCountIsReferrers(t, storageKey, 2, "a soft delete leaves the count alone")

	status, raw = helpers.DoJSONStatus(t, http.MethodDelete,
		deleterCal+"/events/"+victimEvent, deleter.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "delete event: %s", string(raw))
	requireRefCountIsReferrers(t, storageKey, 2, "deleting the event retires nothing a second time")

	sweepRetiredAttachments(t, storageKey)
	requireRefCountIsReferrers(t, storageKey, 1,
		"an attachment removed once releases its reference once")
}

// A reservation holds a reference from the moment it is created. Deleting the
// event it was reserved on leaves it, and the abandoned-upload sweep that
// removes it gives the reference back.
func TestDeletingAnEventWithAnUnconfirmedReservationReleasesOnlyItsOwn(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_STORAGE=1 with object storage running to enable storage tests")
	}
	t.Parallel()

	body := uniqueAttachmentBody("never uploaded")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	keeper := helpers.NewTenant(t, testServerURL)
	reserver := helpers.NewTenant(t, testServerURL)
	attachEvent(t, keeper, "Keeps the file", body)
	requireRefCountIsReferrers(t, storageKey, 1, "the keeper's attachment")

	reserverCal := testServerURL + "/calendars/" + reserver.CalendarID
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, reserverCal+"/events", reserver.AccessToken,
		map[string]any{
			"title": "Reserved but never uploaded", "allDay": false,
			"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
		}, &evt)
	var pres presignResp
	helpers.DoJSON(t, http.MethodPost, reserverCal+"/events/"+evt.ID+"/attachments/presign",
		reserver.AccessToken, map[string]any{
			"filename": "contract.pdf", "contentType": "application/pdf",
			"byteSize": len(body), "sha256": helpers.SHA256Hex(body),
		}, &pres)
	requireRefCountIsReferrers(t, storageKey, 2, "the reservation counts from the moment it exists")

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete,
		reserverCal+"/events/"+evt.ID, reserver.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "delete event: %s", string(raw))
	requireRefCountIsReferrers(t, storageKey, 2, "deleting the event leaves the reservation in place")

	_, err := testDB.Exec(
		"UPDATE calendar_event_attachments SET created_at = ? WHERE public_id = UUID_TO_BIN(?)",
		time.Now().Add(-8*24*time.Hour), pres.AttachmentID)
	require.NoError(t, err)
	cleanup.RunOnce(testCtx(), testDB, getTestStorage())
	requireRefCountIsReferrers(t, storageKey, 1, "the swept reservation gives back its own reference only")
}

// Deleting a calendar retires its attachments; removing them releases what they
// hold, and nothing more.
func TestDeletingACalendarLeavesSharedBlobsAlone(t *testing.T) {
	bootstrap(t)
	if !helpers.StorageEnabled() {
		t.Skip("set TC_TEST_STORAGE=1 with object storage running to enable storage tests")
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
	requireRefCountIsReferrers(t, storageKey, 2, "the retired attachment still points at the object")

	sweepRetiredAttachments(t, storageKey)
	requireRefCountIsReferrers(t, storageKey, 1, "only the deleted calendar's attachment is released")

	_, exists, err := getTestStorage().StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.True(t, exists)
}
