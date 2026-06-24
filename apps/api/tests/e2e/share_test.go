package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func TestShareLifecycle(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create invite link
	var invite struct {
		ID       uint32 `json:"id"`
		Token    string `json:"token"`
		Role     string `json:"role"`
		UseCount uint32 `json:"useCount"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt.AccessToken,
		map[string]any{"role": "viewer"}, &invite)
	require.NotEmpty(t, invite.Token)
	require.Equal(t, "viewer", invite.Role)
	require.NotZero(t, invite.ID)

	// List invites
	var invites []struct {
		ID    uint32 `json:"id"`
		Token string `json:"token"`
		Role  string `json:"role"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/invites", tt.AccessToken, nil, &invites)
	require.Len(t, invites, 1)
	require.Equal(t, invite.Token, invites[0].Token)

	// Public calendar view (no auth)
	var pubCal struct {
		CalendarID string `json:"calendarId"`
		Name       string `json:"name"`
		Color      string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/share/"+invite.Token, "", nil, &pubCal)
	require.Equal(t, tt.CalendarID, pubCal.CalendarID)
	require.Equal(t, "テストカレンダー", pubCal.Name)

	// Create an event so we can see it in public view
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "公開イベント",
			"allDay":  false,
			"startAt": "2026-04-20T10:00:00+09:00",
			"endAt":   "2026-04-20T11:00:00+09:00",
		}, nil)

	// Public events view (no auth)
	var pubEvents []struct {
		Title string `json:"title"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/share/"+invite.Token+"/events?start=2026-04-01&end=2026-04-30", "", nil, &pubEvents)
	require.Len(t, pubEvents, 1)
	require.Equal(t, "公開イベント", pubEvents[0].Title)

	// Delete invite
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/invites/"+uintToStr(invite.ID), tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	// Public view should fail after deletion
	status2, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/share/"+invite.Token, "", nil)
	require.Equal(t, 404, status2)

	// List invites should be empty
	var invites2 []struct{ ID uint32 }
	helpers.DoJSON(t, http.MethodGet, calURL+"/invites", tt.AccessToken, nil, &invites2)
	require.Len(t, invites2, 0)
}

func TestShareInvalidToken(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	// Invalid token returns 404
	status, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/share/invalidtoken123", "", nil)
	require.Equal(t, 404, status)

	status2, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/share/invalidtoken123/events", "", nil)
	require.Equal(t, 404, status2)
}

func TestShareAcceptInvite(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt1 := helpers.NewTenant(t, testServerURL)
	tt2 := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt1.CalendarID

	// tt1 creates invite
	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt1.AccessToken,
		map[string]any{"role": "member"}, &invite)

	// tt2 accepts invite
	var accepted struct {
		CalendarID string `json:"calendarId"`
		Role       string `json:"role"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", tt2.AccessToken, nil, &accepted)
	require.Equal(t, tt1.CalendarID, accepted.CalendarID)
	require.Equal(t, "member", accepted.Role)

	// tt2 can now access tt1's calendar
	var cal struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+tt1.CalendarID, tt2.AccessToken, nil, &cal)
	require.Equal(t, tt1.CalendarID, cal.ID)
}

func TestShareNonAdminCannotCreateInvite(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt1 := helpers.NewTenant(t, testServerURL)
	tt2 := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt1.CalendarID

	// tt1 invites tt2 as member
	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt1.AccessToken,
		map[string]any{"role": "member"}, &invite)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", tt2.AccessToken, nil, nil)

	// tt2 (member) tries to create invite — should fail
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/invites", tt2.AccessToken,
		map[string]any{"role": "member"})
	require.Equal(t, 403, status)
}

func TestShareMaxUses(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt1 := helpers.NewTenant(t, testServerURL)
	tt2 := helpers.NewTenant(t, testServerURL)
	tt3 := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt1.CalendarID

	// Create invite with max_uses=1
	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt1.AccessToken,
		map[string]any{"role": "viewer", "maxUses": 1}, &invite)

	// tt2 accepts — should work
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", tt2.AccessToken, nil, nil)

	// tt3 accepts — should fail (max uses reached)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", tt3.AccessToken, nil)
	require.True(t, status == 404 || status == 410, "expected 404 or 410, got %d", status)
}

// helper
func uintToStr(n uint32) string {
	b, _ := json.Marshal(n)
	return string(b)
}
