package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestAUsedUpLinkStopsOfferingToJoin covers the page a share link opens.
//
// The landing page is served before anyone signs in, so it is the only place
// the state of a link can be shown. It matched a link whether or not the link
// still had a use left, and it decided the join button from nothing but
// "is this an embed link" -- so a single-use link that had already been taken
// still showed the calendar's name and a Join button, and the accept endpoint
// refused what the page had just offered.
func TestAUsedUpLinkStopsOfferingToJoin(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	joiner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor", "maxUses": 1}, &invite)

	landing := testServerURL + "/share/" + invite.Token
	var page struct {
		Name     string `json:"name"`
		Joinable bool   `json:"joinable"`
		Spent    bool   `json:"spent"`
	}
	helpers.DoJSON(t, http.MethodGet, landing, "", nil, &page)
	require.True(t, page.Joinable, "an unused single-use link should still offer to join")
	require.False(t, page.Spent)

	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept",
		joiner.AccessToken, nil, nil)

	page = struct {
		Name     string `json:"name"`
		Joinable bool   `json:"joinable"`
		Spent    bool   `json:"spent"`
	}{}
	helpers.DoJSON(t, http.MethodGet, landing, "", nil, &page)
	require.NotEmpty(t, page.Name, "the page still names the calendar the link was for")
	require.True(t, page.Spent, "the link has been used as often as it allowed")
	require.False(t, page.Joinable, "an offer the accept endpoint would refuse must not be made")
}

// TestALinkWithNoUseLimitKeepsOfferingToJoin guards the other side of the
// same predicate: most links are unlimited, and a NULL max_uses must not read
// as "no uses left".
func TestALinkWithNoUseLimitKeepsOfferingToJoin(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	joiner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor"}, &invite)

	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept",
		joiner.AccessToken, nil, nil)

	var page struct {
		Joinable bool `json:"joinable"`
		Spent    bool `json:"spent"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/share/"+invite.Token, "", nil, &page)
	require.False(t, page.Spent)
	require.True(t, page.Joinable, "an unlimited link stays open after someone follows it")
}
