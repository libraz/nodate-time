package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
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
		map[string]any{"role": "viewer", "isPublic": true}, &invite)
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

	status, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/share/"+invite.Token+"/events?start=not-a-date&end=2026-04-30", "", nil)
	require.Equal(t, http.StatusBadRequest, status)

	// Delete invite
	status, _ = helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/invites/"+uintToStr(invite.ID), tt.AccessToken, nil)
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

func TestSharePublicLinkNotJoinable(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt1 := helpers.NewTenant(t, testServerURL)
	tt2 := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt1.CalendarID

	// A public link is a read-only embed link (isPublic), not a join invite.
	var publicInvite struct {
		Token    string `json:"token"`
		IsPublic bool   `json:"isPublic"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt1.AccessToken,
		map[string]any{"role": "viewer", "isPublic": true}, &publicInvite)
	require.True(t, publicInvite.IsPublic)

	// Public calendar view reports it as not joinable.
	var pubCal struct {
		Joinable bool `json:"joinable"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/share/"+publicInvite.Token, "", nil, &pubCal)
	require.False(t, pubCal.Joinable)

	// Joining through a public link is forbidden.
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+publicInvite.Token+"/accept", tt2.AccessToken, nil)
	require.Equal(t, 403, status)

	// tt2 must not have gained access to the calendar.
	status2, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/calendars/"+tt1.CalendarID, tt2.AccessToken, nil)
	require.True(t, status2 >= 400, "non-member should not access the calendar, got %d", status2)

	// A limited viewer invite remains joinable (regression guard).
	var memberInvite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt1.AccessToken,
		map[string]any{"role": "member", "maxUses": 1}, &memberInvite)
	var pubCal2 struct {
		Joinable bool `json:"joinable"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/share/"+memberInvite.Token, "", nil, &pubCal2)
	require.True(t, pubCal2.Joinable)
}

func TestShareOnlyOneActivePublicLinkPerCalendar(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt.AccessToken,
		map[string]any{"role": "viewer", "isPublic": true}, nil)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/invites", tt.AccessToken,
		map[string]any{"role": "viewer", "isPublic": true})
	require.Equal(t, 409, status)

	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt.AccessToken,
		map[string]any{"role": "member", "maxUses": 1}, &invite)
	require.NotEmpty(t, invite.Token)
}

func TestShareEventsRequirePublicInvite(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt.AccessToken,
		map[string]any{"role": "viewer", "maxUses": 1}, &invite)

	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "Join-only private event",
			"allDay":  false,
			"startAt": "2026-04-20T10:00:00+09:00",
			"endAt":   "2026-04-20T11:00:00+09:00",
		}, nil)

	var pubCal struct {
		Joinable bool `json:"joinable"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/share/"+invite.Token, "", nil, &pubCal)
	require.True(t, pubCal.Joinable)

	var pubEvents []struct {
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/share/"+invite.Token+"/events?start=2026-04-01&end=2026-04-30", "", nil, &pubEvents)
	require.Empty(t, pubEvents)
}

func TestShareRecurringEventsHonorExceptions(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	evts := createWeeklyFriday(t, calURL, tt.AccessToken)

	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt.AccessToken,
		map[string]any{"role": "viewer", "isPublic": true}, &invite)

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evts[1].ID+"?scope=this", tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evts[2].ID+"?scope=this", tt.AccessToken,
		map[string]any{
			"title":   "Public moved",
			"allDay":  false,
			"startAt": "2026-04-17T18:00:00+09:00",
			"endAt":   "2026-04-17T19:00:00+09:00",
		}, nil)

	var pubEvents []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		StartAt string `json:"startAt"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/share/"+invite.Token+"/events?start=2026-04-01&end=2026-04-30", "", nil, &pubEvents)
	require.Len(t, pubEvents, 3)

	moved := 0
	for _, e := range pubEvents {
		require.NotEqual(t, evts[1].ID, e.ID, "cancelled occurrence must not appear in public feed")
		if e.Title == "Public moved" {
			moved++
			require.Equal(t, evts[2].ID, e.ID)
			require.Contains(t, e.StartAt, "T09:00:00Z")
		}
	}
	require.Equal(t, 1, moved, "moved occurrence must replace its original public instance")
}

func TestShareRecurringEventsExpandInEventTimezone(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt.AccessToken,
		map[string]any{"role": "viewer", "isPublic": true}, &invite)

	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":    "NY weekly",
			"allDay":   false,
			"startAt":  "2026-03-01T09:00:00-05:00",
			"endAt":    "2026-03-01T10:00:00-05:00",
			"timezone": "America/New_York",
			"recurrenceRule": map[string]any{
				"freq":     "weekly",
				"interval": 1,
				"byDay":    []string{"SU"},
			},
		}, nil)

	var pubEvents []struct {
		StartAt string `json:"startAt"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/share/"+invite.Token+"/events?start=2026-03-01&end=2026-03-15", "", nil, &pubEvents)
	require.Len(t, pubEvents, 3)
	require.True(t, strings.Contains(pubEvents[0].StartAt, "2026-03-01T14:00:00Z"), pubEvents[0].StartAt)
	require.True(t, strings.Contains(pubEvents[1].StartAt, "2026-03-08T13:00:00Z"), pubEvents[1].StartAt)
	require.True(t, strings.Contains(pubEvents[2].StartAt, "2026-03-15T13:00:00Z"), pubEvents[2].StartAt)
}

// helper
func uintToStr(n uint32) string {
	b, _ := json.Marshal(n)
	return string(b)
}
