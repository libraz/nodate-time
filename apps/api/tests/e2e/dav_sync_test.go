package e2e

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// davCreate creates a timed event and returns its id.
func davCreate(t *testing.T, calURL, token string, body map[string]any) string {
	t.Helper()
	full := map[string]any{
		"allDay": false, "timezone": "Asia/Tokyo",
		"startAt": "2026-06-05T10:00:00+09:00", "endAt": "2026-06-05T11:00:00+09:00",
	}
	for k, v := range body {
		full[k] = v
	}
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", token, full, &evt)
	require.NotEmpty(t, evt.ID)
	return evt.ID
}

// davTokenWith rewrites one colon-separated field of a sync token.
func davTokenWith(token string, field int, value string) string {
	parts := strings.Split(token, ":")
	parts[field] = value
	return strings.Join(parts, ":")
}

// Field positions in urn:nodate-time:sync:<calendar>:<logID>:<day>:<rv>.
const (
	davTokenCalendar = 3
	davTokenLogID    = 4
	davTokenDay      = 5
	davTokenVersion  = 6
)

// TestDAVSyncReportsEachChange walks one calendar through every kind of change
// a client must hear about, and checks after each that the change is reported
// and that nothing outside what the test touched is.
func TestDAVSyncReportsEachChange(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, guest, "editor")
	a := newDAVAccount(t, owner)
	calPath := a.calendar(owner.CalendarID)

	first := davCreate(t, calURL, owner.AccessToken, map[string]any{"title": "first"})
	known := map[string]bool{davObjectPath(calPath, first): true}

	initial := a.sync(t, calPath, "")
	require.Equal(t, known, davPaths(initial.Updated))
	require.Empty(t, initial.Deleted)
	token := initial.SyncToken

	// The property and the report hand out the same token while nothing changes.
	require.Equal(t, token, a.propfindSyncToken(t, calPath))
	require.Equal(t, token, a.propfindSyncToken(t, calPath))

	step := func(what string, updated, deleted []string) {
		t.Helper()
		res := a.sync(t, calPath, token)
		got := davPaths(res.Updated)
		gone := map[string]bool{}
		for _, p := range res.Deleted {
			gone[p] = true
		}
		for _, p := range updated {
			require.True(t, got[p], "%s: %s not reported as updated (%v)", what, p, got)
		}
		for _, p := range deleted {
			require.True(t, gone[p], "%s: %s not reported as deleted (%v)", what, p, gone)
		}
		for p := range got {
			require.True(t, known[p], "%s: unexpected update %s", what, p)
		}
		for p := range gone {
			require.True(t, known[p], "%s: unexpected deletion %s", what, p)
		}
		require.NotEmpty(t, res.SyncToken)
		token = res.SyncToken
	}

	second := davCreate(t, calURL, owner.AccessToken, map[string]any{"title": "second"})
	known[davObjectPath(calPath, second)] = true
	step("create", []string{davObjectPath(calPath, second)}, nil)

	doAnyVersion(t, http.MethodPatch, calURL+"/events/"+first, owner.AccessToken,
		map[string]any{"title": "first, renamed"}, nil)
	step("update", []string{davObjectPath(calPath, first)}, nil)

	series := davCreate(t, calURL, owner.AccessToken, map[string]any{
		"title":          "weekly",
		"recurrenceRule": map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"FR"}, "until": "2026-06-30T23:59:59+09:00"},
	})
	seriesPath := davObjectPath(calPath, series)
	known[seriesPath] = true
	step("create series", []string{seriesPath}, nil)

	doAnyVersion(t, http.MethodPatch, calURL+"/events/"+series+"_20260612?scope=this", owner.AccessToken,
		map[string]any{"title": "weekly, moved", "startAt": "2026-06-12T15:00:00+09:00", "endAt": "2026-06-12T16:00:00+09:00"}, nil)
	step("create override", []string{seriesPath}, nil)

	doAnyVersion(t, http.MethodPatch, calURL+"/events/"+series+"_20260612?scope=this", owner.AccessToken,
		map[string]any{"title": "weekly, moved again"}, nil)
	step("update override", []string{seriesPath}, nil)

	status, raw := sendAnyVersion(t, http.MethodDelete, calURL+"/events/"+series+"_20260619?scope=this", owner.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	step("cancel occurrence", []string{seriesPath}, nil)

	status, raw = sendAnyVersion(t, http.MethodDelete, calURL+"/events/"+series+"/exceptions/2026-06-19", owner.AccessToken, nil)
	require.True(t, status >= 200 && status < 300, "undo cancellation: %d %s", status, raw)
	step("undo cancellation", []string{seriesPath}, nil)

	doAnyVersion(t, http.MethodPatch, calURL+"/events/"+second, owner.AccessToken,
		map[string]any{"participantIds": []string{guest.UserID}}, nil)
	step("add attendee", []string{davObjectPath(calPath, second)}, nil)

	doAnyVersion(t, http.MethodPatch, calURL+"/events/"+second, owner.AccessToken,
		map[string]any{"participantIds": []string{}}, nil)
	step("remove attendee", []string{davObjectPath(calPath, second)}, nil)

	status, raw = sendAnyVersion(t, http.MethodDelete, calURL+"/events/"+first, owner.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	step("delete", nil, []string{davObjectPath(calPath, first)})
}

// TestDAVSyncHidesWhatTheReaderNeverSaw verifies that a change to a
// confidential event neither forces a non-party into a full resync nor names
// the event to them, and that its deletion is not reported to them either.
func TestDAVSyncHidesWhatTheReaderNeverSaw(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, member, "editor")
	m := newDAVAccount(t, member)
	calPath := m.calendar(owner.CalendarID)

	hidden := davCreate(t, calURL, owner.AccessToken, map[string]any{"title": "hidden", "visibility": "confidential"})
	shown := davCreate(t, calURL, owner.AccessToken, map[string]any{"title": "shown"})
	hiddenPath := davObjectPath(calPath, hidden)

	initial := m.sync(t, calPath, "")
	require.Equal(t, map[string]bool{davObjectPath(calPath, shown): true}, davPaths(initial.Updated))

	doAnyVersion(t, http.MethodPatch, calURL+"/events/"+hidden, owner.AccessToken, map[string]any{"title": "hidden, renamed"}, nil)
	res := m.sync(t, calPath, initial.SyncToken)
	require.False(t, davPaths(res.Updated)[hiddenPath])
	require.NotContains(t, res.Deleted, hiddenPath)

	status, raw := sendAnyVersion(t, http.MethodDelete, calURL+"/events/"+hidden, owner.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	status, raw = sendAnyVersion(t, http.MethodDelete, calURL+"/events/"+shown, owner.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	res = m.sync(t, calPath, res.SyncToken)
	require.Equal(t, []string{davObjectPath(calPath, shown)}, res.Deleted)
	require.Empty(t, res.Updated)
}

// TestDAVSyncRefusesTokensItCannotServe verifies each way a token stops being
// good, and that a token fetched afresh after a refusal is accepted even when
// the row that caused the refusal is the calendar's latest.
func TestDAVSyncRefusesTokensItCannotServe(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	reader := helpers.NewTenant(t, testServerURL)
	bystander := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, reader, "editor")
	joinAs(t, calURL, owner.AccessToken, bystander, "editor")
	davCreate(t, calURL, owner.AccessToken, map[string]any{"title": "one"})
	a := newDAVAccount(t, owner)
	calPath := a.calendar(owner.CalendarID)
	token := a.sync(t, calPath, "").SyncToken

	logID, err := strconv.ParseUint(strings.Split(token, ":")[davTokenLogID], 10, 64)
	require.NoError(t, err)
	day, err := strconv.ParseInt(strings.Split(token, ":")[davTokenDay], 10, 64)
	require.NoError(t, err)

	var other struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars", owner.AccessToken,
		map[string]any{"name": "other", "color": helpers.TenantCalendarColor}, &other)

	for why, bad := range map[string]string{
		"malformed":        "urn:nodate-time:sync:nonsense",
		"foreign scheme":   "http://example.com/token",
		"another calendar": davTokenWith(token, davTokenCalendar, other.ID),
		"a future row":     davTokenWith(token, davTokenLogID, strconv.FormatUint(logID+1_000_000, 10)),
		"yesterday":        davTokenWith(token, davTokenDay, strconv.FormatInt(day-1, 10)),
		"another renderer": davTokenWith(token, davTokenVersion, "0"),
	} {
		a.requireSyncRefused(t, calPath, bad, why)
	}
	// The token itself is still good.
	a.sync(t, calPath, token)

	doAnyVersion(t, http.MethodPatch, calURL, owner.AccessToken, map[string]any{"name": "renamed"}, nil)
	a.requireSyncRefused(t, calPath, token, "calendar settings changed")
	fresh := a.sync(t, calPath, "").SyncToken
	a.sync(t, calPath, fresh)

	// Someone else's role change is not the reader's business; their own is.
	r := newDAVAccount(t, reader)
	readerToken := r.sync(t, r.calendar(owner.CalendarID), "").SyncToken
	require.Less(t, promote(t, calURL, owner.AccessToken, bystander, "viewer"), 300)
	readerToken = r.sync(t, r.calendar(owner.CalendarID), readerToken).SyncToken
	require.Less(t, promote(t, calURL, owner.AccessToken, reader, "viewer"), 300)
	r.requireSyncRefused(t, r.calendar(owner.CalendarID), readerToken, "own role changed")
	r.sync(t, r.calendar(owner.CalendarID), r.sync(t, r.calendar(owner.CalendarID), "").SyncToken)
}

// TestDAVSyncRefusesATokenTooFarBehind verifies that more than 5,000 new log
// rows behind a token forces a full resync rather than a replay.
func TestDAVSyncRefusesATokenTooFarBehind(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	a := newDAVAccount(t, owner)
	calPath := a.calendar(owner.CalendarID)
	token := a.sync(t, calPath, "").SyncToken

	ws := helpers.TestWorkspace(generated.New(testDB))
	calID := internalCalendarID(t, owner.CalendarID)
	const rows, batch = 5001, 500
	for done := 0; done < rows; done += batch {
		n := min(batch, rows-done)
		var values []string
		var args []any
		for i := 0; i < n; i++ {
			pub, subject := uuid.New(), uuid.New()
			values = append(values, "(?, ?, ?, ?, 'calendar.event.updated', JSON_OBJECT('eventId', ?, 'title', 'seed'), NOW(3))")
			args = append(args, pub[:], ws.ID, calID, subject[:], subject.String())
		}
		_, err := testDB.Exec(
			`INSERT INTO events (public_id, workspace_id, calendar_id, subject_public_id, type, payload_json, occurred_at) VALUES `+
				strings.Join(values, ","), args...)
		require.NoError(t, err)
	}
	a.requireSyncRefused(t, calPath, token, fmt.Sprintf("%d rows behind", rows))
}

// TestDAVSyncHonoursTheLimit verifies that a sync that cannot fit the
// client's limit is answered 507 rather than truncated.
func TestDAVSyncHonoursTheLimit(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	davCreate(t, calURL, owner.AccessToken, map[string]any{"title": "one"})
	davCreate(t, calURL, owner.AccessToken, map[string]any{"title": "two"})
	a := newDAVAccount(t, owner)

	body := `<?xml version="1.0" encoding="utf-8"?>` +
		`<d:sync-collection xmlns:d="DAV:"><d:sync-token/><d:sync-level>1</d:sync-level>` +
		`<d:limit><d:nresults>1</d:nresults></d:limit><d:prop><d:getetag/></d:prop></d:sync-collection>`
	status, _, resp := a.do(t, "REPORT", a.calendar(owner.CalendarID), nil, body)
	require.Equal(t, http.StatusInsufficientStorage, status, resp)
	require.Contains(t, resp, "number-of-matches-within-limits")
}
