package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type shareListing struct {
	Items   []map[string]any `json:"items"`
	HasMore bool             `json:"hasMore"`
}

func getShareListing(t *testing.T, token, query string) (shareListing, []byte) {
	t.Helper()
	status, raw := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/share/"+token+"/events?"+query, "", nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var out shareListing
	require.NoError(t, json.Unmarshal(raw, &out))
	return out, raw
}

// createSharedDailySeries adds a series that has an occurrence on every day of 2026.
func createSharedDailySeries(t *testing.T, calURL, token, title, visibility string) {
	t.Helper()
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", token, map[string]any{
		"title": title, "allDay": false, "visibility": visibility,
		"startAt": "2026-01-01T10:00:00Z", "endAt": "2026-01-01T11:00:00Z",
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "count": 365},
	}, nil)
}

const shareYearQuery = "start=2026-01-01&end=2026-12-31"

// Occurrences the reader cannot see must not use up the listing's cap.
func TestShareHiddenOccurrencesDoNotTakeCapSlots(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	token := publicShareToken(t, calURL, owner.AccessToken)

	// 14 series x 365 days is more than the cap, all of it confidential.
	for i := 0; i < 14; i++ {
		createSharedDailySeries(t, calURL, owner.AccessToken, "Hidden", "confidential")
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "The one visible event", "allDay": false, "visibility": "public",
		"startAt": "2026-12-20T10:00:00Z", "endAt": "2026-12-20T11:00:00Z",
	}, nil)

	out, raw := getShareListing(t, token, shareYearQuery)
	require.Len(t, out.Items, 1, string(raw[:min(len(raw), 300)]))
	require.Equal(t, "The one visible event", out.Items[0]["title"])
	require.False(t, out.HasMore)
}

// hasMore is set only when visible occurrences were cut.
func TestShareHasMoreWhenVisibleOccurrencesExceedTheCap(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	token := publicShareToken(t, calURL, owner.AccessToken)

	for i := 0; i < 14; i++ {
		createSharedDailySeries(t, calURL, owner.AccessToken, "Open", "public")
	}
	out, _ := getShareListing(t, token, shareYearQuery)
	require.Len(t, out.Items, 5000)
	require.True(t, out.HasMore)
}

func TestShareEmptyListingIsAnEmptyEnvelope(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	token := publicShareToken(t, calURL, owner.AccessToken)

	_, raw := getShareListing(t, token, "start=2026-07-01&end=2026-07-31")
	require.JSONEq(t, `{"items":[],"hasMore":false}`, string(raw))
}

func TestInviteLimitIsEnforcedAndFreedByRevoking(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var first struct {
		ID string `json:"id"`
	}
	for i := 0; i < 500; i++ {
		var inv struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
			map[string]any{"role": "viewer"}, &inv)
		if i == 0 {
			first = inv
		}
	}

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer"})
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Contains(t, string(raw), "INVITE.LIMIT_REACHED")

	status, raw = helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/invites/"+first.ID, owner.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	status, raw = helpers.DoJSONStatus(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer"})
	require.Equal(t, http.StatusCreated, status, string(raw))
}

// Accepting an invite creates a membership, so it is held to the same
// member limit as adding one directly.
func TestAcceptingAnInviteIsHeldToTheMemberLimit(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	joiner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer"}, &inv)

	// Fill the calendar with 499 users made for this test, bypassing the API.
	tag := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	var workspaceID, calendarID uint32
	require.NoError(t, testDB.QueryRowContext(t.Context(),
		`SELECT workspace_id, id FROM calendars WHERE public_id = UUID_TO_BIN(?)`, owner.CalendarID,
	).Scan(&workspaceID, &calendarID))
	t.Cleanup(func() {
		ctx := context.Background()
		pattern := "member-fill-" + tag + "-%"
		for _, q := range []string{
			`DELETE cm FROM calendar_members cm INNER JOIN users u ON u.id = cm.user_id WHERE u.email LIKE ?`,
			`DELETE wm FROM workspace_members wm INNER JOIN users u ON u.id = wm.user_id WHERE u.email LIKE ?`,
			`DELETE FROM users WHERE email LIKE ?`,
		} {
			_, _ = testDB.ExecContext(ctx, q, pattern)
		}
	})
	tx, err := testDB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	for i := 0; i < 499; i++ {
		res, err := tx.Exec(`INSERT INTO users (public_id, email, display_name)
			VALUES (UUID_TO_BIN(UUID()), ?, 'filler')`, fmt.Sprintf("member-fill-%s-%d@fill.test", tag, i))
		require.NoError(t, err)
		id, err := res.LastInsertId()
		require.NoError(t, err)
		_, err = tx.Exec(`INSERT INTO workspace_members (public_id, workspace_id, user_id)
			VALUES (UUID_TO_BIN(UUID()), ?, ?)`, workspaceID, id)
		require.NoError(t, err)
		_, err = tx.Exec(`INSERT INTO calendar_members (public_id, workspace_id, calendar_id, user_id, role)
			VALUES (UUID_TO_BIN(UUID()), ?, ?, ?, 'viewer')`, workspaceID, calendarID, id)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())

	status, raw := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept",
		joiner.AccessToken, nil)
	require.Equal(t, http.StatusConflict, status, string(raw))
	require.Contains(t, string(raw), "MEMBER.LIMIT_REACHED")
}
