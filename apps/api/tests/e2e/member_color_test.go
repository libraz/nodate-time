package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type memberItem struct {
	ID    string `json:"id"`
	Color string `json:"color"`
}

func membersOf(t *testing.T, calURL, token string) map[string]memberItem {
	t.Helper()
	var list []memberItem
	helpers.DoJSON(t, http.MethodGet, calURL+"/members", token, nil, &list)
	out := map[string]memberItem{}
	for _, m := range list {
		out[m.ID] = m
	}
	return out
}

// TestMemberColorIsChangeable verifies the product's own model: one shared
// calendar with a colour per person. An invited member arrives on a fixed
// colour, and until this is reachable everyone after the first is drawn the
// same, which is the whole way a reader tells whose plan is whose.
func TestMemberColorIsChangeable(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	joinAs(t, calURL, owner.AccessToken, member, "editor")

	// The member changes their own colour.
	var updated memberItem
	helpers.DoJSON(t, http.MethodPut, calURL+"/members/"+member.UserID+"/color",
		member.AccessToken, map[string]any{"color": "#B38BDC"}, &updated)
	require.Equal(t, "#B38BDC", updated.Color)
	require.Equal(t, "#B38BDC", membersOf(t, calURL, owner.AccessToken)[member.UserID].Color)

	// Whoever runs the calendar can change anyone's, because two people
	// claiming one colour is only resolvable by somebody seeing the whole list.
	adminChange, _ := helpers.DoJSONStatus(t, http.MethodPut,
		calURL+"/members/"+member.UserID+"/color", owner.AccessToken,
		map[string]any{"color": "#2ECC87"})
	require.Equal(t, 200, adminChange)
	require.Equal(t, "#2ECC87", membersOf(t, calURL, owner.AccessToken)[member.UserID].Color)

	// A plain member cannot repaint somebody else.
	otherWay, _ := helpers.DoJSONStatus(t, http.MethodPut,
		calURL+"/members/"+owner.UserID+"/color", member.AccessToken,
		map[string]any{"color": "#E73B3B"})
	require.Equal(t, 403, otherWay)

	// An outsider reaches nothing.
	outsider := helpers.NewTenant(t, testServerURL)
	denied, _ := helpers.DoJSONStatus(t, http.MethodPut,
		calURL+"/members/"+member.UserID+"/color", outsider.AccessToken,
		map[string]any{"color": "#E73B3B"})
	require.Equal(t, 403, denied)
}

// TestEventTakesItsOwnerColour verifies that an event renders in the colour of
// the layer it sits on, and follows that layer when it changes -- which is why
// the event carries no colour of its own.
func TestEventTakesItsOwnerColour(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	joinAs(t, calURL, owner.AccessToken, member, "editor")
	helpers.DoJSON(t, http.MethodPut, calURL+"/members/"+member.UserID+"/color",
		member.AccessToken, map[string]any{"color": "#F35F8C"}, nil)

	var evt struct {
		ID    string `json:"id"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", member.AccessToken,
		map[string]any{
			"title":   "Mine",
			"allDay":  false,
			"startAt": "2026-09-01T10:00:00+09:00",
			"endAt":   "2026-09-01T11:00:00+09:00",
		}, &evt)
	require.Equal(t, "#F35F8C", evt.Color)

	// An event has no colour to send. The field is refused rather than
	// accepted and overwritten, so a client cannot believe it set one.
	rejected, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", member.AccessToken,
		map[string]any{
			"title":   "With a colour",
			"allDay":  false,
			"startAt": "2026-09-02T10:00:00+09:00",
			"endAt":   "2026-09-02T11:00:00+09:00",
			"color":   "#000000",
		})
	require.Equal(t, 422, rejected)

	// Repainting the layer repaints what sits on it.
	helpers.DoJSON(t, http.MethodPut, calURL+"/members/"+member.UserID+"/color",
		member.AccessToken, map[string]any{"color": "#FDC02D"}, nil)

	var after []struct {
		ID    string `json:"id"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-09-01&end=2026-09-30",
		owner.AccessToken, nil, &after)
	require.Len(t, after, 1)
	require.Equal(t, "#FDC02D", after[0].Color)
}
