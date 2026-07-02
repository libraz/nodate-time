package e2e

import (
	"net/http"
	"sync"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestUpdateCalendarIsAdminOnly verifies that only admins can update calendar
// settings; members and viewers are rejected (audit H-2).
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
		map[string]any{"role": "member"}, &inv)
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
// via the iCal import endpoint (audit H-1).
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

// TestInviteCannotGrantAdmin verifies invite links may not grant the admin role
// (audit H-11).
func TestInviteCannotGrantAdmin(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "admin"})
	// The admin role is rejected at the schema layer (enum: member,viewer), which
	// Huma reports as 422; a 400 from the handler is equally acceptable. Either way
	// an invite must never be able to grant admin.
	require.True(t, status == http.StatusBadRequest || status == http.StatusUnprocessableEntity,
		"expected admin role to be rejected, got %d", status)
}

// TestSingleUseInviteCannotBeReused verifies the atomic use-count guard: a
// max_uses=1 invite admits exactly one new member (audit H-10).
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
		map[string]any{"role": "member", "maxUses": 1}, &inv)

	// First user consumes the single use.
	firstStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", first.AccessToken, nil)
	require.True(t, firstStatus >= 200 && firstStatus < 300)

	// Second distinct user is rejected — the invite is exhausted.
	secondStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", second.AccessToken, nil)
	require.True(t, secondStatus == 404 || secondStatus == 410, "expected exhausted invite to be rejected, got %d", secondStatus)
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
		map[string]any{"role": "member", "maxUses": 1}, &inv)

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
// invite succeeds without burning a use (audit H-12).
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
		map[string]any{"role": "member", "maxUses": 2}, &inv)

	s1, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", member.AccessToken, nil)
	require.True(t, s1 >= 200 && s1 < 300)
	s2, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", member.AccessToken, nil)
	require.True(t, s2 >= 200 && s2 < 300, "re-accept by existing member should be idempotent, got %d", s2)
}

// TestUpdateEventRejectsInvalidDates verifies UpdateEvent no longer silently
// writes a zero timestamp on a malformed date (audit H-8).
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
			"title":   "Broken",
			"allDay":  false,
			"startAt": "not-a-date",
			"endAt":   "2026-05-12T10:00:00+09:00",
		})
	require.Equal(t, 400, status)
}

// TestCreateEventRejectsInvalidRecurrence verifies unknown recurrence freq is
// rejected rather than producing an invisible event (audit H-7).
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

// TestAssignedToMustBeMember verifies an event assignee must be a calendar
// member, and that a valid assignee round-trips (audit M-20, H-9 sibling).
func TestAssignedToMustBeMember(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	outsider := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	// Assigning a non-member is rejected.
	badStatus, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":      "Assigned to outsider",
			"allDay":     false,
			"startAt":    "2026-05-12T09:00:00+09:00",
			"endAt":      "2026-05-12T10:00:00+09:00",
			"assignedTo": outsider.UserID,
		})
	require.Equal(t, 400, badStatus)

	// Assigning the owner (a member) round-trips.
	var evt struct {
		AssignedTo *string `json:"assignedTo"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":      "Assigned to owner",
			"allDay":     false,
			"startAt":    "2026-05-12T09:00:00+09:00",
			"endAt":      "2026-05-12T10:00:00+09:00",
			"assignedTo": owner.UserID,
		}, &evt)
	require.NotNil(t, evt.AssignedTo)
	require.Equal(t, owner.UserID, *evt.AssignedTo)
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
		map[string]any{"filename": "active.svg", "contentType": "image/svg+xml", "byteSize": 128})
	require.Equal(t, http.StatusBadRequest, status)
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
		ID    uint32 `json:"id"`
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "member"}, &inv)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+inv.Token+"/accept", guest.AccessToken, nil, nil)
	helpers.DoJSON(t, http.MethodPut, calURL+"/members/"+guest.UserID+"/role", owner.AccessToken,
		map[string]any{"role": "admin"}, nil)
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/invites/"+uintToStr(inv.ID), owner.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	type activityFeedItem struct {
		EntityType string `json:"entityType"`
		Action     string `json:"action"`
		ID         uint64 `json:"id"`
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
		if key == "member:role_change" {
			require.Contains(t, item.Summary, "admin")
			require.NotNil(t, item.Actor)
			require.Equal(t, owner.UserID, item.Actor.ID)
		}
	}
	require.True(t, seen["invite:create"], "invite creation must be audited")
	require.True(t, seen["member:join"], "invite acceptance must be audited")
	require.True(t, seen["member:role_change"], "member role changes must be audited")
	require.True(t, seen["invite:revoke"], "invite revocation must be audited")

	var firstPage activityPage
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity?limit=2", owner.AccessToken, nil, &firstPage)
	require.Len(t, firstPage.Items, 2)
	require.NotEmpty(t, firstPage.NextCursor)

	var secondPage activityPage
	helpers.DoJSON(t, http.MethodGet, calURL+"/activity?limit=2&cursor="+firstPage.NextCursor, owner.AccessToken, nil, &secondPage)
	require.NotEmpty(t, secondPage.Items)
	ids := map[uint64]bool{}
	for _, item := range firstPage.Items {
		ids[item.ID] = true
	}
	for _, item := range secondPage.Items {
		require.False(t, ids[item.ID], "activity cursor must not repeat items across pages")
	}
}

// TestAttachmentDownloadIsTenantScoped verifies the cross-tenant attachment IDOR
// is closed: a foreign attachment id cannot be downloaded through another
// calendar/event path (audit C-1). Requires object storage.
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
		map[string]any{"filename": "contract.pdf", "contentType": "application/pdf", "byteSize": 1024}, &att)
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
