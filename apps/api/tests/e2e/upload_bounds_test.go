package e2e

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

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
	expires, err := strconv.Atoi(parsed.Query().Get("X-Amz-Expires"))
	require.NoError(t, err, "the image URL should carry a signed expiry")
	require.GreaterOrEqual(t, expires, 3600,
		"a thumbnail loaded lazily must still resolve an hour after the listing")
}

// The reminder offsets are a fixed set: the picker offers those and nothing
// else, and an event carrying anything else reads back in the modal as having
// no reminder at all. Only the client enforced that, so an offset stored
// through the API directly was one nobody could see or clear.
func TestEventRejectsAReminderNoClientCanShow(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	create := spaCreateBody()
	create["notificationOffset"] = 7
	status, body := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", tt.AccessToken, create)
	require.Equal(t, http.StatusBadRequest, status, "create: %s", string(body))
	require.Contains(t, string(body), "EVENT.NOTIFICATION_OFFSET_INVALID")

	// One the picker does offer is still accepted, and the update path is
	// guarded as well -- a full replace can set the field just as a create can.
	create["notificationOffset"] = 30
	var created struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken, create, &created)

	update := spaCreateBody()
	update["notificationOffset"] = 7
	status, body = helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+created.ID,
		tt.AccessToken, update)
	require.Equal(t, http.StatusBadRequest, status, "update: %s", string(body))
	require.Contains(t, string(body), "EVENT.NOTIFICATION_OFFSET_INVALID")
}
