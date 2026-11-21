package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// digestPhoto is one photo's worth of bytes with a unique digest, so a test
// does not collide with the storage objects earlier runs left in the database.
func digestPhoto(marker string) []byte {
	return distinctPNG(fmt.Sprintf("%s-%d-%d", marker, photoRunID, uniquePhotoSuffix.Add(1)))
}

type digestPresign struct {
	status int
	raw    []byte
	albumPresignResp
}

func presignDigestPhoto(t *testing.T, tt *helpers.TestTenant, body map[string]any) digestPresign {
	t.Helper()
	status, raw := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/presign", tt.AccessToken, body)
	out := digestPresign{status: status, raw: raw}
	if status == http.StatusCreated {
		require.NoError(t, json.Unmarshal(raw, &out.albumPresignResp))
	}
	return out
}

type digestPhotoResp struct {
	ID           string `json:"id"`
	Caption      string `json:"caption"`
	EventID      string `json:"eventId"`
	ETag         string `json:"etag"`
	ImageURL     string `json:"imageUrl"`
	ThumbnailURL string `json:"thumbnailUrl"`
}

type digestAlbumList struct {
	Items      []digestPhotoResp `json:"items"`
	NextCursor string            `json:"nextCursor"`
}

func albumURL(tt *helpers.TestTenant) string {
	return testServerURL + "/calendars/" + tt.CalendarID + "/albums"
}

func TestAlbumPresignRequiresALowercaseDigest(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	png := digestPhoto("required")
	base := func() map[string]any {
		return map[string]any{"contentType": "image/png", "byteSize": len(png)}
	}

	missing := presignDigestPhoto(t, tt, base())
	require.Equal(t, http.StatusUnprocessableEntity, missing.status, string(missing.raw))
	require.Equal(t, "REQUEST.VALIDATION", errorCode(t, missing.raw))

	upper := base()
	upper["sha256"] = "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789"
	got := presignDigestPhoto(t, tt, upper)
	require.Equal(t, http.StatusUnprocessableEntity, got.status, string(got.raw))

	ok := base()
	ok["sha256"] = helpers.SHA256Hex(png)
	good := presignDigestPhoto(t, tt, ok)
	require.Equal(t, http.StatusCreated, good.status, string(good.raw))
	require.NotEmpty(t, good.UploadURL)
}

func TestAlbumConfirmRefusesBytesThatDoNotMatchTheDigest(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	declared := digestPhoto("declared")
	sent := append([]byte(nil), declared...)
	sent[len(sent)-1] ^= 0x01 // same length, different bytes

	pres := presignDigestPhoto(t, tt, map[string]any{
		"contentType": "image/png", "byteSize": len(declared), "sha256": helpers.SHA256Hex(declared),
	})
	require.Equal(t, http.StatusCreated, pres.status, string(pres.raw))
	helpers.UploadToSignedURL(t, pres.UploadURL, "image/png", sent)

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, albumURL(tt)+"/"+pres.PhotoID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	require.Equal(t, "UPLOAD.DIGEST_MISMATCH", errorCode(t, raw))

	var list digestAlbumList
	helpers.DoJSON(t, http.MethodGet, albumURL(tt), tt.AccessToken, nil, &list)
	require.Empty(t, list.Items, "a refused upload never surfaces")

	// The reservation was discarded with the refusal.
	status, _ = helpers.DoJSONStatus(t, http.MethodPost, albumURL(tt)+"/"+pres.PhotoID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)

	// The refused bytes were removed, so the same file can be sent again.
	retry := presignDigestPhoto(t, tt, map[string]any{
		"contentType": "image/png", "byteSize": len(declared), "sha256": helpers.SHA256Hex(declared),
	})
	require.Equal(t, http.StatusCreated, retry.status, string(retry.raw))
	helpers.UploadToSignedURL(t, retry.UploadURL, "image/png", declared)
	status, raw = helpers.DoJSONStatus(t, http.MethodPost, albumURL(tt)+"/"+retry.PhotoID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, string(raw))

	helpers.DoJSON(t, http.MethodGet, albumURL(tt), tt.AccessToken, nil, &list)
	require.Len(t, list.Items, 1)
}

func TestAlbumThumbnailNeedsItsOwnDigestAndIsDroppedWhenItDoesNotMatch(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	photo := digestPhoto("photo")
	thumb := digestPhoto("thumb")

	incomplete := presignDigestPhoto(t, tt, map[string]any{
		"contentType": "image/png", "byteSize": len(photo), "sha256": helpers.SHA256Hex(photo),
		"thumbnailContentType": "image/png", "thumbnailByteSize": len(thumb),
	})
	require.Equal(t, http.StatusBadRequest, incomplete.status, string(incomplete.raw))
	require.Equal(t, "ALBUM.THUMBNAIL_INCOMPLETE", errorCode(t, incomplete.raw))

	confirm := func(thumbDeclared, thumbSent []byte) digestPhotoResp {
		pic := digestPhoto("pic")
		pres := presignDigestPhoto(t, tt, map[string]any{
			"contentType": "image/png", "byteSize": len(pic), "sha256": helpers.SHA256Hex(pic),
			"thumbnailContentType": "image/png", "thumbnailByteSize": len(thumbDeclared),
			"thumbnailSha256": helpers.SHA256Hex(thumbDeclared),
		})
		require.Equal(t, http.StatusCreated, pres.status, string(pres.raw))
		require.NotEmpty(t, pres.ThumbnailUploadURL)
		helpers.UploadToSignedURL(t, pres.UploadURL, "image/png", pic)
		helpers.UploadToSignedURL(t, pres.ThumbnailUploadURL, "image/png", thumbSent)
		var got digestPhotoResp
		helpers.DoJSON(t, http.MethodPost, albumURL(tt)+"/"+pres.PhotoID+"/confirm", tt.AccessToken, nil, &got)
		return got
	}

	matching := confirm(thumb, thumb)
	require.NotEmpty(t, matching.ThumbnailURL, "a thumbnail that matches its digest is kept")

	wrong := append([]byte(nil), thumb...)
	wrong[len(wrong)-1] ^= 0x01
	mismatched := confirm(thumb, wrong)
	require.NotEmpty(t, mismatched.ImageURL, "the picture is not lost to its thumbnail")
	require.Empty(t, mismatched.ThumbnailURL, "a thumbnail that fails its digest is dropped")
}

func TestAlbumPhotoWriteIsConditional(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	pres := uploadDigestPhoto(t, tt, digestPhoto("conditional"))
	url := albumURL(tt) + "/" + pres

	var list digestAlbumList
	helpers.DoJSON(t, http.MethodGet, albumURL(tt), tt.AccessToken, nil, &list)
	require.Len(t, list.Items, 1)
	etag := list.Items[0].ETag
	require.NotEmpty(t, etag, "a list item carries the etag a write needs")

	status, raw := helpers.DoJSONStatus(t, http.MethodPatch, url, tt.AccessToken, map[string]any{"caption": "a"})
	require.Equal(t, http.StatusPreconditionRequired, status, string(raw))
	require.Equal(t, "REQUEST.PRECONDITION_REQUIRED", errorCode(t, raw))

	status, raw, _ = helpers.DoJSONFull(t, http.MethodPatch, url, tt.AccessToken, map[string]any{"caption": "a"},
		map[string]string{"If-Match": `"20000101T000000.000"`})
	require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	require.Equal(t, "ALBUM.STALE", errorCode(t, raw))

	status, raw, hdr := helpers.DoJSONFull(t, http.MethodPatch, url, tt.AccessToken, map[string]any{"caption": "kept"},
		map[string]string{"If-Match": etag})
	require.Equal(t, http.StatusOK, status, string(raw))
	var updated digestPhotoResp
	require.NoError(t, json.Unmarshal(raw, &updated))
	require.Equal(t, "kept", updated.Caption)
	require.NotEqual(t, etag, updated.ETag)
	require.Equal(t, updated.ETag, hdr.Get("ETag"))

	// The old tag is stale now; a delete with it is refused and the photo stays.
	status, raw, _ = helpers.DoJSONFull(t, http.MethodDelete, url, tt.AccessToken, nil, map[string]string{"If-Match": etag})
	require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	status, raw, _ = helpers.DoJSONFull(t, http.MethodDelete, url, tt.AccessToken, nil, map[string]string{"If-Match": updated.ETag})
	require.Equal(t, http.StatusNoContent, status, string(raw))
}

// uploadDigestPhoto runs presign / PUT / confirm with a declared digest and
// returns the photo id.
func uploadDigestPhoto(t *testing.T, tt *helpers.TestTenant, png []byte) string {
	t.Helper()
	pres := presignDigestPhoto(t, tt, map[string]any{
		"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png),
	})
	require.Equal(t, http.StatusCreated, pres.status, string(pres.raw))
	helpers.UploadToSignedURL(t, pres.UploadURL, "image/png", png)
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, albumURL(tt)+"/"+pres.PhotoID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	return pres.PhotoID
}

func TestAlbumPhotoEventLinkIsHiddenFromAReaderWhoCannotSeeTheEvent(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	viewer := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, viewer, "viewer")

	newEvent := func(visibility string) string {
		var evt struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
			"title": "Picnic " + visibility, "allDay": false, "visibility": visibility,
			"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
		}, &evt)
		require.NotEmpty(t, evt.ID)
		return evt.ID
	}
	linked := func(visibility string) string {
		eventID := newEvent(visibility)
		png := digestPhoto("link-" + visibility)
		pres := presignDigestPhoto(t, owner, map[string]any{
			"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png), "eventId": eventID,
		})
		require.Equal(t, http.StatusCreated, pres.status, string(pres.raw))
		helpers.UploadToSignedURL(t, pres.UploadURL, "image/png", png)
		var got digestPhotoResp
		helpers.DoJSON(t, http.MethodPost, albumURL(owner)+"/"+pres.PhotoID+"/confirm", owner.AccessToken, nil, &got)
		require.Equal(t, eventID, got.EventID)
		return pres.PhotoID
	}

	publicPhoto := linked("public")
	confidentialPhoto := linked("confidential")

	eventIDs := func(token string) map[string]*string {
		var list struct {
			Items []struct {
				ID      string  `json:"id"`
				EventID *string `json:"eventId"`
			} `json:"items"`
		}
		helpers.DoJSON(t, http.MethodGet, albumURL(owner), token, nil, &list)
		out := map[string]*string{}
		for _, it := range list.Items {
			out[it.ID] = it.EventID
		}
		return out
	}

	seenByOwner := eventIDs(owner.AccessToken)
	require.NotNil(t, seenByOwner[publicPhoto])
	require.NotNil(t, seenByOwner[confidentialPhoto], "the owner sees every link")

	seenByViewer := eventIDs(viewer.AccessToken)
	require.Len(t, seenByViewer, 2, "the photos themselves stay visible")
	require.NotNil(t, seenByViewer[publicPhoto])
	require.Nil(t, seenByViewer[confidentialPhoto], "an event the reader cannot see has no link")

	// The raw body says null, not a missing member.
	_, raw := helpers.DoJSONStatus(t, http.MethodGet, albumURL(owner), viewer.AccessToken, nil)
	var generic struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &generic))
	for _, it := range generic.Items {
		require.Contains(t, it, "eventId")
	}
}

func TestAlbumPhotoPatchLinksAndUnlinksAnEvent(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken, map[string]any{
		"title": "Trip", "allDay": false,
		"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
	}, &evt)

	photoID := uploadDigestPhoto(t, tt, digestPhoto("patch-link"))
	url := albumURL(tt) + "/" + photoID
	etagOf := func() string {
		var list digestAlbumList
		helpers.DoJSON(t, http.MethodGet, albumURL(tt), tt.AccessToken, nil, &list)
		require.Len(t, list.Items, 1)
		return list.Items[0].ETag
	}

	var linkedPhoto digestPhotoResp
	status, raw, _ := helpers.DoJSONFull(t, http.MethodPatch, url, tt.AccessToken, map[string]any{"eventId": evt.ID},
		map[string]string{"If-Match": etagOf()})
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &linkedPhoto))
	require.Equal(t, evt.ID, linkedPhoto.EventID)

	// A patch that does not name the link leaves it.
	status, raw, _ = helpers.DoJSONFull(t, http.MethodPatch, url, tt.AccessToken, map[string]any{"caption": "c"},
		map[string]string{"If-Match": linkedPhoto.ETag})
	require.Equal(t, http.StatusOK, status, string(raw))
	var captioned digestPhotoResp
	require.NoError(t, json.Unmarshal(raw, &captioned))
	require.Equal(t, evt.ID, captioned.EventID)

	status, raw, _ = helpers.DoJSONFull(t, http.MethodPatch, url, tt.AccessToken, map[string]any{"eventId": nil},
		map[string]string{"If-Match": captioned.ETag})
	require.Equal(t, http.StatusOK, status, string(raw))
	var unlinked struct {
		EventID *string `json:"eventId"`
	}
	require.NoError(t, json.Unmarshal(raw, &unlinked))
	require.Nil(t, unlinked.EventID)
}

func TestAlbumListWalksByCursorAcrossADeletedPhoto(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	var ids []string
	for i := range 3 {
		png := digestPhoto("cursor")
		pres := presignDigestPhoto(t, tt, map[string]any{
			"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png),
			"takenAt": base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
		})
		require.Equal(t, http.StatusCreated, pres.status, string(pres.raw))
		helpers.UploadToSignedURL(t, pres.UploadURL, "image/png", png)
		helpers.DoJSON(t, http.MethodPost, albumURL(tt)+"/"+pres.PhotoID+"/confirm", tt.AccessToken, nil, nil)
		ids = append(ids, pres.PhotoID)
	}

	var first digestAlbumList
	helpers.DoJSON(t, http.MethodGet, albumURL(tt)+"?limit=2", tt.AccessToken, nil, &first)
	require.Len(t, first.Items, 2)
	require.Equal(t, ids[2], first.Items[0].ID, "newest first")
	require.Equal(t, ids[1], first.Items[1].ID)
	require.NotEmpty(t, first.NextCursor)

	// Deleting the photo the cursor names does not break the walk.
	status, raw, _ := helpers.DoJSONFull(t, http.MethodDelete, albumURL(tt)+"/"+ids[1], tt.AccessToken, nil,
		map[string]string{"If-Match": first.Items[1].ETag})
	require.Equal(t, http.StatusNoContent, status, string(raw))

	var second digestAlbumList
	helpers.DoJSON(t, http.MethodGet, albumURL(tt)+"?limit=2&cursor="+first.NextCursor, tt.AccessToken, nil, &second)
	require.Len(t, second.Items, 1)
	require.Equal(t, ids[0], second.Items[0].ID)
	require.Empty(t, second.NextCursor)

	// A cursor from another calendar is refused, not read from somewhere else.
	other := helpers.NewTenant(t, testServerURL)
	status, raw = helpers.DoJSONStatus(t, http.MethodGet, albumURL(other)+"?cursor="+first.NextCursor, other.AccessToken, nil)
	require.Equal(t, http.StatusBadRequest, status, string(raw))

	status, raw = helpers.DoJSONStatus(t, http.MethodGet, albumURL(tt)+"?limit=0", tt.AccessToken, nil)
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
}
