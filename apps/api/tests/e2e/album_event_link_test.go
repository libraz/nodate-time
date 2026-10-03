package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A photo's eventId names a live event in every response shape, or is empty in
// all of them. Deleting an event leaves the photo's link column in place, so
// the listing has to apply the same rule the single-photo responses do.
func TestAPhotoStopsNamingItsEventOnceTheEventIsDeleted(t *testing.T) {
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
			"title": "Picnic", "allDay": false,
			"startAt": "2026-05-12T09:00:00+09:00", "endAt": "2026-05-12T10:00:00+09:00",
		}, &evt)

	body := distinctPNG(fmt.Sprintf("event-link-%d", time.Now().UnixNano()))
	var pres albumPresignResp
	helpers.DoJSON(t, http.MethodPost, calURL+"/albums/presign", tt.AccessToken,
		map[string]any{"caption": "linked", "contentType": "image/png", "byteSize": len(body), "eventId": evt.ID}, &pres)
	helpers.UploadToSignedURL(t, pres.UploadURL, "image/png", body)
	var confirmed albumPhotoResp
	helpers.DoJSON(t, http.MethodPost, calURL+"/albums/"+pres.PhotoID+"/confirm", tt.AccessToken, nil, &confirmed)
	require.Equal(t, evt.ID, confirmed.EventID)

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evt.ID, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	var list albumListResp
	helpers.DoJSON(t, http.MethodGet, calURL+"/albums", tt.AccessToken, nil, &list)
	var listed *albumPhotoResp
	for i := range list.Items {
		if list.Items[i].ID == pres.PhotoID {
			listed = &list.Items[i]
		}
	}
	require.NotNil(t, listed)
	require.Empty(t, listed.EventID, "the listing must not link a deleted event")

	var updated albumPhotoResp
	helpers.DoJSON(t, http.MethodPut, calURL+"/albums/"+pres.PhotoID, tt.AccessToken,
		map[string]any{"caption": "still here"}, &updated)
	require.Empty(t, updated.EventID, "and neither does a single-photo response")
}
