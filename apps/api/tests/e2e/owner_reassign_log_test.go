package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// ownerReassignRows returns the "owner changed" entries recorded for an event,
// with the public id of the member who caused each.
func ownerReassignRows(t *testing.T, eventID string) (actors []string) {
	t.Helper()
	rows, err := testDB.QueryContext(t.Context(), `
		SELECT BIN_TO_UUID(u.public_id), e.payload_json
		  FROM events e JOIN users u ON u.id = e.actor_user_id
		 WHERE e.type = 'calendar.event.updated'
		   AND JSON_UNQUOTE(JSON_EXTRACT(e.payload_json, '$.eventId')) = ?
		 ORDER BY e.id`, eventID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var actor string
		var raw []byte
		require.NoError(t, rows.Scan(&actor, &raw))
		var payload struct {
			Changes []string `json:"changes"`
		}
		require.NoError(t, json.Unmarshal(raw, &payload))
		for _, c := range payload.Changes {
			if c == "owner" {
				actors = append(actors, actor)
			}
		}
	}
	require.NoError(t, rows.Err())
	return actors
}

// Taking a member off a calendar hands their events to the longest-standing
// owner, and each event that changed hands says so in its own history, once,
// under the name of whoever removed them.
func TestRemovingAMemberLogsEachOwnerChange(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	leaver := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, leaver, "editor")

	create := func(token, title, ownerID string, rule map[string]any) string {
		t.Helper()
		body := map[string]any{
			"title": title, "allDay": false,
			"startAt": "2026-04-03T10:00:00Z", "endAt": "2026-04-03T11:00:00Z",
			"ownerId": ownerID,
		}
		if rule != nil {
			body["recurrenceRule"] = rule
		}
		var out struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", token, body, &out)
		require.NotEmpty(t, out.ID)
		return out.ID
	}
	single := create(leaver.AccessToken, "Leaver single", leaver.UserID, nil)
	series := create(leaver.AccessToken, "Leaver weekly", leaver.UserID,
		map[string]any{"freq": "weekly", "interval": 1, "count": 4})
	kept := create(owner.AccessToken, "Owner's own", owner.UserID, nil)

	// The leaver's second occurrence becomes a row of its own, which moves with
	// the series and is not an event to log a second time.
	var listed struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30&tz=UTC",
		leaver.AccessToken, nil, &listed)
	var second string
	for _, e := range listed.Items {
		if e.Title == "Leaver weekly" {
			second = e.ID
		}
	}
	require.NotEmpty(t, second)

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/members/"+leaver.UserID, owner.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))

	for _, id := range []string{single, series} {
		require.Equal(t, []string{owner.UserID}, ownerReassignRows(t, id),
			"one entry, by the member who removed them")
		var e struct {
			OwnerID string `json:"ownerId"`
		}
		helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, owner.AccessToken, nil, &e)
		require.Equal(t, owner.UserID, e.OwnerID)
	}
	require.Empty(t, ownerReassignRows(t, kept), "an event that did not change hands logs nothing")
}

// A member who leaves of their own accord is the one the entries name.
func TestLeavingLogsTheLeaverAsTheActor(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	leaver := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, leaver, "editor")

	var created struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", leaver.AccessToken, map[string]any{
		"title": "Mine", "allDay": false,
		"startAt": "2026-04-03T10:00:00Z", "endAt": "2026-04-03T11:00:00Z",
		"ownerId": leaver.UserID,
	}, &created)

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/members/"+leaver.UserID, leaver.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	require.Equal(t, []string{leaver.UserID}, ownerReassignRows(t, created.ID))
}
