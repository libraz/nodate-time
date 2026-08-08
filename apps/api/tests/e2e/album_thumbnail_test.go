package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The album grid draws tiles from a second, smaller rendering when the photo
// has one. Everything here is about that "when": the thumbnail is optional at
// every step, so what these tests measure is that the picture is never the
// thing that suffers when it is missing.
//
// The bytes are distinct per upload on purpose. A thumbnail URL that is merely
// non-empty proves nothing -- it would be just as non-empty if it pointed at
// the photo -- so every assertion below fetches both URLs and compares what
// comes back.

// thumbnailUpload is what a test needs to check afterwards: the photo, and the
// two sets of bytes it was built from.
type thumbnailUpload struct {
	photoID   string
	photo     []byte
	thumbnail []byte
	confirmed albumPhotoResp
}

// uploadPhotoWithThumbnail runs the presign / two PUTs / confirm sequence.
func uploadPhotoWithThumbnail(t *testing.T, tt *helpers.TestTenant, calendarID, marker string) thumbnailUpload {
	t.Helper()
	photo := distinctPNG("photo-" + marker)
	thumbnail := distinctPNG("thumb-" + marker)

	var pres albumPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+calendarID+"/albums/presign",
		tt.AccessToken, map[string]any{
			"caption":              "with a thumbnail",
			"contentType":          "image/png",
			"byteSize":             len(photo),
			"thumbnailContentType": "image/png",
			"thumbnailByteSize":    len(thumbnail),
		}, &pres)
	require.NotEmpty(t, pres.ThumbnailUploadURL, "a declared thumbnail must be answered with a URL to put it at")
	require.NotEqual(t, pres.UploadURL, pres.ThumbnailUploadURL, "the two uploads must not land on one key")

	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", photo)
	helpers.UploadToPresignedURL(t, pres.ThumbnailUploadURL, "image/png", thumbnail)

	var confirmed albumPhotoResp
	helpers.DoJSON(t, http.MethodPost,
		testServerURL+"/calendars/"+calendarID+"/albums/"+pres.PhotoID+"/confirm",
		tt.AccessToken, nil, &confirmed)

	return thumbnailUpload{photoID: pres.PhotoID, photo: photo, thumbnail: thumbnail, confirmed: confirmed}
}

// albumPhotoByID reads one photo back out of the listing, which is the path
// that resolves keys in SQL rather than per row.
func albumPhotoByID(t *testing.T, tt *helpers.TestTenant, calendarID, photoID string) albumPhotoResp {
	t.Helper()
	var list albumListResp
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+calendarID+"/albums",
		tt.AccessToken, nil, &list)
	for _, p := range list.Items {
		if p.ID == photoID {
			return p
		}
	}
	t.Fatalf("photo %s is not in the album", photoID)
	return albumPhotoResp{}
}

// A photo that was uploaded with a thumbnail serves it, and it is the smaller
// rendering rather than the picture signed twice.
func TestAPhotoWithAThumbnailServesTheSmallerBytes(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	up := uploadPhotoWithThumbnail(t, tt, tt.CalendarID, fmt.Sprintf("serves-%d", time.Now().UnixNano()))

	require.NotEmpty(t, up.confirmed.ThumbnailURL, "the confirm answers with the thumbnail it just attached")
	require.Equal(t, up.photo, helpers.FetchURL(t, up.confirmed.ImageURL))
	require.Equal(t, up.thumbnail, helpers.FetchURL(t, up.confirmed.ThumbnailURL),
		"the thumbnail URL must serve the thumbnail, not the photo")

	// The listing resolves both keys in SQL, so it is a second implementation
	// of the same question and has to give the same answer.
	listed := albumPhotoByID(t, tt, tt.CalendarID, up.photoID)
	require.NotEmpty(t, listed.ThumbnailURL)
	require.Equal(t, up.photo, helpers.FetchURL(t, listed.ImageURL))
	require.Equal(t, up.thumbnail, helpers.FetchURL(t, listed.ThumbnailURL))
}

// A photo uploaded without declaring a thumbnail is offered no second URL and
// comes back with no thumbnail. The grid falls back to the picture, which is
// correct and merely larger.
func TestAPhotoWithoutAThumbnailHasNone(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := distinctPNG(fmt.Sprintf("no-thumb-%d", time.Now().UnixNano()))

	var pres albumPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+tt.CalendarID+"/albums/presign",
		tt.AccessToken, map[string]any{"contentType": "image/png", "byteSize": len(png)}, &pres)
	require.Empty(t, pres.ThumbnailUploadURL, "nothing was declared, so there is nothing to sign")

	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", png)
	var confirmed albumPhotoResp
	helpers.DoJSON(t, http.MethodPost,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/"+pres.PhotoID+"/confirm",
		tt.AccessToken, nil, &confirmed)

	require.Empty(t, confirmed.ThumbnailURL)
	require.Equal(t, png, helpers.FetchURL(t, confirmed.ImageURL))
	require.Empty(t, albumPhotoByID(t, tt, tt.CalendarID, pres.PhotoID).ThumbnailURL)
}

// The load-bearing one. A thumbnail URL is issued and never used -- the tab is
// closed between the two PUTs, the second request fails, the client decides
// against it. The photo's own bytes reached the server, and confirming them
// must not depend on a second upload that is optional by design.
func TestAThumbnailThatNeverArrivesDoesNotCostThePhoto(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := distinctPNG(fmt.Sprintf("abandoned-thumb-%d", time.Now().UnixNano()))

	var pres albumPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+tt.CalendarID+"/albums/presign",
		tt.AccessToken, map[string]any{
			"contentType": "image/png", "byteSize": len(png),
			"thumbnailContentType": "image/png", "thumbnailByteSize": 64,
		}, &pres)
	require.NotEmpty(t, pres.ThumbnailUploadURL)

	// Only the photo is uploaded.
	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", png)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/"+pres.PhotoID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusOK, status,
		"a photo whose bytes arrived must confirm even though the thumbnail did not: %s", string(raw))

	listed := albumPhotoByID(t, tt, tt.CalendarID, pres.PhotoID)
	require.Empty(t, listed.ThumbnailURL, "there is no thumbnail to point at")
	require.Equal(t, png, helpers.FetchURL(t, listed.ImageURL), "and the photo itself is intact")
}

// An object kept alive only as a thumbnail must not be offered to the object
// sweep.
//
// The listing assertion is the one that matters. Nothing downstream of it
// reports a failure: the foreign key is RESTRICT, so a sweep that tried to
// collect this object would be refused by the database and the bytes would
// survive anyway -- which is why the survival checks below pass with or
// without the exclusion, and why they are not evidence of it. What the missing
// clause actually costs is an object listed on every tick, refused on every
// tick, spending the batch budget the cursor exists to protect.
//
// The state is arranged rather than reached: ref_count is forced to zero to
// stand for a release that ran without the row going away, which is the drift
// the exclusion is there for. The age is forced too, because the sweep only
// looks at objects old enough to be past the reservation window.
func TestTheSweepKeepsAnObjectHeldOnlyAsAThumbnail(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	up := uploadPhotoWithThumbnail(t, tt, tt.CalendarID, fmt.Sprintf("sweep-%d", time.Now().UnixNano()))

	var objectID uint32
	var objectKey string
	require.NoError(t, testDB.QueryRow(
		`SELECT so.id, so.storage_key
		   FROM album_photos ap
		   JOIN storage_objects so ON so.id = ap.thumbnail_object_id
		  WHERE ap.public_id = UUID_TO_BIN(?)`, up.photoID).Scan(&objectID, &objectKey),
		"the confirmed photo holds a thumbnail object")

	_, err := testDB.Exec(
		`UPDATE storage_objects SET ref_count = 0, created_at = ? WHERE id = ?`,
		time.Now().Add(-30*24*time.Hour), objectID)
	require.NoError(t, err)

	offered, err := generated.New(testDB).ListUnreferencedStorageObjects(testCtx(),
		generated.ListUnreferencedStorageObjectsParams{
			CreatedAt: time.Now(),
			ID:        objectID - 1,
			Limit:     100,
		})
	require.NoError(t, err)
	for _, obj := range offered {
		require.NotEqual(t, objectID, obj.ID,
			"an object a live photo still points at as its thumbnail must not be offered to the sweep")
	}

	cleanup.RunOnce(testCtx(), generated.New(testDB), getTestStorage())

	var rows int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM storage_objects WHERE id = ?`, objectID).Scan(&rows))
	require.Equal(t, 1, rows, "the object row is still there")

	_, exists, err := getTestStorage().StatObject(testCtx(), objectKey)
	require.NoError(t, err)
	require.True(t, exists, "and so are the bytes the tile is drawn from")

	require.Equal(t, up.thumbnail,
		helpers.FetchURL(t, albumPhotoByID(t, tt, tt.CalendarID, up.photoID).ThumbnailURL),
		"which is what the tile still gets")
}

// The thumbnail is validated the way the photo is, and a declaration that is
// only half made is refused rather than silently answered with no URL: a
// client that believes it is sending one would otherwise find out by noticing
// the grid downloading full-size pictures.
func TestPresignRefusesAThumbnailThatIsNotOne(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	// The code is asserted, not just the status. All four are 400, so a status
	// alone cannot tell whether the caller was told about the thumbnail or
	// about the photo -- and being told about the photo's 20MB ceiling when a
	// 1.5MB thumbnail was refused sends them to look at the wrong file.
	cases := []struct {
		name string
		code string
		body map[string]any
	}{
		{"active content", "IMAGE.INVALID_CONTENT_TYPE", map[string]any{
			"thumbnailContentType": "image/svg+xml", "thumbnailByteSize": 128,
		}},
		{"a full-size picture in a thumbnail's clothing", "ALBUM.THUMBNAIL_TOO_LARGE", map[string]any{
			"thumbnailContentType": "image/jpeg", "thumbnailByteSize": 2 * 1024 * 1024,
		}},
		{"a type with no size", "ALBUM.THUMBNAIL_INCOMPLETE", map[string]any{
			"thumbnailContentType": "image/png",
		}},
		{"a size with no type", "ALBUM.THUMBNAIL_INCOMPLETE", map[string]any{
			"thumbnailByteSize": 4096,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"contentType": "image/png", "byteSize": 1024}
			for k, v := range tc.body {
				body[k] = v
			}
			status, raw := helpers.DoJSONStatus(t, http.MethodPost,
				testServerURL+"/calendars/"+tt.CalendarID+"/albums/presign", tt.AccessToken, body)
			require.Equal(t, http.StatusBadRequest, status, "body: %s", string(raw))
			var failure struct {
				Code string `json:"code"`
			}
			require.NoError(t, json.Unmarshal(raw, &failure))
			require.Equal(t, tc.code, failure.Code, "body: %s", string(raw))
		})
	}
}
