package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// createInvite makes a link on a calendar and returns what the API said about
// it. The body is passed through so each test can state its own terms.
func createInvite(t *testing.T, calURL, token string, body map[string]any) inviteResponse {
	t.Helper()
	var inv inviteResponse
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", token, body, &inv)
	require.NotEmpty(t, inv.Token)
	return inv
}

func acceptInvite(t *testing.T, inviteToken, accessToken string) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/invites/"+inviteToken+"/accept", accessToken, nil)
}

// useCountOf reads the link back from the owner's listing, which is the only
// place the count is visible.
func useCountOf(t *testing.T, calURL, token, inviteID string) int {
	t.Helper()
	var listed []inviteResponse
	helpers.DoJSON(t, http.MethodGet, calURL+"/invites", token, nil, &listed)
	for _, inv := range listed {
		if inv.ID == inviteID {
			return inv.UseCount
		}
	}
	t.Fatalf("invite %s is not in the listing", inviteID)
	return 0
}

// Following a link to a calendar that has since been deleted is not the
// holder's mistake, and answering with a server error tells them the site is
// broken. The link is intact; what changed is on the other end of it, and that
// is what they can be told.
func TestAcceptingALinkToADeletedCalendarSaysTheCalendarIsGone(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	joiner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, owner, "Doomed")

	inv := createInvite(t, calURL, owner.AccessToken, map[string]any{"role": "editor"})

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL, owner.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	status, raw := acceptInvite(t, inv.Token, joiner.AccessToken)
	require.Equal(t, http.StatusGone, status,
		"a deleted calendar is not an internal error: %s", string(raw))
	require.Contains(t, string(raw), "INVITE.CALENDAR_GONE",
		"and it is not the same answer as a link that never existed")
}

// Removing someone takes their access away; a link they still hold is what
// gives it back. That is the behaviour that makes the branch handling a
// duplicate-key insert unreachable: the membership row is revived by the
// upsert rather than colliding with it, and the rejoin costs a use like any
// other join. If the insert ever stops being an upsert this fails, which is
// the point of pinning it.
func TestARemovedMemberRejoinsThroughALinkTheyStillHold(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	joiner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, owner, "Revolving door")
	eventsQuery := calURL + "/events?start=2026-08-01&end=2026-08-31"

	inv := createInvite(t, calURL, owner.AccessToken,
		map[string]any{"role": "editor", "maxUses": 5})

	status, raw := acceptInvite(t, inv.Token, joiner.AccessToken)
	require.Equal(t, http.StatusOK, status, "body: %s", string(raw))
	require.Equal(t, 1, useCountOf(t, calURL, owner.AccessToken, inv.ID))

	status, _ = helpers.DoJSONStatus(t, http.MethodDelete,
		calURL+"/members/"+joiner.UserID, owner.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, eventsQuery, joiner.AccessToken, nil)
	require.Equal(t, http.StatusForbidden, status, "removal has to remove the access")

	status, raw = acceptInvite(t, inv.Token, joiner.AccessToken)
	require.Equal(t, http.StatusOK, status,
		"a live link is an offer of access, and it is still live: %s", string(raw))
	status, _ = helpers.DoJSONStatus(t, http.MethodGet, eventsQuery, joiner.AccessToken, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 2, useCountOf(t, calURL, owner.AccessToken, inv.ID),
		"rejoining is a join and spends a use")
}

// Following the same link twice as a member already on the calendar must not
// spend anything: the second click is the same person arriving at the same
// place, and a single-use link would otherwise be finished by its own holder.
func TestAcceptingALinkYouHaveAlreadyUsedSpendsNothing(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	joiner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, owner, "Twice")

	inv := createInvite(t, calURL, owner.AccessToken,
		map[string]any{"role": "editor", "maxUses": 1})

	status, _ := acceptInvite(t, inv.Token, joiner.AccessToken)
	require.Equal(t, http.StatusOK, status)
	status, raw := acceptInvite(t, inv.Token, joiner.AccessToken)
	require.Equal(t, http.StatusOK, status, "body: %s", string(raw))
	require.Equal(t, 1, useCountOf(t, calURL, owner.AccessToken, inv.ID))
}

// A join link that never expires makes removing someone reversible by whoever
// still holds it, which means access was never really taken away. A link
// created without terms therefore gets a life of its own.
func TestAJoinLinkExpiresEvenWhenNoTermsAreGiven(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, owner, "Default terms")

	inv := createInvite(t, calURL, owner.AccessToken, map[string]any{"role": "editor"})
	require.NotNil(t, inv.ExpiresAt, "a link with no stated expiry still has one")
	require.WithinDuration(t, time.Now().Add(7*24*time.Hour), *inv.ExpiresAt, time.Hour)

	// The default is on the life of the link, not on how many people it lets
	// in: one link for a family of five is the ordinary case.
	require.Nil(t, inv.MaxUses)
}

// A read-only publication is not an offer of membership, so nothing about it
// is taken back by removing a member and it keeps working until it is revoked.
// Defaulting it to a week would take embedded calendars offline weekly.
func TestAPublicShareLinkKeepsWorkingWithoutAnExpiry(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, owner, "Published")

	inv := createInvite(t, calURL, owner.AccessToken, map[string]any{"isPublic": true})
	require.Nil(t, inv.ExpiresAt,
		"a published feed is not an invitation and does not expire on its own")
}

// The default belongs to the moment a link is created, not to the moment one
// is followed. Links already in people's hands were issued without an expiry
// and keep the terms they were issued under; reading the default at acceptance
// instead would expire every one of them at once, and the holder would be
// turned away by a rule that did not exist when they were given the link.
func TestALinkIssuedWithoutAnExpiryIsStillHonoured(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	joiner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, owner, "Grandfathered")

	inv := createInvite(t, calURL, owner.AccessToken, map[string]any{"role": "editor"})

	// What a row created before the default looks like.
	_, err := testDB.Exec(
		`UPDATE calendar_invites SET expires_at = NULL, created_at = NOW(3) - INTERVAL 60 DAY
		 WHERE public_id = UUID_TO_BIN(?)`, inv.ID)
	require.NoError(t, err)

	status, raw := acceptInvite(t, inv.Token, joiner.AccessToken)
	require.Equal(t, http.StatusOK, status,
		"a link issued without an expiry must not be expired retroactively: %s", string(raw))
}
