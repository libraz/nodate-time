package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func TestCalendarLifecycle(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	// List calendars — should have 1 (created by NewTenant)
	var cals []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars", tt.AccessToken, nil, &cals)
	require.Len(t, cals, 1)
	require.Equal(t, "テストカレンダー", cals[0].Name)

	// Get single calendar
	var cal struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+tt.CalendarID, tt.AccessToken, nil, &cal)
	require.Equal(t, tt.CalendarID, cal.ID)

	// Update calendar
	var updated struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodPut, testServerURL+"/calendars/"+tt.CalendarID, tt.AccessToken,
		map[string]any{"name": "更新後", "color": "#F35F8C", "coverUrl": ""},
		&updated)
	require.Equal(t, "更新後", updated.Name)
	require.Equal(t, "#F35F8C", updated.Color)

	// Create second calendar
	var cal2 struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars", tt.AccessToken,
		map[string]any{"name": "家族", "color": "#47B2F7"},
		&cal2)
	require.NotEmpty(t, cal2.ID)

	// List should have 2
	var cals2 []struct{ ID string }
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars", tt.AccessToken, nil, &cals2)
	require.Len(t, cals2, 2)

	// Delete second calendar
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, testServerURL+"/calendars/"+cal2.ID, tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	// List should have 1
	var cals3 []struct{ ID string }
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars", tt.AccessToken, nil, &cals3)
	require.Len(t, cals3, 1)
}

// calendarBody is the shape every calendar response is read through here: the
// caller's own standing on the calendar travels with it, so a client never has
// to work out its own role from the member list -- which meant recognising the
// signed-in account by its address.
type calendarBody struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	MemberColor  string `json:"memberColor"`
	PublicShared bool   `json:"publicShared"`
}

func TestCalendarListReportsTheCallersOwnRole(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	viewer := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", viewer.AccessToken, nil, nil)

	// The same calendar, listed by two people, reports two different roles.
	var ownerList []calendarBody
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars", owner.AccessToken, nil, &ownerList)
	require.Len(t, ownerList, 1)
	require.Equal(t, owner.CalendarID, ownerList[0].ID)
	require.Equal(t, "owner", ownerList[0].Role)
	require.Equal(t, owner.CalendarColor, ownerList[0].MemberColor)

	var viewerList []calendarBody
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars", viewer.AccessToken, nil, &viewerList)
	shared, ok := findCalendar(viewerList, owner.CalendarID)
	require.True(t, ok, "the invited calendar must appear in the viewer's list")
	require.Equal(t, "viewer", shared.Role)
	// Their own calendar is still theirs to run: one membership says nothing
	// about the next.
	own, ok := findCalendar(viewerList, viewer.CalendarID)
	require.True(t, ok)
	require.Equal(t, "owner", own.Role)

	// Fetching the calendar on its own answers the same question.
	var single calendarBody
	helpers.DoJSON(t, http.MethodGet, calURL, viewer.AccessToken, nil, &single)
	require.Equal(t, "viewer", single.Role)

	// So does creating one: the creator owns what they just made.
	var created calendarBody
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars", viewer.AccessToken,
		map[string]any{"name": "新規", "color": "#B38BDC"}, &created)
	require.Equal(t, "owner", created.Role)
	require.Equal(t, "#B38BDC", created.MemberColor)
}

func TestRenamingAPubliclySharedCalendarKeepsItShared(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var publicInvite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt.AccessToken,
		map[string]any{"role": "viewer", "isPublic": true}, &publicInvite)
	require.NotEmpty(t, publicInvite.Token)

	var before calendarBody
	helpers.DoJSON(t, http.MethodGet, calURL, tt.AccessToken, nil, &before)
	require.True(t, before.PublicShared)

	// A rename says nothing about who the calendar is shared with, so the
	// response must not report it as no longer shared.
	var renamed calendarBody
	helpers.DoJSON(t, http.MethodPut, calURL, tt.AccessToken,
		map[string]any{"name": "改名後"}, &renamed)
	require.Equal(t, "改名後", renamed.Name)
	require.True(t, renamed.PublicShared, "renaming must not take the calendar off the internet")
	require.Equal(t, "owner", renamed.Role)
}

// findCalendar picks a calendar out of a listing by its public id.
func findCalendar(list []calendarBody, id string) (calendarBody, bool) {
	for _, c := range list {
		if c.ID == id {
			return c, true
		}
	}
	return calendarBody{}, false
}

func TestCalendarMembers(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	// List members — should have 1 (creator)
	var members []struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+tt.CalendarID+"/members", tt.AccessToken, nil, &members)
	require.Len(t, members, 1)
	require.Equal(t, "owner", members[0].Role)
}

func TestCalendarLabels(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	var labels []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars/"+tt.CalendarID+"/labels", tt.AccessToken, nil, &labels)
	require.Len(t, labels, 10)
	require.Equal(t, "#47B2F7", labels[0].Color)
}

func TestCalendarLabelsRequireMembership(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	outsider := helpers.NewTenant(t, testServerURL)

	status, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/calendars/"+owner.CalendarID+"/labels", outsider.AccessToken, nil)
	require.Equal(t, http.StatusForbidden, status)
}

func TestCalendarAccessDenied(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt1 := helpers.NewTenant(t, testServerURL)
	tt2 := helpers.NewTenant(t, testServerURL)

	// tt2 cannot access tt1's calendar
	status, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/calendars/"+tt1.CalendarID, tt2.AccessToken, nil)
	require.Equal(t, 403, status)
}

func TestCalendarColorMustBeHex(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/calendars", tt.AccessToken,
		map[string]any{"name": "bad color", "color": "nothex"})
	require.Equal(t, http.StatusUnprocessableEntity, status)

	status, _ = helpers.DoJSONStatus(t, http.MethodPut, testServerURL+"/calendars/"+tt.CalendarID, tt.AccessToken,
		map[string]any{"name": "bad color", "color": "#12345g"})
	require.Equal(t, http.StatusUnprocessableEntity, status)
}
