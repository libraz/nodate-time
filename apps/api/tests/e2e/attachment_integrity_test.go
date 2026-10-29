package e2e

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type presignResp struct {
	AttachmentID string `json:"attachmentId"`
	UploadURL    string `json:"uploadUrl"`
}

// reserveAttachment creates an event and asks for an upload slot for `body`.
func reserveAttachment(
	t *testing.T, tt *helpers.TestTenant, declared []byte,
) (calURL, eventID string, pres presignResp) {
	t.Helper()
	calURL = testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "Has a file", "allDay": false,
			"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
		}, &evt)

	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/attachments/presign", tt.AccessToken,
		map[string]any{
			"filename": "contract.pdf", "contentType": "application/pdf",
			"byteSize": len(declared), "sha256": helpers.SHA256Hex(declared),
		}, &pres)
	return calURL, evt.ID, pres
}

// Blobs are content-addressed and shared across the whole workspace, so the
// storage key of a file is derived from bytes anyone holding a copy can
// compute. A write URL for that key would let a holder of the digest replace
// what everyone else's attachment resolves to, so a caller who cannot already
// read the stored file uploads to a key of their reservation's own -- and what
// they upload there changes nothing anyone else reads.
func TestPresignNeverHandsOutAWriteURLForBytesInUse(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	body := uniqueAttachmentBody("already in use")

	owner := helpers.NewTenant(t, testServerURL)
	ownerEvent, ownerAttachment := attachEventWithAttachment(t, owner, "Owns the file", body)

	other := helpers.NewTenant(t, testServerURL)
	_, _, pres := reserveAttachment(t, other, body)
	require.NotEmpty(t, pres.AttachmentID, "the reservation itself is still made")
	require.NotEmpty(t, pres.UploadURL, "a caller who cannot read the stored file has to send it")

	helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", bytes.Repeat([]byte("x"), len(body)))
	require.Equal(t, body, attachmentDownloadBytes(t, owner, ownerEvent, ownerAttachment),
		"the owner's attachment still resolves to the bytes they stored")
}

// A digest names a file; it does not prove having it. A member of another
// calendar who knows the digest of a file stored here cannot attach it
// without sending the bytes -- and once they do, the attachment shares the
// stored object rather than storing the file twice.
func TestADigestAloneDoesNotAttachAnotherCalendarsFile(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	body := uniqueAttachmentBody("somebody else's file")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	owner := helpers.NewTenant(t, testServerURL)
	attachEvent(t, owner, "Owns the file", body)
	require.Equal(t, 1, objectRefCount(t, storageKey))

	other := helpers.NewTenant(t, testServerURL)
	calURL, eventID, pres := reserveAttachment(t, other, body)
	confirmURL := calURL + "/events/" + eventID + "/attachments/" + pres.AttachmentID + "/confirm"

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, confirmURL, other.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status, "confirm without sending the bytes: %s", raw)
	var listed []map[string]any
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+eventID+"/attachments", other.AccessToken, nil, &listed)
	require.Empty(t, listed, "nothing of the other calendar's file is reachable")
	requireRefCountIsReferrers(t, storageKey, 2, "the owner's attachment and the other's reservation")

	helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", body)
	status, raw = helpers.DoJSONStatus(t, http.MethodPost, confirmURL, other.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "confirm after sending the bytes: %s", raw)
	requireRefCountIsReferrers(t, storageKey, 2, "both attachments stand behind the one object")
}

// Somebody who can already read the stored file -- here, because they attached
// it themselves -- attaches it again without uploading it a second time.
func TestAnAttachmentForBytesAlreadyStoredConfirmsWithoutUploading(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	body := uniqueAttachmentBody("confirms without uploading")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	owner := helpers.NewTenant(t, testServerURL)
	attachEvent(t, owner, "Owns the file", body)
	require.Equal(t, 1, objectRefCount(t, storageKey))

	calURL, eventID, pres := reserveAttachment(t, owner, body)
	require.Empty(t, pres.UploadURL)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+eventID+"/attachments/"+pres.AttachmentID+"/confirm", owner.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "confirm: %s", string(raw))
	require.Equal(t, 2, objectRefCount(t, storageKey),
		"both attachments now stand behind the one object")
}

// A digest is what decides which object bytes are stored as, so it cannot
// also be taken as proof of what they are. Without checking, an upload can
// park anything at a key it names and every later attachment of the real file
// resolves to it.
func TestConfirmRejectsBytesThatDoNotMatchTheDeclaredDigest(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	declared := uniqueAttachmentBody("what was promised")
	substituted := uniqueAttachmentBody("what was uploaded")
	require.Len(t, substituted, len(declared),
		"same length, so only the digest check can tell them apart")

	tt := helpers.NewTenant(t, testServerURL)
	calURL, eventID, pres := reserveAttachment(t, tt, declared)
	require.NotEmpty(t, pres.UploadURL, "nothing stands behind these bytes yet")

	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(declared))
	helpers.PutRawObject(t, getTestBucket(), storageKey, "application/pdf", substituted)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+eventID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusBadRequest, status, "confirm: %s", string(raw))

	var attachments []map[string]any
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+eventID+"/attachments", tt.AccessToken,
		nil, &attachments)
	require.Empty(t, attachments, "and the reservation must not survive as an attachment")
}
