package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestUpdateCalendarIsAdminOnly verifies that only admins can update calendar
// settings; members and viewers are rejected.
func TestUpdateCalendarIsAdminOnly(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", member.AccessToken, nil, nil)

	// Member (non-admin) cannot rename the calendar.
	memberStatus, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL, member.AccessToken,
		map[string]any{"name": "Hijacked", "color": "#000000"})
	require.Equal(t, 403, memberStatus)

	// Owner (admin) can.
	ownerStatus, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL, owner.AccessToken,
		map[string]any{"name": "Renamed", "color": "#123456"})
	require.True(t, ownerStatus >= 200 && ownerStatus < 300)
}

// TestViewerCannotImportICal verifies a read-only viewer cannot inject events
// via the iCal import endpoint.
func TestViewerCannotImportICal(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	viewer := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", viewer.AccessToken, nil, nil)

	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:x@test\r\nDTSTART:20260101T090000Z\r\nDTEND:20260101T100000Z\r\nSUMMARY:Injected\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/import", viewer.AccessToken,
		map[string]any{"ics": ics})
	require.Equal(t, 403, status)
}

// TestInviteCannotGrantAdmin verifies invite links may not grant the admin role.
func TestInviteCannotGrantAdmin(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "owner"})
	// The owner role is rejected at the schema layer (enum: editor,viewer),
	// which Huma reports as 422; a 400 from the handler is equally acceptable.
	// Either way an invite must never be able to grant ownership.
	require.True(t, status == http.StatusBadRequest || status == http.StatusUnprocessableEntity,
		"expected admin role to be rejected, got %d", status)
}

// TestSingleUseInviteCannotBeReused verifies the atomic use-count guard: a
// max_uses=1 invite admits exactly one new member.
func TestSingleUseInviteCannotBeReused(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	first := helpers.NewTenant(t, testServerURL)
	second := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor", "maxUses": 1}, &inv)

	// First user consumes the single use.
	firstStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", first.AccessToken, nil)
	require.True(t, firstStatus >= 200 && firstStatus < 300)

	// Second distinct user is rejected — the invite is exhausted.
	secondStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", second.AccessToken, nil)
	require.True(t, secondStatus == 404 || secondStatus == 410, "expected exhausted invite to be rejected, got %d", secondStatus)
}

// TestDeleteInviteRejectsUnknownIDAndDoesNotAudit verifies that deleting an id
// that names no invite is reported as a 404 rather than a silent success, and
// that no revoke is recorded for a delete that did not happen.
//
// The id here is malformed rather than merely absent, which is a different
// rejection: it never reaches the lookup. The id that is well-formed and
// belongs to somebody else is the case below.
func TestDeleteInviteRejectsUnknownIDAndDoesNotAudit(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// A real invite, so the feed below has something of this kind to record.
	var inv struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor"}, &inv)
	require.NotEmpty(t, inv.ID)

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/invites/999999999", owner.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)

	type activityFeedItem struct {
		EntityType string `json:"entityType"`
		Action     string `json:"action"`
	}
	type activityPage struct {
		Items []activityFeedItem `json:"items"`
	}
	var feed activityPage
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity?limit=20", owner.AccessToken, nil, &feed)

	seen := map[string]bool{}
	for _, item := range feed.Items {
		seen[item.EntityType+":"+item.Action] = true
	}
	// Asserting the absence of a revoke over a feed that might be empty proves
	// nothing -- an activity endpoint that returned no rows at all would read
	// as a clean result. Finding the creation is what makes the silence about
	// revokes mean something.
	require.True(t, seen["invite:calendar.invite.created"],
		"the feed must be recording invite events for its silence about revokes to count")
	require.False(t, seen["invite:calendar.invite.revoked"],
		"a no-op delete must not be audited as a revoke")
}

// TestDeleteInviteRejectsAnInviteFromAnotherCalendar covers the case the test
// above names in passing and never sends: an id that is well-formed and
// belongs to somebody else.
//
// The scoping is real today -- the handler resolves the calendar and revokes
// by public id *and* calendar. What was missing is anything that would notice
// if it stopped being. The generated data layer still carries an unscoped
// RevokeInvite, held out of use by a comment in the drift script's allow-list
// saying the authorization would have to be repeated outside the query. This
// is that comment turned into something that fails.
func TestDeleteInviteRejectsAnInviteFromAnotherCalendar(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	outsider := helpers.NewTenant(t, testServerURL)
	joiner := helpers.NewTenant(t, testServerURL)

	ownerCal := testServerURL + "/calendars/" + owner.CalendarID
	var inv struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, ownerCal+"/invites", owner.AccessToken,
		map[string]any{"role": "editor", "maxUses": 2}, &inv)
	require.NotEmpty(t, inv.ID)
	require.NotEmpty(t, inv.Token)

	// The outsider manages a calendar of their own, so the request is refused
	// on the invite belonging elsewhere rather than on their rights.
	outsiderCal := testServerURL + "/calendars/" + outsider.CalendarID
	status, body := helpers.DoJSONStatus(t, http.MethodDelete,
		outsiderCal+"/invites/"+inv.ID, outsider.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status,
		"an invite on another calendar must not be reachable: %s", string(body))

	// The status on its own would pass against a handler that revoked the
	// invite and then reported a miss, which is exactly what dropping the
	// calendar from the query would produce. Using the link is what does not.
	acceptStatus, acceptBody := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/invites/"+inv.Token+"/accept", joiner.AccessToken, nil)
	require.True(t, acceptStatus >= 200 && acceptStatus < 300,
		"the refused delete must leave the invite usable: %s", string(acceptBody))
}

// TestSingleUseInviteConcurrentAccept verifies the invite use-count guard under
// actual concurrent accepts: exactly one distinct user can consume maxUses=1.
func TestSingleUseInviteConcurrentAccept(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	first := helpers.NewTenant(t, testServerURL)
	second := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor", "maxUses": 1}, &inv)

	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, token := range []string{first.AccessToken, second.AccessToken} {
		wg.Add(1)
		go func(accessToken string) {
			defer wg.Done()
			<-start
			status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", accessToken, nil)
			statuses <- status
		}(token)
	}
	close(start)
	wg.Wait()
	close(statuses)

	successes := 0
	for status := range statuses {
		if status >= 200 && status < 300 {
			successes++
			continue
		}
		require.True(t, status == http.StatusNotFound || status == http.StatusGone, "unexpected concurrent accept status %d", status)
	}
	require.Equal(t, 1, successes)
}

// TestReacceptInviteIsIdempotent verifies that an existing member re-accepting an
// invite succeeds without burning a use.
func TestReacceptInviteIsIdempotent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor", "maxUses": 2}, &inv)

	s1, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", member.AccessToken, nil)
	require.True(t, s1 >= 200 && s1 < 300)
	s2, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", member.AccessToken, nil)
	require.True(t, s2 >= 200 && s2 < 300, "re-accept by existing member should be idempotent, got %d", s2)
}

// TestUpdateEventRejectsInvalidDates verifies UpdateEvent no longer silently
// writes a zero timestamp on a malformed date.
func TestUpdateEventRejectsInvalidDates(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "Valid",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		}, &evt)

	status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID, owner.AccessToken,
		map[string]any{
			"title":              "Broken",
			"allDay":             false,
			"startAt":            "not-a-date",
			"endAt":              "2026-05-12T10:00:00+09:00",
			"location":           "",
			"memo":               "",
			"url":                "",
			"notificationOffset": nil,
			"participants":       []string{},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		})
	require.Equal(t, 400, status)
}

// TestCreateEventRejectsInvalidRecurrence verifies unknown recurrence freq is
// rejected rather than producing an invisible event.
func TestCreateEventRejectsInvalidRecurrence(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":          "Bad recurrence",
			"allDay":         false,
			"startAt":        "2026-05-12T09:00:00+09:00",
			"endAt":          "2026-05-12T10:00:00+09:00",
			"recurrenceRule": map[string]any{"freq": "Daily", "interval": 1},
		})
	require.Equal(t, 400, status)
}

// TestOwnerIDMustBeMember verifies an event assignee must be a calendar
// member, and that a valid assignee round-trips.
func TestOwnerIDMustBeMember(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	outsider := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// Assigning a non-member is rejected.
	badStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "Assigned to outsider",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
			"ownerId": outsider.UserID,
		})
	require.Equal(t, 400, badStatus)

	// Assigning the owner (a member) round-trips.
	var evt struct {
		OwnerID *string `json:"ownerId"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "Assigned to owner",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
			"ownerId": owner.UserID,
		}, &evt)
	require.NotNil(t, evt.OwnerID)
	require.Equal(t, owner.UserID, *evt.OwnerID)
}

// TestEventHistoryIsCalendarScoped verifies an event audit history request
// cannot read another calendar's audit log by guessing its event id.
func TestEventHistoryIsCalendarScoped(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	victim := helpers.NewTenant(t, testServerURL)
	attacker := helpers.NewTenant(t, testServerURL)

	victimCal := testServerURL + "/calendars/" + victim.CalendarID
	var victimEvent struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, victimCal+"/events", victim.AccessToken,
		map[string]any{
			"title":   "Private audited event",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		}, &victimEvent)
	require.NotEmpty(t, victimEvent.ID)

	attackerCal := testServerURL + "/calendars/" + attacker.CalendarID
	var history []struct {
		ID      uint64 `json:"id"`
		Summary string `json:"summary"`
	}
	helpers.DoJSON(t, http.MethodGet,
		attackerCal+"/events/"+victimEvent.ID+"/history", attacker.AccessToken, nil, &history)
	require.Empty(t, history, "foreign event audit history must not be returned through another calendar")
}

// TestEventHistoryIsNewestFirst verifies the per-entity history endpoint
// returns the most recent changes first: with no cursor and a fixed page
// size, an ascending order would truncate to the oldest entries and hide
// everything since.
func TestEventHistoryIsNewestFirst(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":   "History order v1",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		}, &evt)
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID, owner.AccessToken,
		map[string]any{
			"title":              "History order v2",
			"allDay":             false,
			"startAt":            "2026-05-12T09:00:00+09:00",
			"endAt":              "2026-05-12T10:00:00+09:00",
			"location":           "",
			"memo":               "",
			"url":                "",
			"notificationOffset": nil,
			"participants":       []string{},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		}, nil)

	var history []struct {
		Action  string `json:"action"`
		Summary string `json:"summary"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID+"/history", owner.AccessToken, nil, &history)
	require.Len(t, history, 2)
	require.Equal(t, "calendar.event.updated", history[0].Action, "the most recent change must come first")
	require.Contains(t, history[0].Summary, "v2")
	require.Equal(t, "calendar.event.created", history[1].Action)
}

// TestAdminRouteRejectsNonAdminWithJSONError verifies a non-admin's request
// to an admin-only route is rejected with a proper JSON Content-Type (not
// text/plain from a bare http.Error call) and the expected error code.
func TestAdminRouteRejectsNonAdminWithJSONError(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	req, err := http.NewRequest(http.MethodGet, testServerURL+"/admin/oauth-providers", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+tt.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "AUTH.ADMIN_REQUIRED", body.Code)
}

// TestEventHistoryAcceptsCompositeRecurrenceID verifies the audit history
// endpoint works for a recurring instance's composite id ("uuid_YYYYMMDD"),
// matching the sibling sub-resource endpoints (checklist, attachments) that
// already resolve through the parent series.
func TestEventHistoryAcceptsCompositeRecurrenceID(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	evts := createWeeklyFriday(t, calURL, owner.AccessToken)
	target := evts[1] // composite id, e.g. "<parent-uuid>_20260410"
	require.Contains(t, target.ID, "_")
	parentID, _, _ := strings.Cut(target.ID, "_")

	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+target.ID+"?scope=this", owner.AccessToken,
		map[string]any{
			"title":              "Composite history edit",
			"allDay":             false,
			"startAt":            "2026-04-10T18:00:00+09:00",
			"endAt":              "2026-04-10T19:00:00+09:00",
			"location":           "",
			"memo":               "",
			"url":                "",
			"notificationOffset": nil,
			"participants":       []string{},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		}, nil)

	var byComposite []struct {
		Action  string `json:"action"`
		Summary string `json:"summary"`
	}
	status, _ := helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+target.ID+"/history", owner.AccessToken, nil)
	require.Equal(t, 200, status)
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+target.ID+"/history", owner.AccessToken, nil, &byComposite)
	require.NotEmpty(t, byComposite)
	require.Equal(t, "calendar.event.updated", byComposite[0].Action)

	// The composite id resolves to the same parent series, so its history
	// must match fetching by the plain parent id.
	var byParent []struct {
		Action  string `json:"action"`
		Summary string `json:"summary"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+parentID+"/history", owner.AccessToken, nil, &byParent)
	require.Equal(t, byParent, byComposite)
}

func TestAttachmentPresignRejectsSVG(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "Attachment host",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		}, &evt)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events/"+evt.ID+"/attachments/presign", tt.AccessToken,
		map[string]any{"filename": "active.svg", "contentType": "image/svg+xml", "byteSize": 128, "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"})
	require.Equal(t, http.StatusBadRequest, status)
}

// TestAttachmentPresignedPutRejectsMismatchedContentLength verifies the
// presigned PUT rejects a body whose length disagrees with the byteSize bound
// into its signature, so an upload cannot exceed the size the caller checked
// against the per-attachment limit before ever being presigned.
func TestAttachmentPresignedPutRejectsMismatchedContentLength(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "Attachment host",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		}, &evt)

	body := []byte("%PDF-1.4 fake contract body")
	var pres struct {
		AttachmentID string `json:"attachmentId"`
		UploadURL    string `json:"uploadUrl"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/attachments/presign", tt.AccessToken,
		map[string]any{"filename": "contract.pdf", "contentType": "application/pdf", "byteSize": len(body) - 1, "sha256": helpers.SHA256Hex(body)}, &pres)

	status, _ := helpers.UploadToPresignedURLStatus(t, pres.UploadURL, "application/pdf", body)
	require.True(t, status >= 400, "expected the signed Content-Length mismatch to be rejected, got %d", status)
}

// TestAttachmentConfirmRejectsMismatchedObjectAndDeletesIt verifies the
// Confirm-time defense-in-depth: an object at the presigned key that
// disagrees with the declared size is rejected and removed rather than left
// as an orphan, and the pending row itself is deleted (not just left disabled).
func TestAttachmentConfirmRejectsMismatchedObjectAndDeletesIt(t *testing.T) {
	bootstrap(t)
	requireStorage(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "Attachment host",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		}, &evt)

	body := []byte("%PDF-1.4 fake contract body")
	var pres struct {
		AttachmentID string `json:"attachmentId"`
		UploadURL    string `json:"uploadUrl"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/attachments/presign", tt.AccessToken,
		map[string]any{"filename": "contract.pdf", "contentType": "application/pdf", "byteSize": len(body) - 1, "sha256": helpers.SHA256Hex(body)}, &pres)

	storageClient := getTestStorage()
	require.NotNil(t, storageClient)
	// Reconstruct the key PresignUpload built, bypassing the (now
	// size-enforcing) presigned URL to place a mismatched object directly.
	storageKey := helpers.AttachmentStorageKey(testWorkspacePublicID, helpers.SHA256Hex(body))
	helpers.PutRawObject(t, getTestBucket(), storageKey, "application/pdf", body)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+evt.ID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusBadRequest, status)

	// The blob is deliberately left alone. It is content-addressed, so the
	// same bytes may already back a correctly confirmed attachment
	// elsewhere in the workspace, and deleting it here would break that
	// one. Nothing points at it from this reservation any more, so the
	// unreferenced-object sweep is what collects it.
	_, exists, err := storageClient.StatObject(testCtx(), storageKey)
	require.NoError(t, err)
	require.True(t, exists, "a shared blob must survive one reservation failing")

	var objectRefs int
	require.NoError(t, testDB.QueryRow(
		"SELECT ref_count FROM storage_objects WHERE storage_key = ?", storageKey,
	).Scan(&objectRefs))
	require.Zero(t, objectRefs, "a failed confirm must not take a reference on the blob")

	status, _ = helpers.DoJSONStatus(t, http.MethodPost,
		calURL+"/events/"+evt.ID+"/attachments/"+pres.AttachmentID+"/confirm", tt.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status, "the pending row should have been deleted, not just disabled")
}

func TestViewerListMembersHidesOtherEmails(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	viewer := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", viewer.AccessToken, nil, nil)

	var members []struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/members", viewer.AccessToken, nil, &members)
	require.Len(t, members, 2)

	emails := map[string]string{}
	for _, m := range members {
		emails[m.ID] = m.Email
	}
	require.Empty(t, emails[owner.UserID], "viewer must not see another member's email")
	require.Equal(t, viewer.Email, emails[viewer.UserID], "viewer may see their own email")
}

func TestMemberAndInviteChangesAppearInActivity(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var inv struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", guest.AccessToken, nil, nil)
	helpers.DoJSON(t, http.MethodPut, calURL+"/members/"+guest.UserID+"/role", owner.AccessToken,
		map[string]any{"role": "owner"}, nil)
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/invites/"+inv.ID, owner.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	type activityFeedItem struct {
		EntityType string `json:"entityType"`
		Action     string `json:"action"`
		ID         string `json:"id"`
		Summary    string `json:"summary"`
		Actor      *struct {
			ID string `json:"id"`
		} `json:"actor"`
	}
	type activityPage struct {
		Items      []activityFeedItem `json:"items"`
		NextCursor string             `json:"nextCursor"`
	}
	var feed activityPage
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity?limit=20", owner.AccessToken, nil, &feed)

	seen := map[string]bool{}
	for _, item := range feed.Items {
		key := item.EntityType + ":" + item.Action
		seen[key] = true
		if key == "member:calendar.member.role_changed" {
			require.Contains(t, item.Summary, "owner")
			require.NotNil(t, item.Actor)
			require.Equal(t, owner.UserID, item.Actor.ID)
		}
	}
	require.True(t, seen["invite:calendar.invite.created"], "invite creation must be recorded")
	require.True(t, seen["member:calendar.member.joined"], "invite acceptance must be recorded")
	require.True(t, seen["member:calendar.member.role_changed"], "member role changes must be recorded")
	require.True(t, seen["invite:calendar.invite.revoked"], "invite revocation must be recorded")

	var firstPage activityPage
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity?limit=2", owner.AccessToken, nil, &firstPage)
	require.Len(t, firstPage.Items, 2)
	require.NotEmpty(t, firstPage.NextCursor)

	var secondPage activityPage
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity?limit=2&cursor="+firstPage.NextCursor, owner.AccessToken, nil, &secondPage)
	require.NotEmpty(t, secondPage.Items)
	ids := map[string]bool{}
	for _, item := range firstPage.Items {
		ids[item.ID] = true
	}
	for _, item := range secondPage.Items {
		require.False(t, ids[item.ID], "activity cursor must not repeat items across pages")
	}
}

// TestAttachmentDownloadIsTenantScoped verifies the cross-tenant attachment IDOR
// is closed: a foreign attachment id cannot be downloaded through another
// calendar/event path. Requires object storage.
func TestAttachmentDownloadIsTenantScoped(t *testing.T) {
	bootstrap(t)
	if testStorage == nil {
		t.Skip("object storage not configured; skipping attachment IDOR test")
	}
	t.Parallel()

	victim := helpers.NewTenant(t, testServerURL)
	attacker := helpers.NewTenant(t, testServerURL)

	// Victim creates an event and an attachment on it.
	victimCal := testServerURL + "/calendars/" + victim.CalendarID
	var vEvt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, victimCal+"/events", victim.AccessToken,
		map[string]any{
			"title":   "Private",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		}, &vEvt)
	var att struct {
		AttachmentID string `json:"attachmentId"`
	}
	helpers.DoJSON(t, http.MethodPost, victimCal+"/events/"+vEvt.ID+"/attachments/presign", victim.AccessToken,
		map[string]any{"filename": "contract.pdf", "contentType": "application/pdf", "byteSize": 1024, "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}, &att)
	require.NotEmpty(t, att.AttachmentID)

	// Attacker creates their own event and tries to download the victim's
	// attachment id through their own calendar/event path.
	attackerCal := testServerURL + "/calendars/" + attacker.CalendarID
	var aEvt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, attackerCal+"/events", attacker.AccessToken,
		map[string]any{
			"title":   "Decoy",
			"allDay":  false,
			"startAt": "2026-05-12T09:00:00+09:00",
			"endAt":   "2026-05-12T10:00:00+09:00",
		}, &aEvt)

	status, _ := helpers.DoJSONStatus(t, http.MethodGet,
		attackerCal+"/events/"+aEvt.ID+"/attachments/"+att.AttachmentID+"/download", attacker.AccessToken, nil)
	require.Equal(t, 404, status, "cross-tenant attachment download must be rejected")
}
