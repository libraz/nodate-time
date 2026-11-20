package e2e

import (
	"encoding/json"
	"net/http"
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
// revision exists only for someone who can answer.
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

	var managerView map[string]any
	helpers.DoJSON(t, http.MethodGet, pollURL, owner.AccessToken, nil, &managerView)
	require.NotContains(t, managerView, "answerVersion", "a manager who is not a recipient has no answer revision")
	require.Contains(t, managerView, "reviewVersion")
	require.NotContains(t, managerView, "responseVersion")
	people, ok := managerView["members"].([]any)
	require.True(t, ok, "the manager sees the recipients")
	require.Len(t, people, 1)
	require.Equal(t, member.UserID, people[0].(map[string]any)["id"])

	var recipientView schedulePollBody
	helpers.DoJSON(t, http.MethodGet, pollURL, member.AccessToken, nil, &recipientView)
	require.NotNil(t, recipientView.AnswerVersion)
	var raw map[string]any
	helpers.DoJSON(t, http.MethodGet, pollURL, member.AccessToken, nil, &raw)
	require.Equal(t, float64(*recipientView.AnswerVersion), raw["answerVersion"])
	require.NotContains(t, raw, "members", "a recipient does not see who else was asked")
	require.Contains(t, raw, "reviewVersion")
}

// An address is shown to whoever administers the calendar and to its owner;
// for everyone else it is absent, not an empty string.
func TestMemberListOmitsOthersEmailForNonManagers(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	joinScheduleCalendar(t, owner, member)
	url := testServerURL + "/calendars/" + owner.CalendarID + "/members"

	var seen []map[string]any
	helpers.DoJSON(t, http.MethodGet, url, member.AccessToken, nil, &seen)
	require.Len(t, seen, 2)
	for _, entry := range seen {
		if entry["id"] == member.UserID {
			require.Equal(t, member.Email, entry["email"])
			continue
		}
		require.NotContains(t, entry, "email")
	}

	var managed []map[string]any
	helpers.DoJSON(t, http.MethodGet, url, owner.AccessToken, nil, &managed)
	for _, entry := range managed {
		require.NotEmpty(t, entry["email"])
	}
}
