package e2e

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// davAccount is a tenant reaching the server over CalDAV with an app password,
// both through the go-webdav client and through raw requests.
type davAccount struct {
	*helpers.TestTenant
	password string
	client   *caldav.Client
}

// newDAVAccount gives tt an app password and a client presenting it.
func newDAVAccount(t *testing.T, tt *helpers.TestTenant) *davAccount {
	t.Helper()
	return davAccountWith(t, tt, helpers.CreateAppPassword(t, testDB, tt.UserID, "e2e device"))
}

func davAccountWith(t *testing.T, tt *helpers.TestTenant, password string) *davAccount {
	t.Helper()
	hc := webdav.HTTPClientWithBasicAuth(http.DefaultClient, tt.Email, password)
	c, err := caldav.NewClient(hc, testServerURL+"/dav/")
	require.NoError(t, err)
	return &davAccount{TestTenant: tt, password: password, client: c}
}

func (a *davAccount) principal() string { return "/dav/" + a.UserID + "/" }

func (a *davAccount) home() string { return a.principal() + "calendars/" }

func (a *davAccount) calendar(calendarID string) string { return a.home() + calendarID + "/" }

// davRequest sends one raw request to a server path. An empty password sends
// no credentials.
func davRequest(
	t *testing.T, method, p, username, password string, headers map[string]string, body string,
) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(method, testServerURL+p, strings.NewReader(body))
	require.NoError(t, err)
	if password != "" {
		req.SetBasicAuth(username, password)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, string(raw)
}

func (a *davAccount) do(t *testing.T, method, p string, headers map[string]string, body string) (int, http.Header, string) {
	t.Helper()
	return davRequest(t, method, p, a.Email, a.password, headers, body)
}

const davPropfindPrincipal = `<?xml version="1.0" encoding="utf-8"?>` +
	`<d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/><d:resourcetype/></d:prop></d:propfind>`

const davPropfindSyncToken = `<?xml version="1.0" encoding="utf-8"?>` +
	`<d:propfind xmlns:d="DAV:"><d:prop><d:sync-token/></d:prop></d:propfind>`

var davSyncTokenElement = regexp.MustCompile(`<(?:[A-Za-z0-9]+:)?sync-token[^>]*>([^<]*)</`)

// propfindSyncToken reads the collection's DAV:sync-token property.
func (a *davAccount) propfindSyncToken(t *testing.T, calPath string) string {
	t.Helper()
	status, _, body := a.do(t, "PROPFIND", calPath, map[string]string{"Depth": "0"}, davPropfindSyncToken)
	require.Equal(t, http.StatusMultiStatus, status, body)
	m := davSyncTokenElement.FindStringSubmatch(body)
	require.NotNil(t, m, body)
	return m[1]
}

func davSyncBody(token string) string {
	tok := "<d:sync-token/>"
	if token != "" {
		tok = "<d:sync-token>" + token + "</d:sync-token>"
	}
	return `<?xml version="1.0" encoding="utf-8"?>` +
		`<d:sync-collection xmlns:d="DAV:">` + tok +
		`<d:sync-level>1</d:sync-level><d:prop><d:getetag/></d:prop></d:sync-collection>`
}

// syncRaw sends a sync-collection REPORT and returns the status and body, for
// the refusals the client reports only as an error.
func (a *davAccount) syncRaw(t *testing.T, calPath, token string) (int, string) {
	t.Helper()
	status, _, body := a.do(t, "REPORT", calPath, nil, davSyncBody(token))
	return status, body
}

// requireSyncRefused asserts the token is answered with 403 valid-sync-token.
func (a *davAccount) requireSyncRefused(t *testing.T, calPath, token, why string) {
	t.Helper()
	status, body := a.syncRaw(t, calPath, token)
	require.Equal(t, http.StatusForbidden, status, "%s: %s", why, body)
	require.Contains(t, body, "valid-sync-token", why)
}

// sync runs sync-collection through the client.
func (a *davAccount) sync(t *testing.T, calPath, token string) *caldav.SyncResponse {
	t.Helper()
	res, err := a.client.SyncCollection(context.Background(), calPath, &caldav.SyncQuery{SyncToken: token})
	require.NoError(t, err)
	return res
}

// objects lists every VEVENT resource of a calendar through calendar-query.
func (a *davAccount) objects(t *testing.T, calPath string) []caldav.CalendarObject {
	t.Helper()
	objs, err := a.client.QueryCalendar(context.Background(), calPath, &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{Name: "VCALENDAR", AllProps: true, AllComps: true},
		CompFilter:  caldav.CompFilter{Name: "VCALENDAR", Comps: []caldav.CompFilter{{Name: "VEVENT"}}},
	})
	require.NoError(t, err)
	return objs
}

// davPaths is the set of object paths in objs.
func davPaths(objs []caldav.CalendarObject) map[string]bool {
	out := make(map[string]bool, len(objs))
	for _, o := range objs {
		out[o.Path] = true
	}
	return out
}

// davObjectPath is where a series no client named is found.
func davObjectPath(calPath, eventID string) string {
	return calPath + "ndt-" + eventID + ".ics"
}

// davOwnCalendars lists the calendars a tenant already belongs to through the
// REST API, which resolves through the same membership rows as the DAV home.
func davOwnCalendars(t *testing.T, tt *helpers.TestTenant) []string {
	t.Helper()
	var list []calendarBody
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/calendars", tt.AccessToken, nil, &list)
	ids := make([]string, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	return ids
}
