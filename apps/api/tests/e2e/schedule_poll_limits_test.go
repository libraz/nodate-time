package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A limit refusal is a semantic refusal of a well-formed request, so it
// answers 422 and carries its own code.
func TestSchedulePollLimitsAnswerUnprocessable(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	joinScheduleCalendar(t, owner, member)
	baseURL := testServerURL + "/calendars/" + owner.CalendarID + "/schedule-polls"
	start := time.Now().UTC().AddDate(0, 0, 7).Truncate(time.Hour)

	post := func(body map[string]any) (int, string) {
		status, raw := helpers.DoJSONStatus(t, http.MethodPost, baseURL, owner.AccessToken, body)
		var failure struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(raw, &failure), string(raw))
		return status, failure.Code
	}

	t.Run("too many members", func(t *testing.T) {
		ids := make([]string, 501)
		for i := range ids {
			ids[i] = uuid.NewString()
		}
		status, code := post(map[string]any{
			"title": "Everyone", "timezone": "UTC", "memberIds": ids,
			"slots": []map[string]string{scheduleSlot(start)},
		})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Equal(t, "SCHEDULE_POLL.TOO_MANY_MEMBERS", code)
	})

	t.Run("too many slots", func(t *testing.T) {
		slots := make([]map[string]string, 257)
		for i := range slots {
			slots[i] = scheduleSlot(start.Add(time.Duration(i) * time.Hour))
		}
		status, code := post(map[string]any{
			"title": "Every hour", "timezone": "UTC", "memberIds": []string{member.UserID},
			"slots": slots,
		})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Equal(t, "SCHEDULE_POLL.TOO_MANY_SLOTS", code)
	})

	t.Run("too much history", func(t *testing.T) {
		_, err := testDB.Exec(`
			INSERT INTO calendar_schedule_polls (public_id, workspace_id, calendar_id, created_by_user_id, manager_user_id, title, timezone)
			WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 501)
			SELECT UUID_TO_BIN(UUID()), c.workspace_id, c.id, u.id, u.id, CONCAT('Backlog ', n.i), 'UTC'
			FROM n JOIN calendars c ON c.public_id = UUID_TO_BIN(?) JOIN users u ON u.public_id = UUID_TO_BIN(?)`,
			owner.CalendarID, owner.UserID)
		require.NoError(t, err)

		status, raw := helpers.DoJSONStatus(t, http.MethodGet, baseURL, owner.AccessToken, nil)
		require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
		var failure struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(raw, &failure))
		require.Equal(t, "SCHEDULE_POLL.HISTORY_LIMIT", failure.Code)
	})
}

// A field the caller may not see is absent rather than blank, and the answer
// resource exists only for someone who can answer.
func TestSchedulePollDetailOmitsWhatTheCallerMayNotSee(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	joinScheduleCalendar(t, owner, member)
	baseURL := testServerURL + "/calendars/" + owner.CalendarID + "/schedule-polls"
	start := time.Now().UTC().AddDate(0, 0, 7).Truncate(time.Hour)

	var poll schedulePollBody
	helpers.DoJSON(t, http.MethodPost, baseURL, owner.AccessToken, map[string]any{
		"title": "Who sees what", "timezone": "UTC", "memberIds": []string{member.UserID},
		"slots": []map[string]string{scheduleSlot(start)},
	}, &poll)
	pollURL := baseURL + "/" + poll.ID

	status, raw := helpers.DoJSONStatus(t, http.MethodGet, pollURL+"/response", owner.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status, "a manager who is not a recipient has no answer: %s", raw)

	var managerView map[string]any
	helpers.DoJSON(t, http.MethodGet, pollURL, owner.AccessToken, nil, &managerView)
	require.Contains(t, managerView, "etag")
	for _, gone := range []string{"version", "answerVersion", "reviewVersion", "responseVersion"} {
		require.NotContains(t, managerView, gone)
	}
	people, ok := managerView["members"].([]any)
	require.True(t, ok, "the manager sees the recipients")
	require.Len(t, people, 1)
	require.Equal(t, member.UserID, people[0].(map[string]any)["id"])

	var answer struct {
		ETag    string `json:"etag"`
		Answers []any  `json:"answers"`
	}
	status, raw, header := helpers.DoJSONFull(t, http.MethodGet, pollURL+"/response", member.AccessToken, nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &answer))
	require.NotEmpty(t, answer.ETag)
	require.Equal(t, answer.ETag, header.Get("ETag"))
	require.NotNil(t, answer.Answers)

	var raw2 map[string]any
	helpers.DoJSON(t, http.MethodGet, pollURL, member.AccessToken, nil, &raw2)
	require.NotContains(t, raw2, "members", "a recipient does not see who else was asked")
	require.Contains(t, raw2, "etag")
}

// Only the calendar's owner reads other members' addresses. A manager, like
// any other member, sees only their own, and the field is absent rather than
// an empty string -- in the member list and in the candidates for adding.
func TestMemberEmailsAreForTheOwnerOnly(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	manager := helpers.NewTenant(t, testServerURL)
	joinScheduleCalendar(t, owner, member)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinScheduleCalendar(t, owner, manager)
	helpers.DoJSON(t, http.MethodPut, calURL+"/members/"+manager.UserID+"/role", owner.AccessToken,
		map[string]any{"role": "manager"}, nil)

	for _, caller := range []*helpers.TestTenant{member, manager} {
		var seen []map[string]any
		helpers.DoJSON(t, http.MethodGet, calURL+"/members", caller.AccessToken, nil, &seen)
		require.Len(t, seen, 3)
		for _, entry := range seen {
			if entry["id"] == caller.UserID {
				require.Equal(t, caller.Email, entry["email"])
				continue
			}
			require.NotContains(t, entry, "email")
		}
	}

	var managed []map[string]any
	helpers.DoJSON(t, http.MethodGet, calURL+"/members", owner.AccessToken, nil, &managed)
	for _, entry := range managed {
		require.NotEmpty(t, entry["email"])
	}

	// A workspace colleague not on the calendar: found by address, never shown it.
	colleague := helpers.NewTenant(t, testServerURL)
	query := "/member-candidates?q=" + url.QueryEscape(colleague.Email)
	var byManager, byOwner struct {
		Items []map[string]any `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+query, manager.AccessToken, nil, &byManager)
	require.Len(t, byManager.Items, 1)
	require.NotContains(t, byManager.Items[0], "email")
	helpers.DoJSON(t, http.MethodGet, calURL+query, owner.AccessToken, nil, &byOwner)
	require.Len(t, byOwner.Items, 1)
	require.Equal(t, colleague.Email, byOwner.Items[0]["email"])
}
