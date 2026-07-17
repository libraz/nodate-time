package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestCreateEventRejectsInvalidTimezone verifies that an unrecognized IANA
// timezone is rejected with 400 instead of silently falling back to UTC.
func TestCreateEventRejectsInvalidTimezone(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	bad, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":    "typo zone",
			"allDay":   false,
			"startAt":  "2026-05-10T10:00:00+09:00",
			"endAt":    "2026-05-10T11:00:00+09:00",
			"timezone": "America/New_Yrok",
		})
	require.Equal(t, http.StatusBadRequest, bad)

	// A valid zone still succeeds.
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":    "good zone",
			"allDay":   false,
			"startAt":  "2026-05-10T10:00:00+09:00",
			"endAt":    "2026-05-10T11:00:00+09:00",
			"timezone": "America/New_York",
		}, &evt)
	require.NotEmpty(t, evt.ID)
}
