package e2e

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type schedulePollBody struct {
	ID               string `json:"id"`
	ETag             string `json:"etag"`
	Status           string `json:"status"`
	ConfirmedEventID string `json:"confirmedEventId"`
	Version          int    `json:"version"`
	AnswerVersion    *int   `json:"answerVersion"`
	ReviewVersion    int    `json:"reviewVersion"`
	CanManage        bool   `json:"canManage"`
	CanRespond       bool   `json:"canRespond"`
	RespondedCount   int    `json:"respondedCount"`
	MyAnswers        []struct {
		SlotID string `json:"slotId"`
		Value  string `json:"value"`
	} `json:"myAnswers"`
	Responses []struct {
		MemberID string `json:"memberId"`
	} `json:"responses"`
	Slots []struct {
		ID     string `json:"id"`
		Active bool   `json:"active"`
	} `json:"slots"`
}

func joinScheduleCalendar(t *testing.T, owner, member *helpers.TestTenant) {
	t.Helper()
	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+owner.CalendarID+"/invites", owner.AccessToken,
		map[string]any{"role": "viewer", "maxUses": 1}, &invite)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", member.AccessToken, nil, nil)
}

// putPollResponse saves the caller's answers under the answer tag read just
// before, and returns the status and raw body.
func putPollResponse(t *testing.T, pollURL, token string, answers any) (int, []byte) {
	t.Helper()
	status, _, header := helpers.DoJSONFull(t, http.MethodGet, pollURL+"/response", token, nil, nil)
	require.Equal(t, http.StatusOK, status)
	return helpers.DoJSONStatusWithHeaders(t, http.MethodPut, pollURL+"/response", token,
		map[string]any{"answers": answers}, map[string]string{"If-Match": header.Get("ETag")})
}

// doIfMatch sends a conditional write, requires a 2xx answer and decodes it.
func doIfMatch(t *testing.T, method, url, token, etag string, body, out any) {
	t.Helper()
	status, raw, _ := helpers.DoJSONFull(t, method, url, token, body, map[string]string{"If-Match": etag})
	require.True(t, status >= 200 && status < 300, "%s %s: %d %s", method, url, status, raw)
	if out != nil {
		require.NoError(t, json.Unmarshal(raw, out))
	}
}

func scheduleSlot(start time.Time) map[string]string {
	return map[string]string{
		"startAt": start.Format(time.RFC3339),
		"endAt":   start.Add(time.Hour).Format(time.RFC3339),
	}
}

func TestSchedulePollLifecycle(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	respondent := helpers.NewTenant(t, testServerURL)
	secondRespondent := helpers.NewTenant(t, testServerURL)
	outsider := helpers.NewTenant(t, testServerURL)
	joinScheduleCalendar(t, owner, respondent)
	joinScheduleCalendar(t, owner, secondRespondent)
	joinScheduleCalendar(t, owner, outsider)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	baseURL := calURL + "/schedule-polls"
	start := time.Now().UTC().AddDate(0, 0, 7).Truncate(time.Hour)
	var poll schedulePollBody
	helpers.DoJSON(t, http.MethodPost, baseURL, owner.AccessToken, map[string]any{
		"title": "Studio booking", "timezone": "UTC", "memberIds": []string{respondent.UserID, secondRespondent.UserID},
		"slots": []map[string]string{scheduleSlot(start), scheduleSlot(start.Add(24 * time.Hour))},
	}, &poll)
	require.True(t, poll.CanManage)
	require.Len(t, poll.Slots, 2)
	pollURL := baseURL + "/" + poll.ID

	code, raw := helpers.DoJSONStatus(t, http.MethodGet, pollURL, outsider.AccessToken, nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, code)
	require.NotContains(t, string(raw), "Studio booking")

	// A fresh password session must find the same unanswered request.
	var login struct{ Token string }
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/login", "", map[string]string{
		"email": respondent.Email, "password": respondent.Password,
	}, &login)
	require.NotEmpty(t, login.Token)
	type action struct{ PollID, Kind string }
	assertAction := func(token string, expected ...action) {
		t.Helper()
		var body []action
		helpers.DoJSON(t, http.MethodGet, testServerURL+"/user/schedule-poll-actions", token, nil, &body)
		require.ElementsMatch(t, expected, body)
	}
	assertAction(login.Token, action{PollID: poll.ID, Kind: "answer"})
	var recipient schedulePollBody
	helpers.DoJSON(t, http.MethodGet, pollURL, login.Token, nil, &recipient)
	require.True(t, recipient.CanRespond)
	require.False(t, recipient.CanManage)
	require.Empty(t, recipient.Responses)

	type savedResponse struct {
		ETag    string `json:"etag"`
		Answers []struct{ SlotID, Value string }
	}
	save := func(etag string, answers ...map[string]string) savedResponse {
		t.Helper()
		var saved savedResponse
		doIfMatch(t, http.MethodPut, pollURL+"/response", login.Token, etag, map[string]any{"answers": answers}, &saved)
		return saved
	}
	var initial savedResponse
	helpers.DoJSON(t, http.MethodGet, pollURL+"/response", login.Token, nil, &initial)
	require.Empty(t, initial.Answers)
	yes := map[string]string{"slotId": poll.Slots[0].ID, "value": "yes"}
	no := map[string]string{"slotId": poll.Slots[1].ID, "value": "no"}
	partial := save(initial.ETag, yes)
	require.Len(t, partial.Answers, 1)
	code, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPut, pollURL+"/response", login.Token,
		map[string]any{"answers": []map[string]string{{"slotId": poll.Slots[0].ID, "value": "no"}}},
		ifMatch(initial.ETag))
	require.Equal(t, http.StatusPreconditionFailed, code, string(raw))
	require.Contains(t, string(raw), "SCHEDULE_POLL.RESPONSE_STALE")
	assertAction(login.Token, action{PollID: poll.ID, Kind: "answer"})
	assertAction(owner.AccessToken)
	complete := save(partial.ETag, yes, no)
	require.Len(t, complete.Answers, 2)
	assertAction(login.Token)
	assertAction(owner.AccessToken)
	status, raw := putPollResponse(t, pollURL, secondRespondent.AccessToken,
		[]map[string]string{yes, {"slotId": poll.Slots[1].ID, "value": "yes"}})
	require.Equal(t, http.StatusOK, status, string(raw))
	assertAction(owner.AccessToken, action{PollID: poll.ID, Kind: "finalize"})

	helpers.DoJSON(t, http.MethodGet, pollURL, owner.AccessToken, nil, &poll)
	require.Equal(t, 2, poll.RespondedCount)
	require.Len(t, poll.Responses, 2)
	require.ElementsMatch(t, []string{respondent.UserID, secondRespondent.UserID},
		[]string{poll.Responses[0].MemberID, poll.Responses[1].MemberID})
	body := map[string]any{"slotId": poll.Slots[0].ID, "ownerId": respondent.UserID}
	wrongSlot := map[string]any{"slotId": poll.Slots[1].ID, "ownerId": respondent.UserID}
	code, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPost, pollURL+"/finalize", owner.AccessToken, wrongSlot, ifMatch(poll.ETag))
	require.GreaterOrEqual(t, code, 400, string(raw))
	require.Less(t, code, 500, string(raw))

	// Two sessions can send the same finalization while the first is committing.
	var parallel sync.WaitGroup
	type result struct {
		status int
		body   []byte
	}
	results := make(chan result, 2)
	for range 2 {
		parallel.Go(func() {
			status, body := helpers.DoJSONStatusWithHeaders(t, http.MethodPost, pollURL+"/finalize", owner.AccessToken, body, ifMatch(poll.ETag))
			results <- result{status: status, body: body}
		})
	}
	parallel.Wait()
	close(results)
	var confirmedID string
	for response := range results {
		require.True(t, response.status >= 200 && response.status < 300, "%d: %s", response.status, response.body)
		var confirmed schedulePollBody
		require.NoError(t, json.Unmarshal(response.body, &confirmed))
		require.NotEmpty(t, confirmed.ConfirmedEventID)
		require.Equal(t, "finalized", confirmed.Status)
		if confirmedID == "" {
			confirmedID = confirmed.ConfirmedEventID
		}
		require.Equal(t, confirmedID, confirmed.ConfirmedEventID)
	}
	assertAction(owner.AccessToken)

	var created struct {
		ID, Title, Visibility, OwnerID string
		ParticipantIDs                 []string
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+confirmedID, secondRespondent.AccessToken, nil, &created)
	require.Equal(t, confirmedID, created.ID)
	require.Equal(t, "Studio booking", created.Title)
	require.Equal(t, "confidential", created.Visibility)
	require.Equal(t, respondent.UserID, created.OwnerID)
	require.ElementsMatch(t, []string{respondent.UserID, secondRespondent.UserID}, created.ParticipantIDs)
	var eventCount int
	require.NoError(t, testDB.QueryRow(`SELECT COUNT(*) FROM calendar_events e JOIN calendars c ON c.id=e.calendar_id
		WHERE c.public_id=UUID_TO_BIN(?) AND e.title=?`, owner.CalendarID, created.Title).Scan(&eventCount))
	require.Equal(t, 1, eventCount)
	require.NoError(t, testDB.QueryRow(`SELECT COUNT(*) FROM events
		WHERE subject_public_id=UUID_TO_BIN(?) AND type='calendar.event.created'`, created.ID).Scan(&eventCount))
	require.Equal(t, 1, eventCount)
}

func TestSchedulePollChecksOtherCalendarsAndNewConflicts(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	organizer := helpers.NewTenant(t, testServerURL)
	sourceOwner := helpers.NewTenant(t, testServerURL)
	participant := helpers.NewTenant(t, testServerURL)
	joinScheduleCalendar(t, organizer, participant)
	joinScheduleCalendar(t, sourceOwner, participant)

	calURL := testServerURL + "/calendars/" + organizer.CalendarID
	sourceURL := testServerURL + "/calendars/" + sourceOwner.CalendarID
	pollsURL := calURL + "/schedule-polls"
	start := time.Now().UTC().AddDate(0, 0, 7).Truncate(time.Hour)
	soft := start.Add(2 * time.Hour)
	usable := soft.Add(2 * time.Hour)
	addEvent := func(when time.Time, flexibility string) {
		t.Helper()
		body := map[string]any{
			"title":          "Private source event",
			"allDay":         false,
			"timezone":       "UTC",
			"startAt":        scheduleSlot(when)["startAt"],
			"endAt":          scheduleSlot(when)["endAt"],
			"participantIds": []string{participant.UserID},
			"flexibility":    flexibility,
			"visibility":     "confidential",
		}
		helpers.DoJSON(t, http.MethodPost, sourceURL+"/events", sourceOwner.AccessToken, body, nil)
	}
	addEvent(start, "fixed")
	addEvent(soft, "negotiable")
	preview := func() map[time.Time]string {
		t.Helper()
		var result []struct {
			StartAt      time.Time
			Availability string
		}
		status, raw := helpers.DoJSONStatus(t, http.MethodPost, pollsURL+"/availability", organizer.AccessToken, map[string]any{
			"timezone": "UTC", "memberIds": []string{participant.UserID},
			"fromDate": start.Format(time.DateOnly), "toDate": usable.Format(time.DateOnly),
			"startTime": "09:00", "endTime": "18:00", "durationMinutes": 60,
			"slots": []map[string]string{scheduleSlot(start), scheduleSlot(soft), scheduleSlot(usable)},
		})
		require.Equal(t, http.StatusOK, status, string(raw))
		require.NotContains(t, string(raw), "Private source event")
		require.NoError(t, json.Unmarshal(raw, &result))
		slots := make(map[time.Time]string, len(result))
		for _, slot := range result {
			slots[slot.StartAt.UTC()] = slot.Availability
		}
		return slots
	}
	require.Equal(t, "open", preview()[start])
	helpers.DoJSON(t, http.MethodPut, sourceURL+"/schedule-polls/availability-sharing", sourceOwner.AccessToken,
		map[string]bool{"enabled": true}, nil)
	require.Equal(t, map[time.Time]string{soft: "soft", usable: "open"}, preview())

	var poll schedulePollBody
	helpers.DoJSON(t, http.MethodPost, pollsURL, organizer.AccessToken, map[string]any{
		"title": "Check the latest calendar", "timezone": "UTC", "memberIds": []string{participant.UserID},
		"slots": []map[string]string{scheduleSlot(soft), scheduleSlot(usable)},
	}, &poll)
	require.Len(t, poll.Slots, 2)
	pollURL := pollsURL + "/" + poll.ID
	status, raw := putPollResponse(t, pollURL, participant.AccessToken, []map[string]string{
		{"slotId": poll.Slots[0].ID, "value": "yes"},
		{"slotId": poll.Slots[1].ID, "value": "yes"},
	})
	require.Equal(t, http.StatusOK, status, string(raw))
	helpers.DoJSON(t, http.MethodGet, pollURL, organizer.AccessToken, nil, &poll)
	addEvent(usable, "fixed")
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPost, pollURL+"/finalize", organizer.AccessToken,
		map[string]any{"slotId": poll.Slots[1].ID, "ownerId": participant.UserID}, ifMatch(poll.ETag))
	require.Equal(t, http.StatusConflict, status, string(raw))
	helpers.DoJSON(t, http.MethodGet, pollURL, organizer.AccessToken, nil, &poll)
	require.Equal(t, "open", poll.Status)
	require.Empty(t, poll.ConfirmedEventID)
	require.False(t, poll.Slots[1].Active)
	require.Equal(t, 1, poll.RespondedCount)

	softChoice := map[string]any{"slotId": poll.Slots[0].ID, "ownerId": participant.UserID}
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPost, pollURL+"/finalize", organizer.AccessToken, softChoice, ifMatch(poll.ETag))
	require.Equal(t, http.StatusConflict, status, string(raw))
	softChoice["acknowledged"] = true

	status, raw = putPollResponse(t, pollURL, participant.AccessToken,
		[]map[string]string{{"slotId": poll.Slots[0].ID, "value": "maybe"}})
	require.Equal(t, http.StatusOK, status, string(raw))
	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPost, pollURL+"/finalize", organizer.AccessToken, softChoice, ifMatch(poll.ETag))
	require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	require.Contains(t, string(raw), "SCHEDULE_POLL.STALE")
	helpers.DoJSON(t, http.MethodGet, pollURL, organizer.AccessToken, nil, &poll)
	doIfMatch(t, http.MethodPost, pollURL+"/finalize", organizer.AccessToken, poll.ETag, softChoice, &poll)
	require.Equal(t, "finalized", poll.Status)
	require.NotEmpty(t, poll.ConfirmedEventID)
}

func TestSchedulePollParallelConfirmationsOnlyBookOnePoll(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	joinScheduleCalendar(t, owner, member)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	pollsURL := calURL + "/schedule-polls"
	start := time.Now().UTC().AddDate(0, 0, 8).Truncate(time.Hour)
	titles := []string{"First concurrent poll", "Second concurrent poll"}
	polls := make([]schedulePollBody, len(titles))
	for i, title := range titles {
		helpers.DoJSON(t, http.MethodPost, pollsURL, owner.AccessToken, map[string]any{
			"title": title, "timezone": "UTC", "memberIds": []string{member.UserID},
			"slots": []map[string]string{scheduleSlot(start), scheduleSlot(start.Add(2 * time.Hour))},
		}, &polls[i])
		require.Len(t, polls[i].Slots, 2)
		pollURL := pollsURL + "/" + polls[i].ID
		status, raw := putPollResponse(t, pollURL, member.AccessToken, []map[string]string{
			{"slotId": polls[i].Slots[0].ID, "value": "yes"},
			{"slotId": polls[i].Slots[1].ID, "value": "no"},
		})
		require.Equal(t, http.StatusOK, status, string(raw))
		helpers.DoJSON(t, http.MethodGet, pollURL, owner.AccessToken, nil, &polls[i])
	}
	var parallel sync.WaitGroup
	codes := make(chan int, len(polls))
	for _, poll := range polls {
		parallel.Go(func() {
			status, body := helpers.DoJSONStatusWithHeaders(t, http.MethodPost, pollsURL+"/"+poll.ID+"/finalize", owner.AccessToken,
				map[string]any{"slotId": poll.Slots[0].ID, "ownerId": member.UserID}, ifMatch(poll.ETag))
			t.Logf("finalizing %s: %d %s", poll.ID, status, body)
			codes <- status
		})
	}
	parallel.Wait()
	close(codes)
	var statuses []int
	for code := range codes {
		statuses = append(statuses, code)
	}
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, statuses)
	var count int
	require.NoError(t, testDB.QueryRow(`SELECT COUNT(*) FROM calendar_events e
		JOIN calendars c ON c.id = e.calendar_id WHERE c.public_id = UUID_TO_BIN(?) AND e.title IN (?, ?)`,
		owner.CalendarID, titles[0], titles[1]).Scan(&count))
	require.Equal(t, 1, count)
}

// TestSchedulePollFinalizesWithoutPendingAnswers verifies that a manager can
// close a poll while some recipients have not answered; a "no" from someone
// who did answer still blocks that slot.
func TestSchedulePollFinalizesWithoutPendingAnswers(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	answered := helpers.NewTenant(t, testServerURL)
	silent := helpers.NewTenant(t, testServerURL)
	joinScheduleCalendar(t, owner, answered)
	joinScheduleCalendar(t, owner, silent)
	baseURL := testServerURL + "/calendars/" + owner.CalendarID + "/schedule-polls"
	start := time.Now().UTC().AddDate(0, 0, 9).Truncate(time.Hour)
	var poll schedulePollBody
	helpers.DoJSON(t, http.MethodPost, baseURL, owner.AccessToken, map[string]any{
		"title": "Rehearsal", "timezone": "UTC", "memberIds": []string{answered.UserID, silent.UserID},
		"slots": []map[string]string{scheduleSlot(start), scheduleSlot(start.Add(24 * time.Hour))},
	}, &poll)
	pollURL := baseURL + "/" + poll.ID

	status, raw := putPollResponse(t, pollURL, answered.AccessToken, []map[string]string{
		{"slotId": poll.Slots[0].ID, "value": "yes"},
		{"slotId": poll.Slots[1].ID, "value": "no"},
	})
	require.Equal(t, http.StatusOK, status, string(raw))
	helpers.DoJSON(t, http.MethodGet, pollURL, owner.AccessToken, nil, &poll)
	require.Equal(t, 1, poll.RespondedCount)

	status, raw = helpers.DoJSONStatusWithHeaders(t, http.MethodPost, pollURL+"/finalize", owner.AccessToken,
		map[string]any{"slotId": poll.Slots[1].ID, "ownerId": answered.UserID}, ifMatch(poll.ETag))
	require.Equal(t, http.StatusConflict, status, string(raw))

	doIfMatch(t, http.MethodPost, pollURL+"/finalize", owner.AccessToken, poll.ETag,
		map[string]any{"slotId": poll.Slots[0].ID, "ownerId": answered.UserID}, &poll)
	require.Equal(t, "finalized", poll.Status)
	require.NotEmpty(t, poll.ConfirmedEventID)
}
