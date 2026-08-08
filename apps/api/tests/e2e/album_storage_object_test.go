package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// Album photos reach their bytes through storage_objects, the way attachments
// and avatars already did. What that buys is a reference count: bytes two
// photos share are not one photo's to delete.
//
// The pictures here carry their own bytes rather than the suite's shared
// fixture. Content addressing means identical bytes are one object, so a test
// using the fixture would be sharing a row with every other album test in the
// package -- and asserting about a lifecycle it does not control. Sharing is
// arranged deliberately below, where it is the thing being measured.

// distinctPNG returns a PNG nobody else in the suite is uploading. The trailer
// is ignored by every reader of the file and changes its digest, which is the
// only property these tests need from it.
func distinctPNG(marker string) []byte {
	return append(helpers.TinyPNG(), []byte("nodate-test-"+marker)...)
}

// uploadPhotoBytes runs the presign / PUT / confirm sequence with bytes of the
// caller's choosing and returns the photo's public id.
func uploadPhotoBytes(t *testing.T, tt *helpers.TestTenant, calendarID string, body []byte, caption string) string {
	t.Helper()
	var pres albumPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+calendarID+"/albums/presign",
		tt.AccessToken, map[string]any{
			"caption": caption, "contentType": "image/png", "byteSize": len(body),
		}, &pres)
	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", body)
	helpers.DoJSON(t, http.MethodPost,
		testServerURL+"/calendars/"+calendarID+"/albums/"+pres.PhotoID+"/confirm",
		tt.AccessToken, nil, nil)
	return pres.PhotoID
}

// photoObjectRef reads the reference a photo holds, which is only visible in
// the row.
func photoObjectRef(t *testing.T, photoID string) (objectID int64, refCount int, objectKey string) {
	t.Helper()
	err := testDB.QueryRow(
		`SELECT so.id, so.ref_count, so.storage_key
		   FROM album_photos ap
		   JOIN storage_objects so ON so.id = ap.storage_object_id
		  WHERE ap.public_id = UUID_TO_BIN(?)`, photoID).Scan(&objectID, &refCount, &objectKey)
	require.NoError(t, err, "photo %s holds no storage object", photoID)
	return objectID, refCount, objectKey
}

func photoImageURL(t *testing.T, tt *helpers.TestTenant, calendarID, photoID string) string {
	t.Helper()
	var list albumListResp
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+calendarID+"/albums",
		tt.AccessToken, nil, &list)
	for _, p := range list.Items {
		if p.ID == photoID {
			return p.ImageURL
		}
	}
	t.Fatalf("photo %s is not in the album", photoID)
	return ""
}

// A confirmed upload is on the object model: it points at a storage object and
// holds the only reference to it.
func TestConfirmingAPhotoPutsItOnTheObjectModel(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := distinctPNG(fmt.Sprintf("confirm-%d", time.Now().UnixNano()))
	photoID := uploadPhotoBytes(t, tt, tt.CalendarID, png, "on the model")

	_, refCount, _ := photoObjectRef(t, photoID)
	require.Equal(t, 1, refCount, "the photo is what holds the bytes")

	require.Equal(t, png, helpers.FetchURL(t, photoImageURL(t, tt, tt.CalendarID, photoID)),
		"and the picture is still served through it")
}

// The reference count earns its keep here. Two calendars holding the same
// picture share one object, and deleting one photo must leave the other one
// showing something.
//
// This is the case that makes deleting the bytes on the delete path wrong: it
// passes only because the photo's own delete stopped removing them.
func TestTwoCalendarsHoldingTheSamePictureShareItSafely(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	other := newCalendar(t, tt, "Second album")
	shared := distinctPNG(fmt.Sprintf("shared-%d", time.Now().UnixNano()))

	first := uploadPhotoBytes(t, tt, tt.CalendarID, shared, "hers")
	second := uploadPhotoBytes(t, tt, other, shared, "his")

	firstObject, _, _ := photoObjectRef(t, first)
	secondObject, refCount, _ := photoObjectRef(t, second)
	require.Equal(t, firstObject, secondObject, "identical bytes are one object")
	require.Equal(t, 2, refCount, "held by both photos")

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/"+first, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	require.Equal(t, shared, helpers.FetchURL(t, photoImageURL(t, tt, other, second)),
		"deleting one photo must not blank the other one's picture")
}

// Bytes come back in one pass, not two.
//
// The sweep that removes a retired photo's row is what releases its last
// reference, and it reclaims the object there rather than leaving it to the
// object sweep -- whose age gate is counted from when the object row was
// created, and exists to protect a reservation waiting to be confirmed. An
// object whose last referring row was just deleted is not one of those, and
// making it wait would put the album's retention and that gate end to end.
func TestADeletedPhotoGivesItsBytesBackInOneSweep(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := distinctPNG(fmt.Sprintf("one-sweep-%d", time.Now().UnixNano()))
	photoID := uploadPhotoBytes(t, tt, tt.CalendarID, png, "not for long")
	objectID, _, objectKey := photoObjectRef(t, photoID)

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/"+photoID, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	_, exists, err := getTestStorage().StatObject(testCtx(), objectKey)
	require.NoError(t, err)
	require.True(t, exists, "deleting a photo does not delete bytes that may be shared")

	// The row goes out of use now and ages past the retention window.
	_, err = testDB.Exec(
		`UPDATE album_photos SET updated_at = ? WHERE public_id = UUID_TO_BIN(?)`,
		time.Now().Add(-30*24*time.Hour), photoID)
	require.NoError(t, err)

	cleanup.RunOnce(context.Background(), generated.New(testDB), getTestStorage())

	var objectRows int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM storage_objects WHERE id = ?`, objectID).Scan(&objectRows))
	require.Zero(t, objectRows, "the object nothing refers to goes with the row that held it")

	_, exists, err = getTestStorage().StatObject(testCtx(), objectKey)
	require.NoError(t, err)
	require.False(t, exists, "and so do the bytes, in the same pass")
}

// A photo uploaded before the column existed is read through its own key and
// keeps working. That is what lets this migration stop half-way: the backfill
// failing, or never running, costs nothing that anybody sees.
func TestAPhotoWithNoStorageObjectIsStillServed(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := distinctPNG(fmt.Sprintf("legacy-%d", time.Now().UnixNano()))
	photoID := uploadPhotoBytes(t, tt, tt.CalendarID, png, "from before")

	detachPhoto(t, photoID)

	require.Equal(t, png, helpers.FetchURL(t, photoImageURL(t, tt, tt.CalendarID, photoID)),
		"a photo the object model has not reached is served from the key it was uploaded with")

	var download struct {
		DownloadURL string `json:"downloadUrl"`
	}
	helpers.DoJSON(t, http.MethodGet,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/"+photoID+"/download",
		tt.AccessToken, nil, &download)
	require.Equal(t, png, helpers.FetchURL(t, download.DownloadURL))
}

// detachPhoto puts a photo back the way one uploaded before this migration
// looks: pointing at no object, reachable only through its own key. The
// reference it held is given back, since the row no longer holds it.
func detachPhoto(t *testing.T, photoID string) {
	t.Helper()
	_, err := testDB.Exec(
		`UPDATE storage_objects so
		   JOIN album_photos ap ON ap.storage_object_id = so.id
		    SET so.ref_count = GREATEST(so.ref_count, 1) - 1
		  WHERE ap.public_id = UUID_TO_BIN(?)`, photoID)
	require.NoError(t, err)
	_, err = testDB.Exec(
		`UPDATE album_photos SET storage_object_id = NULL WHERE public_id = UUID_TO_BIN(?)`, photoID)
	require.NoError(t, err)
}

// The backfill reaches the photos that predate the column, and the picture
// does not change hands while it does: the same bytes are served before and
// after, from a key the object now names.
func TestTheBackfillMovesAnOlderPhotoOntoAnObject(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := distinctPNG(fmt.Sprintf("backfill-%d", time.Now().UnixNano()))
	photoID := uploadPhotoBytes(t, tt, tt.CalendarID, png, "waiting for the sweep")
	detachPhoto(t, photoID)

	var objectID int64
	require.Error(t, testDB.QueryRow(
		`SELECT so.id FROM album_photos ap JOIN storage_objects so ON so.id = ap.storage_object_id
		  WHERE ap.public_id = UUID_TO_BIN(?)`, photoID).Scan(&objectID),
		"the photo starts with no object, which is what the sweep is for")

	cleanup.RunOnce(context.Background(), generated.New(testDB), getTestStorage())

	_, refCount, objectKey := photoObjectRef(t, photoID)
	require.Equal(t, 1, refCount, "the backfill takes the reference the photo should have had")
	require.NotEmpty(t, objectKey)

	require.Equal(t, png, helpers.FetchURL(t, photoImageURL(t, tt, tt.CalendarID, photoID)),
		"and the same picture is served through the object")
}
