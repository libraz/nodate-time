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
	require.Equal(t, "admin", members[0].Role)
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

func TestCalendarAccessDenied(t *testing.T) {
	bootstrap(t)
	t.Parallel()


	tt1 := helpers.NewTenant(t, testServerURL)
	tt2 := helpers.NewTenant(t, testServerURL)

	// tt2 cannot access tt1's calendar
	status, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/calendars/"+tt1.CalendarID, tt2.AccessToken, nil)
	require.Equal(t, 403, status)
}
