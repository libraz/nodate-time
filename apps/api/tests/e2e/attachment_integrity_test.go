package e2e

import (
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
// compute. Handing out a write URL for a key a confirmed attachment already
// stands behind therefore lets a holder of those bytes replace what everyone
// else's attachment resolves to.
func TestPresignRefusesToReissueAWriteURLForBytesInUse(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	body := uniqueAttachmentBody("already in use")

	owner := helpers.NewTenant(t, testServerURL)
	attachEvent(t, owner, "Owns the file", body)

	other := helpers.NewTenant(t, testServerURL)
	_, _, pres := reserveAttachment(t, other, body)
	require.NotEmpty(t, pres.AttachmentID, "the reservation itself is still made")
	require.Empty(t, pres.UploadURL,
		"a caller with the same digest must get the stored object, not a chance to replace it")
}

// The same caller must still be able to complete the attachment: the bytes
// are already there, so confirming is all that is left.
func TestAnAttachmentForBytesAlreadyStoredConfirmsWithoutUploading(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	body := uniqueAttachmentBody("confirms without uploading")
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))

	owner := helpers.NewTenant(t, testServerURL)
	attachEvent(t, owner, "Owns the file", body)
	require.Equal(t, 1, objectRefCount(t, storageKey))

	other := helpers.NewTenant(t, testServerURL)
	calURL, eventID, pres := reserveAttachment(t, other, body)
	require.Empty(t, pres.UploadURL)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+eventID+"/attachments/"+pres.AttachmentID+"/confirm", other.AccessToken, nil)
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
