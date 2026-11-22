package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/apppassword"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestDAVDiscoveryOptionsNeedNoCredentials verifies that OPTIONS answers
// anonymously and that anything else, whatever path it names, asks for Basic.
func TestDAVDiscoveryOptionsNeedNoCredentials(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	status, h, _ := davRequest(t, http.MethodOptions, "/dav/", "", "", nil, "")
	require.Equal(t, http.StatusNoContent, status)
	require.Contains(t, h.Get("DAV"), "calendar-access")

	owner := helpers.NewTenant(t, testServerURL)
	for _, p := range []string{
		"/dav/",
		"/dav",
		"/dav/" + owner.UserID + "/calendars/" + owner.CalendarID + "/",
		"/dav/not-a-uuid/elsewhere/x/y/z",
	} {
		status, h, _ := davRequest(t, "PROPFIND", p, "", "", map[string]string{"Depth": "0"}, davPropfindPrincipal)
		require.Equal(t, http.StatusUnauthorized, status, p)
		require.True(t, strings.HasPrefix(h.Get("WWW-Authenticate"), "Basic "), p)
	}
}

// TestDAVDiscoveryRefusesEveryDeadCredential verifies that each way an app
// password stops being good answers 401, the same 401, and that an identity
// lock is not one of them.
func TestDAVDiscoveryRefusesEveryDeadCredential(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	propfind := func(a *davAccount) int {
		t.Helper()
		status, _, _ := a.do(t, "PROPFIND", "/dav/", map[string]string{"Depth": "0"}, davPropfindPrincipal)
		return status
	}

	t.Run("wrong password", func(t *testing.T) {
		tt := helpers.NewTenant(t, testServerURL)
		newDAVAccount(t, tt)
		require.Equal(t, http.StatusUnauthorized, propfind(davAccountWith(t, tt, apppassword.Prefix+strings.Repeat("A", 32))))
		require.Equal(t, http.StatusUnauthorized, propfind(davAccountWith(t, tt, tt.Password)))
	})

	t.Run("typed in groups and lower case", func(t *testing.T) {
		tt := helpers.NewTenant(t, testServerURL)
		a := newDAVAccount(t, tt)
		require.Equal(t, http.StatusMultiStatus, propfind(davAccountWith(t, tt, strings.ToLower(apppassword.Format(a.password)))))
	})

	t.Run("revoked", func(t *testing.T) {
		tt := helpers.NewTenant(t, testServerURL)
		a := newDAVAccount(t, tt)
		require.Equal(t, http.StatusMultiStatus, propfind(a))
		_, err := testDB.Exec(`UPDATE dav_app_passwords SET revoked_at = NOW(3) WHERE token_hash = ?`,
			apppassword.Hash(a.password))
		require.NoError(t, err)
		require.Equal(t, http.StatusUnauthorized, propfind(a))
	})

	t.Run("user disabled", func(t *testing.T) {
		tt := helpers.NewTenant(t, testServerURL)
		a := newDAVAccount(t, tt)
		_, err := testDB.Exec(`UPDATE users SET enabled = FALSE WHERE public_id = UUID_TO_BIN(?)`, tt.UserID)
		require.NoError(t, err)
		require.Equal(t, http.StatusUnauthorized, propfind(a))
	})

	t.Run("no live identity", func(t *testing.T) {
		tt := helpers.NewTenant(t, testServerURL)
		a := newDAVAccount(t, tt)
		_, err := testDB.Exec(
			`UPDATE identities i JOIN users u ON u.id = i.user_id SET i.enabled = FALSE
			  WHERE u.public_id = UUID_TO_BIN(?)`, tt.UserID)
		require.NoError(t, err)
		require.Equal(t, http.StatusUnauthorized, propfind(a))
	})

	t.Run("second factor enrolled", func(t *testing.T) {
		tt := helpers.NewTenant(t, testServerURL)
		a := newDAVAccount(t, tt)
		plantConfirmedSecondFactor(t, tt.Email)
		require.Equal(t, http.StatusUnauthorized, propfind(a))
	})

	t.Run("identity locked", func(t *testing.T) {
		tt := helpers.NewTenant(t, testServerURL)
		a := newDAVAccount(t, tt)
		_, err := testDB.Exec(
			`UPDATE identities SET failed_attempts = 5, locked_until_at = NOW(3) + INTERVAL 1 HOUR
			  WHERE provider = 'local' AND subject = ?`, tt.Email)
		require.NoError(t, err)
		require.Equal(t, http.StatusMultiStatus, propfind(a))
	})
}

// TestDAVDiscoveryWalksPrincipalHomeAndCalendars verifies the discovery
// chain and that the home lists exactly the calendars the user belongs to.
func TestDAVDiscoveryWalksPrincipalHomeAndCalendars(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	ctx := context.Background()

	owner := helpers.NewTenant(t, testServerURL)
	own := davOwnCalendars(t, owner)
	other := helpers.NewTenant(t, testServerURL)
	joinAs(t, testServerURL+"/calendars/"+other.CalendarID, other.AccessToken, owner, "viewer")
	stranger := helpers.NewTenant(t, testServerURL)
	a := newDAVAccount(t, owner)

	principal, err := a.client.FindCurrentUserPrincipal(ctx)
	require.NoError(t, err)
	require.Equal(t, a.principal(), principal)

	home, err := a.client.FindCalendarHomeSet(ctx, principal)
	require.NoError(t, err)
	require.Equal(t, a.home(), home)

	cals, err := a.client.FindCalendars(ctx, home)
	require.NoError(t, err)
	got := map[string]bool{}
	for _, c := range cals {
		got[c.Path] = true
	}
	want := map[string]bool{a.calendar(other.CalendarID): true}
	for _, id := range own {
		want[a.calendar(id)] = true
	}
	require.Contains(t, want, a.calendar(owner.CalendarID))
	require.Equal(t, want, got)

	// A collection named without its slash is the same collection.
	status, _, body := a.do(t, "PROPFIND", strings.TrimSuffix(a.calendar(owner.CalendarID), "/"),
		map[string]string{"Depth": "0"}, davPropfindPrincipal)
	require.Equal(t, http.StatusMultiStatus, status, body)
	require.Contains(t, body, a.calendar(owner.CalendarID))

	for name, p := range map[string]string{
		"another user's principal": "/dav/" + stranger.UserID + "/",
		"another user's home":      "/dav/" + stranger.UserID + "/calendars/",
		"a calendar not joined":    a.calendar(stranger.CalendarID),
		"a calendar that is not":   a.calendar(uuid.NewString()),
		"a resource that is not":   a.calendar(owner.CalendarID) + "absent.ics",
	} {
		status, _, body := a.do(t, "PROPFIND", p, map[string]string{"Depth": "0"}, davPropfindPrincipal)
		require.Equal(t, http.StatusNotFound, status, "%s: %s", name, body)
	}

	status, _, _ = a.do(t, http.MethodGet, a.calendar(owner.CalendarID)+"bad%20name.ics", nil, "")
	require.Equal(t, http.StatusBadRequest, status)

	for _, m := range []string{"MKCALENDAR", "MKCOL", "COPY", "MOVE"} {
		status, _, _ := a.do(t, m, a.calendar(owner.CalendarID)+"x.ics", nil, "")
		require.Equal(t, http.StatusForbidden, status, m)
	}
}

// TestDAVDiscoveryPrivilegesFollowTheRole verifies that a collection offers
// DAV:write exactly where the REST API would accept a write.
func TestDAVDiscoveryPrivilegesFollowTheRole(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	ctx := context.Background()

	reader := helpers.NewTenant(t, testServerURL)
	own := davOwnCalendars(t, reader)
	viewed := helpers.NewTenant(t, testServerURL)
	edited := helpers.NewTenant(t, testServerURL)
	joinAs(t, testServerURL+"/calendars/"+viewed.CalendarID, viewed.AccessToken, reader, "viewer")
	joinAs(t, testServerURL+"/calendars/"+edited.CalendarID, edited.AccessToken, reader, "editor")
	systemID := davSystemCalendar(t, reader)
	a := newDAVAccount(t, reader)

	cals, err := a.client.FindCalendars(ctx, a.home())
	require.NoError(t, err)
	readOnly := map[string]bool{}
	for _, c := range cals {
		readOnly[c.Path] = c.ReadOnly
	}
	want := map[string]bool{
		a.calendar(viewed.CalendarID): true,
		a.calendar(edited.CalendarID): false,
		a.calendar(systemID):          true,
	}
	for _, id := range own {
		want[a.calendar(id)] = false
	}
	require.Contains(t, want, a.calendar(reader.CalendarID))
	require.Equal(t, want, readOnly)
}

// davSystemCalendar makes a provider-fed calendar with tt as an editor of it,
// written the way a feed import leaves one: there is no API that creates it.
func davSystemCalendar(t *testing.T, tt *helpers.TestTenant) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	_, err := testDB.Exec(
		`INSERT INTO calendars (public_id, workspace_id, kind, name, color, system_slug)
		 SELECT ?, workspace_id, 'system', 'Holidays', '#888888', ? FROM calendars
		  WHERE public_id = UUID_TO_BIN(?)`,
		id[:], fmt.Sprintf("dav-test-%d", time.Now().UnixNano()), tt.CalendarID)
	require.NoError(t, err)
	_, err = testDB.Exec(
		`INSERT INTO calendar_members (public_id, workspace_id, calendar_id, user_id, role, member_color)
		 SELECT UUID_TO_BIN(UUID()), c.workspace_id, c.id, u.id, 'editor', '#888888'
		   FROM calendars c JOIN users u ON u.public_id = UUID_TO_BIN(?)
		  WHERE c.public_id = ?`,
		tt.UserID, id[:])
	require.NoError(t, err)
	return id.String()
}

// TestDAVDiscoveryRefusesEachPropertyPatch verifies that a PROPPATCH, which
// Apple's clients send for a calendar's order and colour, is answered with
// each property refused and nothing changed.
func TestDAVDiscoveryRefusesEachPropertyPatch(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	a := newDAVAccount(t, owner)
	calPath := a.calendar(owner.CalendarID)
	before, err := a.client.FindCalendars(context.Background(), a.home())
	require.NoError(t, err)

	body := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<A:propertyupdate xmlns:A="DAV:"><A:set><A:prop>` +
		`<B:calendar-order xmlns:B="http://apple.com/ns/ical/">3</B:calendar-order>` +
		`<B:calendar-color xmlns:B="http://apple.com/ns/ical/">#FF0000FF</B:calendar-color>` +
		`</A:prop></A:set><A:remove><A:prop><A:displayname/></A:prop></A:remove></A:propertyupdate>`
	status, _, resp := a.do(t, "PROPPATCH", calPath, nil, body)
	require.Equal(t, http.StatusMultiStatus, status, resp)
	require.Contains(t, resp, calPath)
	require.Contains(t, resp, "HTTP/1.1 403 Forbidden")
	for _, name := range []string{"calendar-order", "calendar-color", "displayname"} {
		require.Contains(t, resp, name)
	}

	after, err := a.client.FindCalendars(context.Background(), a.home())
	require.NoError(t, err)
	require.Equal(t, before, after)

	status, _, _ = a.do(t, "PROPPATCH", calPath, nil, `<A:propertyupdate xmlns:A="DAV:"><A:set>`)
	require.Equal(t, http.StatusBadRequest, status)
}
