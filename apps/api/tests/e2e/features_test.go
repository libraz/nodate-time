package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// currentTag reads the entity tag a conditional write to url must name.
func currentTag(t *testing.T, url, token string) string {
	t.Helper()
	status, raw, header := helpers.DoJSONFull(t, http.MethodGet, url, token, nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NotEmpty(t, header.Get("ETag"), "GET %s carries no ETag", url)
	return header.Get("ETag")
}

// patchNow merges body into the resource at url against its current version.
func patchNow(t *testing.T, url, token string, body any) (int, []byte) {
	t.Helper()
	tagged, _, _ := strings.Cut(url, "?")
	return helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, url, token, body,
		ifMatch(currentTag(t, tagged, token)))
}

// doTagged sends a write naming tag as the version it replaces, requires a
// 2xx answer and decodes it into out when out is set.
func doTagged(t *testing.T, method, url, token, tag string, body, out any) {
	t.Helper()
	status, raw := helpers.DoJSONStatusWithHeaders(t, method, url, token, body, ifMatch(tag))
	require.True(t, status >= 200 && status < 300, "%s %s: %d %s", method, url, status, raw)
	if out != nil && len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, out))
	}
}

// taggedStatus is doTagged that hands back the status instead of requiring success.
func taggedStatus(t *testing.T, method, url, token, tag string, body any) int {
	t.Helper()
	status, _ := helpers.DoJSONStatusWithHeaders(t, method, url, token, body, ifMatch(tag))
	return status
}

// patchNowOK is patchNow that requires success and decodes the answer into out.
func patchNowOK(t *testing.T, url, token string, body, out any) {
	t.Helper()
	status, raw := patchNow(t, url, token, body)
	require.Equal(t, http.StatusOK, status, string(raw))
	if out != nil {
		require.NoError(t, json.Unmarshal(raw, out))
	}
}

func TestEventURLField(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create event with URL
	var evt struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "Meeting with link",
			"allDay":  false,
			"startAt": "2026-04-20T15:00:00+09:00",
			"endAt":   "2026-04-20T16:00:00+09:00",
			"url":     "https://meet.example.com/abc",
		}, &evt)
	require.NotEmpty(t, evt.ID)
	assert.Equal(t, "https://meet.example.com/abc", evt.URL)

	// Get event, verify URL
	var got struct {
		URL string `json:"url"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, tt.AccessToken, nil, &got)
	assert.Equal(t, "https://meet.example.com/abc", got.URL)

	// Update event with new URL
	var updated struct {
		URL string `json:"url"`
	}
	patchNowOK(t, calURL+"/events/"+evt.ID, tt.AccessToken,
		map[string]any{
			"title":              "Meeting with link",
			"allDay":             false,
			"startAt":            "2026-04-20T15:00:00+09:00",
			"endAt":              "2026-04-20T16:00:00+09:00",
			"location":           "",
			"memo":               "",
			"url":                "https://zoom.us/j/123",
			"notificationOffset": nil,
			"participantIds":     []string{},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		}, &updated)
	assert.Equal(t, "https://zoom.us/j/123", updated.URL)

	// Get again, verify new URL
	var got2 struct {
		URL string `json:"url"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, tt.AccessToken, nil, &got2)
	assert.Equal(t, "https://zoom.us/j/123", got2.URL)
}

func TestEventNotificationOffset(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create event with notificationOffset
	var evt struct {
		ID                 string `json:"id"`
		NotificationOffset *int   `json:"notificationOffset"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":              "Reminder event",
			"allDay":             false,
			"startAt":            "2026-04-21T10:00:00+09:00",
			"endAt":              "2026-04-21T11:00:00+09:00",
			"notificationOffset": 30,
		}, &evt)
	require.NotEmpty(t, evt.ID)
	require.NotNil(t, evt.NotificationOffset)
	assert.Equal(t, 30, *evt.NotificationOffset)

	// Get event, verify notificationOffset
	var got struct {
		NotificationOffset *int `json:"notificationOffset"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, tt.AccessToken, nil, &got)
	require.NotNil(t, got.NotificationOffset)
	assert.Equal(t, 30, *got.NotificationOffset)

	// Update with null notificationOffset to clear it
	var updated struct {
		NotificationOffset *int `json:"notificationOffset"`
	}
	patchNowOK(t, calURL+"/events/"+evt.ID, tt.AccessToken,
		map[string]any{
			"title":              "Reminder event",
			"allDay":             false,
			"startAt":            "2026-04-21T10:00:00+09:00",
			"endAt":              "2026-04-21T11:00:00+09:00",
			"location":           "",
			"memo":               "",
			"url":                "",
			"notificationOffset": nil,
			"participantIds":     []string{},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		}, &updated)
	assert.Nil(t, updated.NotificationOffset)

	// Get again, verify notificationOffset is nil
	var got2 struct {
		NotificationOffset *int `json:"notificationOffset"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, tt.AccessToken, nil, &got2)
	assert.Nil(t, got2.NotificationOffset)
}

func TestEventParticipants(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt1 := helpers.NewTenant(t, testServerURL)
	tt2 := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt1.CalendarID

	// tt1 creates an invite link
	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt1.AccessToken,
		map[string]any{"role": "editor"}, &invite)

	// tt2 accepts the invite
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", tt2.AccessToken,
		nil, nil)

	// tt1 creates an event with tt2 as participant
	var evt struct {
		ID           string   `json:"id"`
		Participants []string `json:"participantIds"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt1.AccessToken,
		map[string]any{
			"title":          "Team lunch",
			"allDay":         false,
			"startAt":        "2026-04-22T12:00:00+09:00",
			"endAt":          "2026-04-22T13:00:00+09:00",
			"participantIds": []string{tt2.UserID},
		}, &evt)
	require.NotEmpty(t, evt.ID)
	require.Len(t, evt.Participants, 1)
	assert.Equal(t, tt2.UserID, evt.Participants[0])

	// Get the event, verify participants
	var got struct {
		Participants []string `json:"participantIds"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, tt1.AccessToken, nil, &got)
	require.Len(t, got.Participants, 1)
	assert.Equal(t, tt2.UserID, got.Participants[0])

	// Update event with empty participants
	var updated struct {
		Participants []string `json:"participantIds"`
	}
	patchNowOK(t, calURL+"/events/"+evt.ID, tt1.AccessToken,
		map[string]any{
			"title":              "Team lunch",
			"allDay":             false,
			"startAt":            "2026-04-22T12:00:00+09:00",
			"endAt":              "2026-04-22T13:00:00+09:00",
			"location":           "",
			"memo":               "",
			"url":                "",
			"notificationOffset": nil,
			"participantIds":     []string{},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		}, &updated)
	assert.Empty(t, updated.Participants)

	// Get again, verify participants is empty
	var got2 struct {
		Participants []string `json:"participantIds"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, tt1.AccessToken, nil, &got2)
	assert.Empty(t, got2.Participants)
}

func TestInvalidEventParticipantDoesNotPartiallyMutateEvent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", owner.AccessToken,
		map[string]any{"role": "editor"}, &invite)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", guest.AccessToken, nil, nil)

	var event struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":          "Original title",
			"allDay":         false,
			"startAt":        "2026-05-01T12:00:00+09:00",
			"endAt":          "2026-05-01T13:00:00+09:00",
			"participantIds": []string{guest.UserID},
		}, &event)

	status, _ := patchNow(t, calURL+"/events/"+event.ID, owner.AccessToken,
		map[string]any{
			"title":              "Must not persist",
			"allDay":             false,
			"startAt":            "2026-05-01T12:00:00+09:00",
			"endAt":              "2026-05-01T13:00:00+09:00",
			"location":           "",
			"memo":               "",
			"url":                "",
			"notificationOffset": nil,
			"participantIds":     []string{"not-a-uuid"},
			"ownerId":            nil,
			"recurrenceRule":     nil,
		})
	require.Equal(t, http.StatusBadRequest, status)

	var got struct {
		Title        string   `json:"title"`
		Participants []string `json:"participantIds"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+event.ID, owner.AccessToken, nil, &got)
	assert.Equal(t, "Original title", got.Title)
	assert.Equal(t, []string{guest.UserID}, got.Participants)

	status, _ = helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":          "Must not be created",
			"allDay":         false,
			"startAt":        "2026-05-01T14:00:00+09:00",
			"endAt":          "2026-05-01T15:00:00+09:00",
			"participantIds": []string{"not-a-uuid"},
		})
	assert.Equal(t, http.StatusBadRequest, status)
}

func TestChecklistCRUD(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create event
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "Party planning",
			"allDay":  false,
			"startAt": "2026-04-25T18:00:00+09:00",
			"endAt":   "2026-04-25T21:00:00+09:00",
		}, &evt)
	require.NotEmpty(t, evt.ID)

	checklistURL := calURL + "/events/" + evt.ID + "/checklist"

	// Create first checklist item
	var item1 struct {
		ID    string `json:"id"`
		Etag  string `json:"etag"`
		Title string `json:"title"`
		Done  bool   `json:"done"`
	}
	helpers.DoJSON(t, http.MethodPost, checklistURL, tt.AccessToken,
		map[string]any{"title": "Buy supplies"}, &item1)
	require.NotEmpty(t, item1.ID)
	assert.Equal(t, "Buy supplies", item1.Title)
	assert.False(t, item1.Done)

	// List checklist items, verify 1 item
	var items1 []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Done  bool   `json:"done"`
	}
	helpers.DoJSON(t, http.MethodGet, checklistURL, tt.AccessToken, nil, &items1)
	require.Len(t, items1, 1)

	// Create second checklist item
	var item2 struct {
		ID   string `json:"id"`
		Etag string `json:"etag"`
	}
	helpers.DoJSON(t, http.MethodPost, checklistURL, tt.AccessToken,
		map[string]any{"title": "Send invitations"}, &item2)
	require.NotEmpty(t, item2.ID)

	// List, verify 2 items
	var items2 []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet, checklistURL, tt.AccessToken, nil, &items2)
	require.Len(t, items2, 2)

	// Update first item: set done to true
	var updatedItem struct {
		ID   string `json:"id"`
		Done bool   `json:"done"`
	}
	require.NotEmpty(t, item1.Etag)
	doTagged(t, http.MethodPatch, checklistURL+"/"+item1.ID, tt.AccessToken, item1.Etag,
		map[string]any{"title": "Buy supplies", "done": true}, &updatedItem)
	assert.True(t, updatedItem.Done)

	// Verify via list that first item is done
	var itemsCheck []struct {
		ID   string `json:"id"`
		Done bool   `json:"done"`
	}
	helpers.DoJSON(t, http.MethodGet, checklistURL, tt.AccessToken, nil, &itemsCheck)
	for _, it := range itemsCheck {
		if it.ID == item1.ID {
			assert.True(t, it.Done)
		}
	}

	// Delete second item
	status, _ := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, checklistURL+"/"+item2.ID, tt.AccessToken, nil,
		ifMatch(item2.Etag))
	require.True(t, status >= 200 && status < 300)

	// List, verify 1 item remains
	var items3 []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, checklistURL, tt.AccessToken, nil, &items3)
	require.Len(t, items3, 1)
	assert.Equal(t, item1.ID, items3[0].ID)
}

func TestCommentEditDelete(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create event
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "Discussion",
			"allDay":  false,
			"startAt": "2026-04-23T14:00:00+09:00",
			"endAt":   "2026-04-23T15:00:00+09:00",
		}, &evt)
	require.NotEmpty(t, evt.ID)

	activityURL := calURL + "/events/" + evt.ID + "/activities"

	// Create a comment
	var comment struct {
		ID   string `json:"id"`
		Etag string `json:"etag"`
		Body string `json:"body"`
	}
	helpers.DoJSON(t, http.MethodPost, activityURL, tt.AccessToken,
		map[string]any{"body": "Original comment"}, &comment)
	require.NotEmpty(t, comment.ID)
	assert.Equal(t, "Original comment", comment.Body)

	// List comments, verify 1 comment
	var comments1 struct {
		Items []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, activityURL, tt.AccessToken, nil, &comments1)
	require.Len(t, comments1.Items, 1)
	assert.Equal(t, "Original comment", comments1.Items[0].Body)

	// Update the comment
	var updated struct {
		Body string `json:"body"`
		Etag string `json:"etag"`
	}
	require.NotEmpty(t, comment.Etag)
	doTagged(t, http.MethodPatch, activityURL+"/"+comment.ID, tt.AccessToken, comment.Etag,
		map[string]any{"body": "Edited comment"}, &updated)
	assert.Equal(t, "Edited comment", updated.Body)

	// List comments, verify body is updated
	var comments2 struct {
		Items []struct {
			Body string `json:"body"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, activityURL, tt.AccessToken, nil, &comments2)
	require.Len(t, comments2.Items, 1)
	assert.Equal(t, "Edited comment", comments2.Items[0].Body)

	// Delete the comment
	status, _ := helpers.DoJSONStatusWithHeaders(t, http.MethodDelete, activityURL+"/"+comment.ID, tt.AccessToken, nil,
		ifMatch(updated.Etag))
	require.True(t, status >= 200 && status < 300)

	// List comments, verify 0 comments
	var comments3 struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, activityURL, tt.AccessToken, nil, &comments3)
	require.Len(t, comments3.Items, 0)
}

func TestCommentEditDenied(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt1 := helpers.NewTenant(t, testServerURL)
	tt2 := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt1.CalendarID

	// tt2 joins tt1's calendar
	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt1.AccessToken,
		map[string]any{"role": "editor"}, &invite)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", tt2.AccessToken, nil, nil)

	// tt1 creates event + comment
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt1.AccessToken,
		map[string]any{
			"title": "Private event", "allDay": false,
			"startAt": "2026-04-20T10:00:00+09:00", "endAt": "2026-04-20T11:00:00+09:00",
		}, &evt)

	var comment struct {
		ID   string `json:"id"`
		Etag string `json:"etag"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/activities", tt1.AccessToken,
		map[string]any{"body": "tt1 comment"}, &comment)

	// tt2 tries to edit tt1's comment — should get 403
	status := taggedStatus(t, http.MethodPatch, calURL+"/events/"+evt.ID+"/activities/"+comment.ID, tt2.AccessToken,
		comment.Etag, map[string]any{"body": "hacked"})
	assert.Equal(t, 403, status)

	// tt2 tries to delete tt1's comment — should get 403
	status2 := taggedStatus(t, http.MethodDelete, calURL+"/events/"+evt.ID+"/activities/"+comment.ID, tt2.AccessToken,
		comment.Etag, nil)
	assert.Equal(t, 403, status2)
}

func TestCommentEditDeleteMustMatchEventPath(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var first struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "First", "allDay": false,
			"startAt": "2026-04-20T10:00:00+09:00", "endAt": "2026-04-20T11:00:00+09:00",
		}, &first)

	var second struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "Second", "allDay": false,
			"startAt": "2026-04-21T10:00:00+09:00", "endAt": "2026-04-21T11:00:00+09:00",
		}, &second)

	var comment struct {
		ID   string `json:"id"`
		Etag string `json:"etag"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+first.ID+"/activities", tt.AccessToken,
		map[string]any{"body": "first comment"}, &comment)

	wrongPath := calURL + "/events/" + second.ID + "/activities/" + comment.ID
	status := taggedStatus(t, http.MethodPatch, wrongPath, tt.AccessToken, comment.Etag,
		map[string]any{"body": "wrong event"})
	require.Equal(t, http.StatusNotFound, status)

	status = taggedStatus(t, http.MethodDelete, wrongPath, tt.AccessToken, comment.Etag, nil)
	require.Equal(t, http.StatusNotFound, status)
}

func TestChecklistOnRecurringEvent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create weekly recurring event starting on Monday 2026-04-06
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "Recurring meeting", "allDay": false,
			"startAt": "2026-04-06T10:00:00+09:00", "endAt": "2026-04-06T11:00:00+09:00",
			"recurrenceRule": map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"MO"}},
		}, &evt)

	// List events to get composite instance IDs
	type eventItem struct {
		ID string `json:"id"`
	}
	var listed struct {
		Items []eventItem `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &listed)
	evts := listed.Items
	require.True(t, len(evts) >= 2)
	instanceID := evts[1].ID
	require.Contains(t, instanceID, "_")

	// Add checklist item via composite instance ID
	checklistURL := calURL + "/events/" + instanceID + "/checklist"
	var item struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodPost, checklistURL, tt.AccessToken,
		map[string]any{"title": "Prepare agenda"}, &item)
	require.NotEmpty(t, item.ID)
	assert.Equal(t, "Prepare agenda", item.Title)

	// List checklist via a DIFFERENT instance ID — should see the same item (same parent)
	anotherInstanceID := evts[0].ID
	checklistURL2 := calURL + "/events/" + anotherInstanceID + "/checklist"
	var items []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, checklistURL2, tt.AccessToken, nil, &items)
	require.Len(t, items, 1)
	assert.Equal(t, item.ID, items[0].ID)
}

func TestEventDeleteCascade(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create event
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "Event to delete", "allDay": false,
			"startAt": "2026-04-28T10:00:00+09:00", "endAt": "2026-04-28T11:00:00+09:00",
		}, &evt)

	// Add checklist items
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/checklist", tt.AccessToken,
		map[string]any{"title": "Item 1"}, nil)
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/checklist", tt.AccessToken,
		map[string]any{"title": "Item 2"}, nil)

	// Add a comment
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/activities", tt.AccessToken,
		map[string]any{"body": "Some comment"}, nil)

	// Delete the event
	status := taggedStatus(t, http.MethodDelete, calURL+"/events/"+evt.ID, tt.AccessToken,
		currentTag(t, calURL+"/events/"+evt.ID, tt.AccessToken), nil)
	require.True(t, status >= 200 && status < 300)

	// Get event should 404
	status2, _ := helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+evt.ID, tt.AccessToken, nil)
	assert.Equal(t, 404, status2)
}

func TestNewFieldsInListEvents(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt1 := helpers.NewTenant(t, testServerURL)
	tt2 := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt1.CalendarID

	// tt2 joins
	var invite struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", tt1.AccessToken,
		map[string]any{"role": "editor"}, &invite)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", tt2.AccessToken, nil, nil)

	// Create event with all new fields
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt1.AccessToken,
		map[string]any{
			"title": "Full event", "allDay": false,
			"startAt": "2026-04-20T10:00:00+09:00", "endAt": "2026-04-20T11:00:00+09:00",
			"url":                "https://example.com/meet",
			"notificationOffset": 60,
			"participantIds":     []string{tt2.UserID},
		}, nil)

	// List events
	type listEvent struct {
		URL                string   `json:"url"`
		NotificationOffset *int     `json:"notificationOffset"`
		Participants       []string `json:"participantIds"`
	}
	var listed struct {
		Items []listEvent `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt1.AccessToken, nil, &listed)
	evts := listed.Items
	require.Len(t, evts, 1)
	assert.Equal(t, "https://example.com/meet", evts[0].URL)
	require.NotNil(t, evts[0].NotificationOffset)
	assert.Equal(t, 60, *evts[0].NotificationOffset)
}

func TestStorageAbsentAttachmentPresign(t *testing.T) {
	bootstrap(t)
	requireStorageAbsent(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create event
	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title": "No storage", "allDay": false,
			"startAt": "2026-04-20T10:00:00+09:00", "endAt": "2026-04-20T11:00:00+09:00",
		}, &evt)

	// Presign upload should fail with 503 (no storage configured)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events/"+evt.ID+"/attachments/presign", tt.AccessToken,
		map[string]any{"filename": "test.txt", "byteSize": 100, "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"})
	assert.Equal(t, 503, status)

	// List attachments should still work (empty list)
	var atts []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID+"/attachments", tt.AccessToken, nil, &atts)
	assert.Len(t, atts, 0)
}
