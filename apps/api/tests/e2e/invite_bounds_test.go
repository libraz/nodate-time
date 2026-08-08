package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type inviteResponse struct {
	ID        string     `json:"id"`
	Token     string     `json:"token"`
	MaxUses   *int       `json:"maxUses"`
	UseCount  int        `json:"useCount"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// TestInviteBoundsAreHonoured verifies that the limits a link is created with
// are recorded, reported back, and actually enforced. Without them a join link
// is a standing invitation: it works for whoever finds it, indefinitely, and
// revoking it after the fact is the only remedy left.
func TestInviteBoundsAreHonoured(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// A bounded link reports its bounds, so a listing can tell one link from
	// another rather than offering only "revoke".
	var bounded inviteResponse
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor", "expiresInHours": 24, "maxUses": 1}, &bounded)
	require.NotNil(t, bounded.ExpiresAt)
	require.WithinDuration(t, time.Now().Add(24*time.Hour), *bounded.ExpiresAt, 5*time.Minute)
	require.NotNil(t, bounded.MaxUses)
	require.Equal(t, 1, *bounded.MaxUses)

	// The use limit is enforced: the second person is turned away.
	first := helpers.NewTenant(t, testServerURL)
	second := helpers.NewTenant(t, testServerURL)
	accepted, _ := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/invites/"+bounded.Token+"/accept", first.AccessToken, nil)
	require.True(t, accepted >= 200 && accepted < 300)
	exhausted, _ := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/invites/"+bounded.Token+"/accept", second.AccessToken, nil)
	require.Equal(t, 410, exhausted)

	// A link created without bounds reports what it actually has: the expiry
	// it was given by default, and no use limit, since a link handed to a
	// household is meant to admit all of them.
	var unbounded inviteResponse
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer"}, &unbounded)
	require.NotNil(t, unbounded.ExpiresAt,
		"a link that never expires outlives the reason it was sent")
	require.Nil(t, unbounded.MaxUses)

	// Both appear in the listing with their bounds intact.
	var listed []inviteResponse
	helpers.DoJSON(t, http.MethodGet, calURL+"/invites", owner.AccessToken, nil, &listed)
	byID := map[string]inviteResponse{}
	for _, inv := range listed {
		byID[inv.ID] = inv
	}
	require.NotNil(t, byID[bounded.ID].ExpiresAt)
	require.Nil(t, byID[unbounded.ID].MaxUses)
}

// TestExpiredInviteIsRefused verifies the expiry is a real gate rather than a
// value only shown in a listing.
func TestExpiredInviteIsRefused(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	joiner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv inviteResponse
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor", "expiresInHours": 1}, &inv)
	require.NotEmpty(t, inv.Token)

	// Move the expiry into the past rather than waiting for it: what is being
	// checked is the gate, not the clock.
	_, err := testDB.Exec(
		`UPDATE calendar_invites SET expires_at = NOW() - INTERVAL 1 MINUTE WHERE public_id = UUID_TO_BIN(?)`,
		inv.ID)
	require.NoError(t, err)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/invites/"+inv.Token+"/accept", joiner.AccessToken, nil)
	require.Equal(t, 410, status)

	// And the would-be joiner gained nothing.
	access, _ := helpers.DoJSONStatus(t, http.MethodGet,
		calURL+"/events?start=2026-08-01&end=2026-08-31", joiner.AccessToken, nil)
	require.Equal(t, 403, access)
}
