package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// Attachments and album photos deduplicate onto one storage object per
// (workspace, digest). These tests cover the two kinds meeting on the same
// bytes, and what an object has to have been through before anything else may
// reuse it.

type storageObjectState struct {
	ID       int64
	Key      string
	RefCount int
	Verified bool
	ByteSize int
}

func storageObjectByDigest(t *testing.T, body []byte) storageObjectState {
	t.Helper()
	var o storageObjectState
	require.NoError(t, testDB.QueryRow(`
		SELECT id, storage_key, ref_count, uploaded_at IS NOT NULL, byte_size
		  FROM storage_objects
		 WHERE workspace_id IS NOT NULL AND sha256 = UNHEX(?)`, helpers.SHA256Hex(body),
	).Scan(&o.ID, &o.Key, &o.RefCount, &o.Verified, &o.ByteSize))
	return o
}

func attachmentDownloadBytes(t *testing.T, tt *helpers.TestTenant, eventID, attachmentID string) []byte {
	t.Helper()
	var dl struct {
		DownloadURL string `json:"downloadUrl"`
	}
	helpers.DoJSON(t, http.MethodGet,
		testServerURL+"/calendars/"+tt.CalendarID+"/events/"+eventID+"/attachments/"+attachmentID+"/download",
		tt.AccessToken, nil, &dl)
	return helpers.FetchURL(t, dl.DownloadURL)
}

// A picture already in the album is attached to an event: the attachment
// resolves to the photo's object, at the photo's key, instead of failing on
// the unique digest index.
func TestAttachingBytesAlreadyInTheAlbumReusesThePhotosObject(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	picture := distinctPNG(fmt.Sprintf("album-then-attach-%d", time.Now().UnixNano()))
	photoID := uploadPhotoBytes(t, tt, tt.CalendarID, picture, "in the album")
	_, _, photoKey := photoObjectRef(t, photoID)

	calURL, eventID, pres := reserveAttachment(t, tt, picture)
	require.NotEmpty(t, pres.AttachmentID)
	require.Empty(t, pres.UploadURL, "the bytes are already stored at the photo's key")

	status, raw := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+eventID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "confirm: %s", string(raw))

	object := storageObjectByDigest(t, picture)
	require.Equal(t, photoKey, object.Key)
	require.Equal(t, 2, object.RefCount, "the photo and the attachment both hold the object")
	require.Equal(t, picture, attachmentDownloadBytes(t, tt, eventID, pres.AttachmentID))
}

// An attachment reservation creates its object before any bytes exist. A photo
// of the same digest confirmed meanwhile must not point at that unverified
// row -- whose key here holds bytes that are not the photo at all -- but take
// it over onto the bytes it has itself verified.
func TestAPhotoDoesNotReuseAnUnverifiedAttachmentReservation(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	picture := distinctPNG(fmt.Sprintf("reserved-then-album-%d", time.Now().UnixNano()))
	impostor := make([]byte, len(picture))
	copy(impostor, picture)
	impostor[len(impostor)-1] ^= 0xFF

	calURL, eventID, pres := reserveAttachment(t, tt, picture)
	require.NotEmpty(t, pres.UploadURL)
	attachmentKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(picture))
	helpers.PutRawObject(t, getTestBucket(), attachmentKey, "application/pdf", impostor)
	require.False(t, storageObjectByDigest(t, picture).Verified, "the reservation vouches for nothing yet")

	photoID := uploadPhotoBytes(t, tt, tt.CalendarID, picture, "the real one")
	objectID, _, objectKey := photoObjectRef(t, photoID)
	object := storageObjectByDigest(t, picture)
	require.Equal(t, object.ID, objectID, "one object per digest")
	require.NotEqual(t, attachmentKey, objectKey, "the photo must not resolve to the unverified upload")
	require.True(t, object.Verified)
	require.Equal(t, picture, helpers.FetchURL(t, photoImageURL(t, tt, tt.CalendarID, photoID)))

	// The pending attachment now confirms against the verified bytes, and the
	// unclaimed copy at its own key is cleared.
	status, raw := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+eventID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "confirm: %s", string(raw))
	require.Equal(t, picture, attachmentDownloadBytes(t, tt, eventID, pres.AttachmentID))
	_, exists, err := getTestStorage().StatObject(testCtx(), attachmentKey)
	require.NoError(t, err)
	require.False(t, exists, "bytes no object claims are not left behind")
}

// An upload URL outlives the confirm of its own upload. Using it again must
// not replace bytes every attachment of that digest now resolves to.
func TestAnUploadURLCannotReplaceConfirmedBytes(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	body := uniqueAttachmentBody("confirmed then overwritten")
	calURL, eventID, pres := reserveAttachment(t, tt, body)
	require.NotEmpty(t, pres.UploadURL)
	helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", body)
	helpers.DoJSON(t, http.MethodPost,
		calURL+"/events/"+eventID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil, nil)

	replacement := make([]byte, len(body))
	copy(replacement, body)
	replacement[0] = 'X'
	status, _ := helpers.UploadToSignedURLStatus(t, pres.UploadURL, "application/pdf", replacement)
	require.Equal(t, http.StatusConflict, status, "a stored key is never written twice")
	require.Equal(t, body, attachmentDownloadBytes(t, tt, eventID, pres.AttachmentID))
}

// Bytes that fail the digest check are removed with the reservation. The relay
// never overwrites a key, so leaving them would refuse every honest upload of
// that file.
func TestMismatchedAttachmentBytesDoNotBlockTheRealFile(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	declared := uniqueAttachmentBody("the real file")
	substituted := append([]byte(nil), declared...)
	substituted[0] = 'X'

	calURL, eventID, pres := reserveAttachment(t, tt, declared)
	helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", substituted)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+eventID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusBadRequest, status)

	calURL, eventID, pres = reserveAttachment(t, tt, declared)
	require.NotEmpty(t, pres.UploadURL, "the bad bytes are gone, so there is somewhere to upload")
	helpers.UploadToSignedURL(t, pres.UploadURL, "application/pdf", declared)
	status, raw := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+eventID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "confirm: %s", string(raw))
	require.Equal(t, declared, attachmentDownloadBytes(t, tt, eventID, pres.AttachmentID))
}
