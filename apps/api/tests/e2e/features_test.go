package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID, tt.AccessToken,
		map[string]any{
			"title":   "Meeting with link",
			"allDay":  false,
			"startAt": "2026-04-20T15:00:00+09:00",
			"endAt":   "2026-04-20T16:00:00+09:00",
			"url":     "https://zoom.us/j/123",
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
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID, tt.AccessToken,
		map[string]any{
			"title":              "Reminder event",
			"allDay":             false,
			"startAt":            "2026-04-21T10:00:00+09:00",
			"endAt":              "2026-04-21T11:00:00+09:00",
			"notificationOffset": nil,
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
		map[string]any{"role": "member"}, &invite)

	// tt2 accepts the invite
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", tt2.AccessToken,
		nil, nil)

	// tt1 creates an event with tt2 as participant
	var evt struct {
		ID           string   `json:"id"`
		Participants []string `json:"participants"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt1.AccessToken,
		map[string]any{
			"title":        "Team lunch",
			"allDay":       false,
			"startAt":      "2026-04-22T12:00:00+09:00",
			"endAt":        "2026-04-22T13:00:00+09:00",
			"participants": []string{tt2.UserID},
		}, &evt)
	require.NotEmpty(t, evt.ID)
	require.Len(t, evt.Participants, 1)
	assert.Equal(t, tt2.UserID, evt.Participants[0])

	// Get the event, verify participants
	var got struct {
		Participants []string `json:"participants"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, tt1.AccessToken, nil, &got)
	require.Len(t, got.Participants, 1)
	assert.Equal(t, tt2.UserID, got.Participants[0])

	// Update event with empty participants
	var updated struct {
		Participants []string `json:"participants"`
	}
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID, tt1.AccessToken,
		map[string]any{
			"title":        "Team lunch",
			"allDay":       false,
			"startAt":      "2026-04-22T12:00:00+09:00",
			"endAt":        "2026-04-22T13:00:00+09:00",
			"participants": []string{},
		}, &updated)
	assert.Empty(t, updated.Participants)

	// Get again, verify participants is empty
	var got2 struct {
		Participants []string `json:"participants"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, tt1.AccessToken, nil, &got2)
	assert.Empty(t, got2.Participants)
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
		ID string `json:"id"`
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
	helpers.DoJSON(t, http.MethodPut, checklistURL+"/"+item1.ID, tt.AccessToken,
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
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, checklistURL+"/"+item2.ID, tt.AccessToken, nil)
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
		Body string `json:"body"`
	}
	helpers.DoJSON(t, http.MethodPost, activityURL, tt.AccessToken,
		map[string]any{"content": "Original comment"}, &comment)
	require.NotEmpty(t, comment.ID)
	assert.Equal(t, "Original comment", comment.Body)

	// List comments, verify 1 comment
	var comments1 []struct {
		ID   string `json:"id"`
		Body string `json:"body"`
	}
	helpers.DoJSON(t, http.MethodGet, activityURL, tt.AccessToken, nil, &comments1)
	require.Len(t, comments1, 1)
	assert.Equal(t, "Original comment", comments1[0].Body)

	// Update the comment
	var updated struct {
		Body string `json:"body"`
	}
	helpers.DoJSON(t, http.MethodPut, activityURL+"/"+comment.ID, tt.AccessToken,
		map[string]any{"content": "Edited comment"}, &updated)
	assert.Equal(t, "Edited comment", updated.Body)

	// List comments, verify body is updated
	var comments2 []struct {
		Body string `json:"body"`
	}
	helpers.DoJSON(t, http.MethodGet, activityURL, tt.AccessToken, nil, &comments2)
	require.Len(t, comments2, 1)
	assert.Equal(t, "Edited comment", comments2[0].Body)

	// Delete the comment
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, activityURL+"/"+comment.ID, tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	// List comments, verify 0 comments
	var comments3 []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, activityURL, tt.AccessToken, nil, &comments3)
	require.Len(t, comments3, 0)
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
		map[string]any{"role": "member"}, &invite)
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
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events/"+evt.ID+"/activities", tt1.AccessToken,
		map[string]any{"content": "tt1 comment"}, &comment)

	// tt2 tries to edit tt1's comment — should get 403
	status, _ := helpers.DoJSONStatus(t, http.MethodPut, calURL+"/events/"+evt.ID+"/activities/"+comment.ID, tt2.AccessToken,
		map[string]any{"content": "hacked"})
	assert.Equal(t, 403, status)

	// tt2 tries to delete tt1's comment — should get 403
	status2, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evt.ID+"/activities/"+comment.ID, tt2.AccessToken, nil)
	assert.Equal(t, 403, status2)
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
	var evts []eventItem
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt.AccessToken, nil, &evts)
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
		map[string]any{"content": "Some comment"}, nil)

	// Delete the event
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/events/"+evt.ID, tt.AccessToken, nil)
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
		map[string]any{"role": "member"}, &invite)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/invites/"+invite.Token+"/accept", tt2.AccessToken, nil, nil)

	// Create event with all new fields
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt1.AccessToken,
		map[string]any{
			"title": "Full event", "allDay": false,
			"startAt": "2026-04-20T10:00:00+09:00", "endAt": "2026-04-20T11:00:00+09:00",
			"url":                "https://example.com/meet",
			"notificationOffset": 60,
			"participants":       []string{tt2.UserID},
		}, nil)

	// List events
	type listEvent struct {
		URL                string   `json:"url"`
		NotificationOffset *int     `json:"notificationOffset"`
		Participants       []string `json:"participants"`
	}
	var evts []listEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-04-01&end=2026-04-30", tt1.AccessToken, nil, &evts)
	require.Len(t, evts, 1)
	assert.Equal(t, "https://example.com/meet", evts[0].URL)
	require.NotNil(t, evts[0].NotificationOffset)
	assert.Equal(t, 60, *evts[0].NotificationOffset)
}

func TestAttachmentWithoutStorage(t *testing.T) {
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
			"title": "No storage", "allDay": false,
			"startAt": "2026-04-20T10:00:00+09:00", "endAt": "2026-04-20T11:00:00+09:00",
		}, &evt)

	// Presign upload should fail with 503 (no storage configured)
	status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events/"+evt.ID+"/attachments/presign", tt.AccessToken,
		map[string]any{"filename": "test.txt", "byteSize": 100})
	assert.Equal(t, 503, status)

	// List attachments should still work (empty list)
	var atts []struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID+"/attachments", tt.AccessToken, nil, &atts)
	assert.Len(t, atts, 0)
}
