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
// calendar with a colour per person, which is the whole way a reader tells
// whose plan is whose.
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

// TestJoinersArriveOnColoursNobodyHas pins the colour a link hands out: the
// palette colour fewest members carry, so a calendar filled through one link
// does not draw everyone alike. A repeat stays possible by choice.
func TestJoinersArriveOnColoursNobodyHas(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	first := helpers.NewTenant(t, testServerURL)
	second := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	joinAs(t, calURL, owner.AccessToken, first, "editor")
	joinAs(t, calURL, owner.AccessToken, second, "viewer")

	members := membersOf(t, calURL, owner.AccessToken)
	colours := map[string]bool{}
	for _, m := range members {
		colours[m.Color] = true
	}
	require.Len(t, colours, 3, "owner and two joiners on three colours: %v", members)
	require.Equal(t, "#47B2F7", members[first.UserID].Color, "the earliest free palette colour")
	require.Equal(t, "#F35F8C", members[second.UserID].Color)

	// Claiming a colour somebody already has is allowed.
	helpers.DoJSON(t, http.MethodPut, calURL+"/members/"+second.UserID+"/color",
		second.AccessToken, map[string]any{"color": "#47B2F7"}, nil)
	require.Equal(t, "#47B2F7", membersOf(t, calURL, owner.AccessToken)[second.UserID].Color)
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
		ID        string `json:"id"`
		Color     string `json:"color"`
		OwnerName string `json:"ownerName"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", member.AccessToken,
		map[string]any{
			"title":   "Mine",
			"allDay":  false,
			"startAt": "2026-09-01T10:00:00+09:00",
			"endAt":   "2026-09-01T11:00:00+09:00",
		}, &evt)
	require.Equal(t, "#F35F8C", evt.Color)
	// The owner is named on the event, so a chip can say whose it is
	// without fetching the member list.
	require.Equal(t, member.Name, evt.OwnerName)

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
