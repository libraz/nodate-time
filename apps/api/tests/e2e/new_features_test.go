package e2e

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Password reset ---

var resetTokenRegex = regexp.MustCompile(`/reset-password\?token=([0-9a-f]+)`)

func extractResetToken(t *testing.T, body string) string {
	t.Helper()
	m := resetTokenRegex.FindStringSubmatch(body)
	require.Len(t, m, 2, "no reset token in mail body: %s", body)
	return m[1]
}

func TestPasswordResetHappyPath(t *testing.T) {
	bootstrap(t)
	// not Parallel: shares testMailer

	tt := helpers.NewTenant(t, testServerURL)

	// Request reset
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/password-reset/request", "",
		map[string]any{"email": tt.Email}, nil)

	msg, ok := testMailer.LastFor(tt.Email)
	require.True(t, ok, "no email captured for %s", tt.Email)
	token := extractResetToken(t, msg.Text)

	// Confirm with new password
	newPass := "newpass-secure-123"
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/password-reset/confirm", "",
		map[string]any{"token": token, "newPassword": newPass}, nil)

	// Old password no longer works
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": tt.Email, "password": tt.Password})
	assert.Equal(t, 401, status)

	// New password works
	var loginResp struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": tt.Email, "password": newPass}, &loginResp)
	assert.NotEmpty(t, loginResp.Token)
}

func TestPasswordResetTokenSingleUse(t *testing.T) {
	bootstrap(t)

	tt := helpers.NewTenant(t, testServerURL)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/password-reset/request", "",
		map[string]any{"email": tt.Email}, nil)

	msg, ok := testMailer.LastFor(tt.Email)
	require.True(t, ok)
	token := extractResetToken(t, msg.Text)

	// First use succeeds
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/password-reset/confirm", "",
		map[string]any{"token": token, "newPassword": "first-pass-secure"}, nil)

	// Second use is rejected
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/password-reset/confirm", "",
		map[string]any{"token": token, "newPassword": "second-pass-secure"})
	assert.Equal(t, 400, status)
}

func TestPasswordResetUnknownEmail(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	// Always 200 (no enumeration)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/password-reset/request", "",
		map[string]any{"email": fmt.Sprintf("ghost-%d@nope.local", time.Now().UnixNano())})
	assert.Equal(t, 200, status)
}

func TestPasswordResetInvalidToken(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/password-reset/confirm", "",
		map[string]any{
			"token":       strings.Repeat("a", 64),
			"newPassword": "doesnt-matter-12345",
		})
	assert.Equal(t, 400, status)
}

// --- Member admin ---

func TestUpdateMemberRole(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "member"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", guest.AccessToken, nil, nil)

	// Promote guest to admin
	helpers.DoJSON(t, http.MethodPut, calURL+"/members/"+guest.UserID+"/role", owner.AccessToken,
		map[string]any{"role": "admin"}, nil)

	var members []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/members", owner.AccessToken, nil, &members)
	roles := map[string]string{}
	for _, m := range members {
		roles[m.ID] = m.Role
	}
	assert.Equal(t, "admin", roles[guest.UserID])
}

func TestRemoveMemberLastAdmin(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// Owner is the only admin — can't remove self
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/members/"+owner.UserID, owner.AccessToken, nil)
	assert.GreaterOrEqual(t, status, 400)
	assert.Less(t, status, 500)
}

func TestRemoveMember(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "member"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", guest.AccessToken, nil, nil)

	// Owner removes guest
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/members/"+guest.UserID, owner.AccessToken, nil)
	require.GreaterOrEqual(t, status, 200)
	require.Less(t, status, 300)

	// Guest is no longer in member list
	var members []struct {
		UserID string `json:"userId"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/members", owner.AccessToken, nil, &members)
	for _, m := range members {
		require.NotEqual(t, guest.UserID, m.UserID)
	}
}

// --- iCal export / import ---

func TestICalExportImportRoundTrip(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	src := helpers.NewTenant(t, testServerURL)
	dst := helpers.NewTenant(t, testServerURL)
	srcCalURL := testServerURL + "/calendars/" + src.CalendarID
	dstCalURL := testServerURL + "/calendars/" + dst.CalendarID

	// Seed two events on src
	helpers.DoJSON(t, http.MethodPost, srcCalURL+"/events", src.AccessToken,
		map[string]any{
			"title": "Lunch with Alice", "allDay": false,
			"startAt": "2026-06-01T12:00:00Z", "endAt": "2026-06-01T13:00:00Z",
			"location": "Cafe",
		}, nil)
	helpers.DoJSON(t, http.MethodPost, srcCalURL+"/events", src.AccessToken,
		map[string]any{
			"title": "Holiday", "allDay": true,
			"startAt": "2026-06-05T00:00:00Z", "endAt": "2026-06-06T00:00:00Z",
		}, nil)

	// Export src as iCal
	req, err := http.NewRequest(http.MethodGet, srcCalURL+"/export?format=ics", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+src.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/calendar")

	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	ics := string(buf)
	require.Contains(t, ics, "BEGIN:VCALENDAR")
	require.Contains(t, ics, "Lunch with Alice")
	require.Contains(t, ics, "Holiday")

	// All lines must be <= 75 octets (folded)
	for _, line := range strings.Split(strings.ReplaceAll(ics, "\r\n", "\n"), "\n") {
		require.LessOrEqual(t, len(line), 75, "unfolded ics line: %q", line)
	}

	// Import into dst calendar
	var imp struct {
		Imported int `json:"imported"`
		Skipped  int `json:"skipped"`
		Failed   int `json:"failed"`
	}
	helpers.DoJSON(t, http.MethodPost, dstCalURL+"/import", dst.AccessToken,
		map[string]any{"ics": ics}, &imp)
	require.Equal(t, 2, imp.Imported)
	require.Equal(t, 0, imp.Failed)

	// Verify titles roundtrip
	var listed []struct {
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet, dstCalURL+"/events?start=2026-05-01&end=2026-07-01", dst.AccessToken, nil, &listed)
	titles := map[string]bool{}
	for _, e := range listed {
		titles[e.Title] = true
	}
	assert.True(t, titles["Lunch with Alice"])
	assert.True(t, titles["Holiday"])
}

// --- OAuth state validation ---

func TestOAuthCallbackBadState(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	// State that was never issued should be rejected.
	status, _ := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/auth/oauth/google/callback?code=abc&state=fakestate", "", nil)
	assert.GreaterOrEqual(t, status, 400)
	assert.Less(t, status, 500)
}
