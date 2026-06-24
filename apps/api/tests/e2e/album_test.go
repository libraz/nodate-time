package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type albumPresignResp struct {
	PhotoID    string `json:"photoId"`
	UploadURL  string `json:"uploadUrl"`
	StorageKey string `json:"storageKey"`
}

type albumPhotoResp struct {
	ID          string `json:"id"`
	CalendarID  string `json:"calendarId"`
	Caption     string `json:"caption"`
	ContentType string `json:"contentType"`
	EventID     string `json:"eventId"`
	ByteSize    int64  `json:"byteSize"`
	ImageURL    string `json:"imageUrl"`
	UploadedBy  struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"uploadedBy"`
}

type albumListResp struct {
	Items      []albumPhotoResp `json:"items"`
	NextCursor string           `json:"nextCursor"`
}

// uploadOnePhoto is a convenience helper.
func uploadOnePhoto(t *testing.T, tt *helpers.TestTenant, body map[string]any) albumPresignResp {
	t.Helper()
	png := helpers.TinyPNG()
	body["contentType"] = "image/png"
	body["byteSize"] = len(png)
	var pres albumPresignResp
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+tt.CalendarID+"/albums/presign",
		tt.AccessToken, body, &pres)
	helpers.UploadToPresignedURL(t, pres.UploadURL, "image/png", png)
	// Presign creates the row disabled; confirm makes it visible.
	helpers.DoJSON(t, http.MethodPost,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/"+pres.PhotoID+"/confirm",
		tt.AccessToken, nil, nil)
	return pres
}

func TestAlbumPhotoLifecycle(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	pres := uploadOnePhoto(t, tt, map[string]any{"caption": "hello"})
	require.NotEmpty(t, pres.PhotoID)

	// List should include it with an imageUrl.
	var list albumListResp
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+tt.CalendarID+"/albums",
		tt.AccessToken, nil, &list)
	require.Len(t, list.Items, 1)
	got := list.Items[0]
	assert.Equal(t, pres.PhotoID, got.ID)
	assert.Equal(t, "hello", got.Caption)
	assert.NotEmpty(t, got.ImageURL)
	assert.Equal(t, tt.UserID, got.UploadedBy.ID)

	// Verify the imageUrl actually returns bytes.
	bs := helpers.FetchURL(t, got.ImageURL)
	assert.Equal(t, helpers.TinyPNG(), bs)

	// Update caption
	var updated albumPhotoResp
	newCaption := "updated"
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/calendars/"+tt.CalendarID+"/albums/"+pres.PhotoID,
		tt.AccessToken, map[string]any{"caption": newCaption}, &updated)
	assert.Equal(t, newCaption, updated.Caption)

	// Delete
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/"+pres.PhotoID,
		tt.AccessToken, nil)
	assert.Equal(t, 204, status)

	// List should now be empty.
	var listAfter albumListResp
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+tt.CalendarID+"/albums",
		tt.AccessToken, nil, &listAfter)
	assert.Len(t, listAfter.Items, 0)
}

func TestAlbumPhotoEventLink(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "Album-link event", "allDay": false,
			"startAt": "2026-06-01T10:00:00+09:00", "endAt": "2026-06-01T11:00:00+09:00",
		}, &evt)

	pres := uploadOnePhoto(t, tt, map[string]any{"eventId": evt.ID})
	var list albumListResp
	helpers.DoJSON(t, http.MethodGet, calURL+"/albums", tt.AccessToken, nil, &list)
	require.Len(t, list.Items, 1)
	assert.Equal(t, evt.ID, list.Items[0].EventID)

	// Clear the link.
	empty := ""
	var updated albumPhotoResp
	helpers.DoJSON(t, http.MethodPut, calURL+"/albums/"+pres.PhotoID, tt.AccessToken,
		map[string]any{"eventId": empty}, &updated)
	assert.Empty(t, updated.EventID)
}

func TestAlbumNonMemberForbidden(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	uploadOnePhoto(t, owner, map[string]any{"caption": "private"})

	stranger := helpers.NewTenant(t, testServerURL)
	// Stranger uses their own token but the owner's calendar ID — should be 403.
	status, _ := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/calendars/"+owner.CalendarID+"/albums", stranger.AccessToken, nil)
	assert.Equal(t, 403, status)

	status2, _ := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/calendars/"+owner.CalendarID+"/albums/presign", stranger.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": 64})
	assert.Equal(t, 403, status2)
}

func TestAlbumPhotoNonImage(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/presign", tt.AccessToken,
		map[string]any{"contentType": "application/zip", "byteSize": 100})
	assert.Equal(t, 400, status)
}

func TestAlbumPhotoTooLarge(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/presign", tt.AccessToken,
		map[string]any{"contentType": "image/jpeg", "byteSize": 21 * 1024 * 1024})
	assert.Equal(t, 400, status)
}

func TestAlbumPagination(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	// Upload 4 photos.
	for i := 0; i < 4; i++ {
		uploadOnePhoto(t, tt, map[string]any{"caption": "p"})
	}

	// Limit 2 should yield 2 + nextCursor.
	var page1 albumListResp
	helpers.DoJSON(t, http.MethodGet,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums?limit=2",
		tt.AccessToken, nil, &page1)
	require.Len(t, page1.Items, 2)
	require.NotEmpty(t, page1.NextCursor)

	var page2 albumListResp
	helpers.DoJSON(t, http.MethodGet,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums?limit=2&cursor="+page1.NextCursor,
		tt.AccessToken, nil, &page2)
	require.Len(t, page2.Items, 2)
	assert.Empty(t, page2.NextCursor, "second page should be the last")

	// First page should not overlap with second page.
	seen := map[string]bool{}
	for _, it := range page1.Items {
		seen[it.ID] = true
	}
	for _, it := range page2.Items {
		assert.False(t, seen[it.ID], "page 2 item %s appeared in page 1", it.ID)
	}
}

func TestAlbumDownloadURL(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	pres := uploadOnePhoto(t, tt, map[string]any{"caption": "dl"})

	var dl struct {
		DownloadURL string `json:"downloadUrl"`
	}
	helpers.DoJSON(t, http.MethodGet,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/"+pres.PhotoID+"/download",
		tt.AccessToken, nil, &dl)
	require.NotEmpty(t, dl.DownloadURL)
	body := helpers.FetchURL(t, dl.DownloadURL)
	assert.Equal(t, helpers.TinyPNG(), body)

	// Non-member cannot download.
	stranger := helpers.NewTenant(t, testServerURL)
	status, _ := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/"+pres.PhotoID+"/download",
		stranger.AccessToken, nil)
	assert.Equal(t, 403, status)
}

func TestAlbumWithoutStorage(t *testing.T) {
	bootstrap(t)
	if helpers.StorageEnabled() {
		t.Skip("only meaningful when storage is disabled")
	}
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/calendars/"+tt.CalendarID+"/albums/presign", tt.AccessToken,
		map[string]any{"contentType": "image/png", "byteSize": 64})
	assert.Equal(t, 503, status)

	// List should still return empty.
	var list albumListResp
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+tt.CalendarID+"/albums",
		tt.AccessToken, nil, &list)
	assert.Len(t, list.Items, 0)
}
