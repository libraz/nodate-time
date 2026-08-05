package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// spaCreateBody is the exact field set the browser client sends when saving a
// new event. The body schema forbids unknown properties, so any field the
// client adds without a matching input field fails every save with 422 — the
// client's own tests cannot catch that, because they never reach this schema.
func spaCreateBody() map[string]any {
	return map[string]any{
		"title":              "定例MTG",
		"allDay":             false,
		"startAt":            "2026-04-20T15:00:00+09:00",
		"endAt":              "2026-04-20T16:00:00+09:00",
		"timezone":           "Asia/Tokyo",
		"location":           "会議室A",
		"memo":               "議題：Q2計画",
		"url":                "",
		"notificationOffset": nil,
		"participants":       []string{},
		"ownerId":            nil,
		"recurrenceRule":     nil,
	}
}

func TestEventAcceptsBrowserClientPayload(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var created struct {
		ID    string `json:"id"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken, spaCreateBody(), &created)
	require.NotEmpty(t, created.ID)

	// The full-replace update and the drag/resize payloads carry the same set.
	update := spaCreateBody()
	update["title"] = "定例MTG（移動）"
	update["startAt"] = "2026-04-21T15:00:00+09:00"
	update["endAt"] = "2026-04-21T16:00:00+09:00"

	var updated struct {
		Title string `json:"title"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+created.ID, tt.AccessToken, update, &updated)
	require.Equal(t, "定例MTG（移動）", updated.Title)
	// Colour comes from the owner's membership on both responses; it is not a
	// writable field, which is why the client must not send one.
	require.Equal(t, created.Color, updated.Color)
}

func TestEventRejectsUnknownBodyField(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	body := spaCreateBody()
	body["color"] = "#47B2F7"

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", tt.AccessToken, body)
	require.Equal(t, http.StatusUnprocessableEntity, status,
		"unknown body fields must fail loudly rather than be ignored; body: %s", string(raw))
}
