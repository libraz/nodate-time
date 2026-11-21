package e2e

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The declared size is signed into the upload URL, so it is also the limit of
// the upload itself: a body of any other length fails the signature. A zero
// cannot be signed for, which would leave the URL accepting an object of any
// size at all -- from anyone allowed to write to the calendar.
func TestAttachmentPresignRejectsASizeItCannotBind(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "Wants a file", "allDay": false,
			"startAt": "2026-06-02T09:00:00+09:00", "endAt": "2026-06-02T10:00:00+09:00",
		}, &evt)

	status, body := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+evt.ID+"/attachments/presign", tt.AccessToken,
		map[string]any{
			"filename": "unbounded.bin", "contentType": "application/octet-stream",
			"byteSize": 0, "sha256": helpers.SHA256Hex(nil),
		})
	require.Equal(t, http.StatusUnprocessableEntity, status,
		"a reservation with no size must be refused: %s", string(body))

	var attachments []map[string]any
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID+"/attachments", tt.AccessToken,
		nil, &attachments)
	require.Empty(t, attachments, "and it must not leave a reservation behind")
}

// A file past the ceiling is refused as a limit, not as a malformed request.
func TestAttachmentPresignRefusesAnOversizedFile(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "Wants a big file", "allDay": false,
			"startAt": "2026-06-02T09:00:00+09:00", "endAt": "2026-06-02T10:00:00+09:00",
		}, &evt)

	status, body := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+evt.ID+"/attachments/presign", tt.AccessToken,
		map[string]any{
			"filename": "huge.bin", "contentType": "application/octet-stream",
			"byteSize": 101 * 1024 * 1024, "sha256": helpers.SHA256Hex([]byte("huge")),
		})
	require.Equal(t, http.StatusRequestEntityTooLarge, status, string(body))
	require.Contains(t, string(body), "ATTACHMENT.TOO_LARGE")
}

// A listing signs a URL for every photo on the page, but the browser fetches
// only the thumbnails the reader scrolls to. A signature that dies while the
// album is still open leaves the rest of the grid broken, so these have to
// outlast a browsing session -- unlike the single URL an explicit download
// hands back, which is followed immediately.
func TestAlbumImageURLsOutlastABrowsingSession(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	uploadOnePhoto(t, tt, map[string]any{"caption": "scrolled to later"})

	var list albumListResp
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+tt.CalendarID+"/albums",
		tt.AccessToken, nil, &list)
	require.Len(t, list.Items, 1)

	parsed, err := url.Parse(list.Items[0].ImageURL)
	require.NoError(t, err)
	exp, err := strconv.ParseInt(parsed.Query().Get("exp"), 10, 64)
	require.NoError(t, err, "the image URL should carry a signed expiry")
	require.GreaterOrEqual(t, time.Until(time.Unix(exp, 0)), time.Hour-time.Minute,
		"a thumbnail loaded lazily must still resolve an hour after the listing")
}

// The reminder offset is a whole number of minutes in a fixed range. A value
// outside it is refused as a malformed request on create and on update alike,
// rather than stored for a client that cannot show or clear it.
func TestEventRejectsAReminderNoClientCanShow(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	create := spaCreateBody()
	create["notificationOffset"] = -1
	status, body := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", tt.AccessToken, create)
	require.Equal(t, http.StatusUnprocessableEntity, status, "create: %s", string(body))
	require.Contains(t, string(body), "REQUEST.VALIDATION")

	// One inside the range is still accepted, and the update path is guarded as
	// well.
	create["notificationOffset"] = 30
	var created struct {
		ID   string `json:"id"`
		ETag string `json:"etag"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken, create, &created)

	status, body = helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, calURL+"/events/"+created.ID,
		tt.AccessToken, map[string]any{"notificationOffset": 10081}, ifMatch(created.ETag))
	require.Equal(t, http.StatusUnprocessableEntity, status, "update: %s", string(body))
	require.Contains(t, string(body), "REQUEST.VALIDATION")
}
