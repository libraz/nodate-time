package e2e

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type concurrentPollRequest struct {
	method, url, token string
	body               any
}

// firePollRequestsTogether releases every request at once from a shared
// barrier. Each must succeed or be refused with a 409; a deadlock that outlives
// the transaction retries surfaces as a 500.
func firePollRequestsTogether(t *testing.T, requests []concurrentPollRequest) {
	t.Helper()
	release := make(chan struct{})
	statuses := make([]int, len(requests))
	bodies := make([]string, len(requests))
	var wg sync.WaitGroup
	for i, r := range requests {
		wg.Go(func() {
			<-release
			status, raw := helpers.DoJSONStatus(t, r.method, r.url, r.token, r.body)
			statuses[i], bodies[i] = status, string(raw)
		})
	}
	close(release)
	wg.Wait()
	for i, r := range requests {
		ok := statuses[i] >= 200 && statuses[i] < 300 || statuses[i] == http.StatusConflict
		require.True(t, ok, "%s %s: %d %s", r.method, r.url, statuses[i], bodies[i])
	}
}

// newPollCalendar returns an owner whose calendar has n further members. The
// members are fresh accounts with no events, so slot availability never moves.
func newPollCalendar(t *testing.T, n int) (*helpers.TestTenant, []*helpers.TestTenant) {
	t.Helper()
	owner := helpers.NewTenant(t, testServerURL)
	members := make([]*helpers.TestTenant, n)
	for i := range members {
		members[i] = helpers.NewTenant(t, testServerURL)
		joinScheduleCalendar(t, owner, members[i])
	}
	return owner, members
}

func pollMemberIDs(members []*helpers.TestTenant) []string {
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.UserID
	}
	return ids
}

func createConcurrencyPoll(t *testing.T, owner *helpers.TestTenant, members []*helpers.TestTenant, title string, start time.Time) schedulePollBody {
	t.Helper()
	var poll schedulePollBody
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+owner.CalendarID+"/schedule-polls", owner.AccessToken, map[string]any{
		"title": title, "timezone": "UTC", "memberIds": pollMemberIDs(members),
		"slots": []map[string]string{scheduleSlot(start), scheduleSlot(start.Add(2 * time.Hour))},
	}, &poll)
	require.Len(t, poll.Slots, 2)
	return poll
}

// answerRequests reads each recipient's ballot first, so only the answers
// themselves race.
func answerRequests(t *testing.T, owner *helpers.TestTenant, poll schedulePollBody, members []*helpers.TestTenant) []concurrentPollRequest {
	t.Helper()
	pollURL := testServerURL + "/calendars/" + owner.CalendarID + "/schedule-polls/" + poll.ID
	requests := make([]concurrentPollRequest, 0, len(members))
	for _, m := range members {
		var ballot schedulePollBody
		helpers.DoJSON(t, http.MethodGet, pollURL, m.AccessToken, nil, &ballot)
		require.True(t, ballot.CanRespond)
		requests = append(requests, concurrentPollRequest{
			method: http.MethodPut, url: pollURL + "/response", token: m.AccessToken,
			body: map[string]any{
				"version": ballot.Version, "answerVersion": *ballot.AnswerVersion,
				"answers": []map[string]string{
					{"slotId": ballot.Slots[0].ID, "value": "yes"},
					{"slotId": ballot.Slots[1].ID, "value": "maybe"},
				},
			},
		})
	}
	return requests
}

// TestSchedulePollConcurrency fires poll writes that lock the same member rows
// at the same moment.
func TestSchedulePollConcurrency(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	start := time.Now().UTC().AddDate(0, 0, 9).Truncate(time.Hour)

	t.Run("recipients answer one poll", func(t *testing.T) {
		owner, members := newPollCalendar(t, 6)
		poll := createConcurrencyPoll(t, owner, members, "Concurrent answers", start)
		firePollRequestsTogether(t, answerRequests(t, owner, poll, members))
	})

	t.Run("two polls share recipients", func(t *testing.T) {
		owner, members := newPollCalendar(t, 4)
		first := createConcurrencyPoll(t, owner, members, "Shared recipients one", start)
		second := createConcurrencyPoll(t, owner, members, "Shared recipients two", start)
		requests := answerRequests(t, owner, first, members)
		requests = append(requests, answerRequests(t, owner, second, members)...)
		firePollRequestsTogether(t, requests)
	})

	t.Run("two creates in one calendar", func(t *testing.T) {
		owner, members := newPollCalendar(t, 4)
		body := map[string]any{
			"title": "Concurrent create", "timezone": "UTC", "memberIds": pollMemberIDs(members),
			"slots": []map[string]string{scheduleSlot(start)},
		}
		url := testServerURL + "/calendars/" + owner.CalendarID + "/schedule-polls"
		firePollRequestsTogether(t, []concurrentPollRequest{
			{method: http.MethodPost, url: url, token: owner.AccessToken, body: body},
			{method: http.MethodPost, url: url, token: owner.AccessToken, body: body},
		})
	})
}
