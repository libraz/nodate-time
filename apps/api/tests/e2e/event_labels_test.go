package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type lbLabel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	ETag  string `json:"etag"`
}

type lbEvent struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Color          string  `json:"color"`
	LabelID        *string `json:"labelId"`
	Etag           string  `json:"etag"`
	SeriesEtag     string  `json:"seriesEtag"`
	RecurrenceDate *string `json:"recurrenceDate"`
	DetailsHidden  bool    `json:"detailsHidden"`
}

// label is the event's label id, empty for none.
func (e lbEvent) label() string {
	if e.LabelID == nil {
		return ""
	}
	return *e.LabelID
}

func lbCreateLabel(t *testing.T, calURL, token, name, color string) lbLabel {
	t.Helper()
	var out lbLabel
	helpers.DoJSON(t, http.MethodPost, calURL+"/labels", token, map[string]any{"name": name, "color": color}, &out)
	require.NotEmpty(t, out.ID)
	return out
}

func lbLabels(t *testing.T, calURL, token string) []lbLabel {
	t.Helper()
	var out []lbLabel
	helpers.DoJSON(t, http.MethodGet, calURL+"/labels", token, nil, &out)
	return out
}

// lbSend makes a conditional label write and returns the status and raw body.
func lbSend(t *testing.T, method, url, token, etag string, body any) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatusWithHeaders(t, method, url, token, body, ifMatch(etag))
}

func lbCreateEvent(t *testing.T, calURL, token string, extra map[string]any) lbEvent {
	t.Helper()
	body := map[string]any{
		"title": "Labelled", "allDay": false,
		"startAt": "2026-09-01T10:00:00+09:00", "endAt": "2026-09-01T11:00:00+09:00",
	}
	for k, v := range extra {
		body[k] = v
	}
	var out lbEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", token, body, &out)
	require.NotEmpty(t, out.ID)
	return out
}

func lbGet(t *testing.T, calURL, token, id string) lbEvent {
	t.Helper()
	var out lbEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, token, nil, &out)
	return out
}

func lbPatch(t *testing.T, calURL, token, id, scope, etag string, body map[string]any) (int, []byte, lbEvent) {
	t.Helper()
	url := calURL + "/events/" + id
	if scope != "" {
		url += "?scope=" + scope
	}
	status, raw := lbSend(t, http.MethodPatch, url, token, etag, body)
	var out lbEvent
	if status == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &out))
	}
	return status, raw, out
}

// lbOwnerColor is the colour of userID's layer on the calendar.
func lbOwnerColor(t *testing.T, calURL, token, userID string) string {
	t.Helper()
	c := membersOf(t, calURL, token)[userID].Color
	require.NotEmpty(t, c)
	return c
}

// lbLoggedLabelIDs is the labelId payload of every log row of a kind in a calendar.
func lbLoggedLabelIDs(t *testing.T, calendarID, eventType string) []string {
	t.Helper()
	rows, err := testDB.QueryContext(t.Context(),
		`SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(e.payload_json, '$.labelId')), '')
		   FROM events e JOIN calendars c ON c.id = e.calendar_id
		  WHERE c.public_id = UUID_TO_BIN(?) AND e.type = ? ORDER BY e.id`, calendarID, eventType)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		out = append(out, id)
	}
	require.NoError(t, rows.Err())
	return out
}

// lbSeriesList reads a weekly series' occurrences keyed by date.
func lbSeriesList(t *testing.T, calURL, token string) map[string]lbEvent {
	t.Helper()
	var items []lbEvent
	getItems(t, http.MethodGet, calURL+"/events?"+flWindow, token, nil, &items)
	out := map[string]lbEvent{}
	for _, it := range items {
		if it.RecurrenceDate != nil {
			out[*it.RecurrenceDate] = it
		}
	}
	return out
}

func lbOccurrence(t *testing.T, calURL, token string, n int) lbEvent {
	t.Helper()
	occ, ok := lbSeriesList(t, calURL, token)[flDay(n)]
	require.True(t, ok, "occurrence %d (%s) is not listed", n, flDay(n))
	return occ
}

func TestEventLabels(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	// An administrator creates, renames, recolours and deletes labels, and
	// each change is logged once under the label's id.
	t.Run("manage", func(t *testing.T) {
		t.Parallel()
		owner := helpers.NewTenant(t, testServerURL)
		manager := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		joinAs(t, calURL, owner.AccessToken, manager, "editor")
		require.Equal(t, http.StatusOK, promote(t, calURL, owner.AccessToken, manager, "manager"))

		status, raw, header := helpers.DoJSONFull(t, http.MethodPost, calURL+"/labels", owner.AccessToken,
			map[string]any{"name": "  ライブ　", "color": "#3366cc"}, nil)
		require.Equal(t, http.StatusCreated, status, string(raw))
		var live lbLabel
		require.NoError(t, json.Unmarshal(raw, &live))
		require.Equal(t, "ライブ", live.Name, "the name is stored trimmed")
		require.Equal(t, "#3366CC", live.Color, "the colour is stored upper-case")
		require.NotEmpty(t, live.ETag)
		require.Equal(t, live.ETag, header.Get("ETag"))

		lesson := lbCreateLabel(t, calURL, manager.AccessToken, "レッスン", "#D35400")
		require.Equal(t, []lbLabel{live, lesson}, lbLabels(t, calURL, owner.AccessToken), "listed in creation order")

		status, raw, header = helpers.DoJSONFull(t, http.MethodPatch, calURL+"/labels/"+live.ID, manager.AccessToken,
			map[string]any{"name": "ライブ本番"}, ifMatch(live.ETag))
		require.Equal(t, http.StatusOK, status, string(raw))
		var renamed lbLabel
		require.NoError(t, json.Unmarshal(raw, &renamed))
		require.Equal(t, "ライブ本番", renamed.Name)
		require.Equal(t, "#3366CC", renamed.Color, "an omitted colour keeps the stored one")
		require.Equal(t, renamed.ETag, header.Get("ETag"))

		status, raw = lbSend(t, http.MethodPatch, calURL+"/labels/"+live.ID, owner.AccessToken, renamed.ETag,
			map[string]any{"color": "#1a7f5a"})
		require.Equal(t, http.StatusOK, status, string(raw))
		var recoloured lbLabel
		require.NoError(t, json.Unmarshal(raw, &recoloured))
		require.Equal(t, "ライブ本番", recoloured.Name)
		require.Equal(t, "#1A7F5A", recoloured.Color)

		status, raw = lbSend(t, http.MethodDelete, calURL+"/labels/"+lesson.ID, owner.AccessToken, lesson.ETag, nil)
		require.Equal(t, http.StatusNoContent, status, string(raw))
		require.Equal(t, []lbLabel{recoloured}, lbLabels(t, calURL, owner.AccessToken))

		require.Equal(t, []string{live.ID, lesson.ID}, lbLoggedLabelIDs(t, owner.CalendarID, "calendar.label.created"))
		require.Equal(t, []string{live.ID, live.ID}, lbLoggedLabelIDs(t, owner.CalendarID, "calendar.label.updated"))
		require.Equal(t, []string{lesson.ID}, lbLoggedLabelIDs(t, owner.CalendarID, "calendar.label.deleted"))
	})

	// Editors and viewers read the labels but cannot change them.
	t.Run("roles", func(t *testing.T) {
		t.Parallel()
		owner := helpers.NewTenant(t, testServerURL)
		editor := helpers.NewTenant(t, testServerURL)
		viewer := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		joinAs(t, calURL, owner.AccessToken, editor, "editor")
		joinAs(t, calURL, owner.AccessToken, viewer, "viewer")
		label := lbCreateLabel(t, calURL, owner.AccessToken, "Shoot", "#9B59B6")

		for name, member := range map[string]*helpers.TestTenant{"editor": editor, "viewer": viewer} {
			require.Equal(t, []lbLabel{label}, lbLabels(t, calURL, member.AccessToken), name)

			status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/labels", member.AccessToken,
				map[string]any{"name": "Mine", "color": "#000000"})
			require.Equal(t, http.StatusForbidden, status, "%s create: %s", name, raw)
			status, raw = lbSend(t, http.MethodPatch, calURL+"/labels/"+label.ID, member.AccessToken, label.ETag,
				map[string]any{"name": "Renamed"})
			require.Equal(t, http.StatusForbidden, status, "%s update: %s", name, raw)
			status, raw = lbSend(t, http.MethodDelete, calURL+"/labels/"+label.ID, member.AccessToken, label.ETag, nil)
			require.Equal(t, http.StatusForbidden, status, "%s delete: %s", name, raw)
		}
		require.Equal(t, []lbLabel{label}, lbLabels(t, calURL, owner.AccessToken), "nothing changed")
	})

	// A label on an event decides its colour; removing it brings back the
	// owner's, and leaving it out of a patch keeps it.
	t.Run("assign", func(t *testing.T) {
		t.Parallel()
		owner := helpers.NewTenant(t, testServerURL)
		editor := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		joinAs(t, calURL, owner.AccessToken, editor, "editor")
		ownerColor := lbOwnerColor(t, calURL, owner.AccessToken, owner.UserID)
		label := lbCreateLabel(t, calURL, owner.AccessToken, "Live", "#1a7f5a")
		require.NotEqual(t, ownerColor, label.Color)

		created := lbCreateEvent(t, calURL, owner.AccessToken, map[string]any{"labelId": label.ID})
		require.Equal(t, label.ID, created.label())
		require.Equal(t, "#1A7F5A", created.Color)
		got := lbGet(t, calURL, owner.AccessToken, created.ID)
		require.Equal(t, label.ID, got.label())
		require.Equal(t, "#1A7F5A", got.Color)

		// An editor files their own event under a label without managing labels.
		editorColor := lbOwnerColor(t, calURL, owner.AccessToken, editor.UserID)
		require.NotEqual(t, editorColor, label.Color)
		plain := lbCreateEvent(t, calURL, editor.AccessToken, nil)
		require.Nil(t, plain.LabelID)
		require.Equal(t, editorColor, plain.Color)

		status, raw, labelled := lbPatch(t, calURL, editor.AccessToken, plain.ID, "", plain.Etag,
			map[string]any{"labelId": label.ID})
		require.Equal(t, http.StatusOK, status, string(raw))
		require.Equal(t, label.ID, labelled.label())
		require.Equal(t, "#1A7F5A", labelled.Color)
		// The label-only edit moved the event's tag.
		status, raw, _ = lbPatch(t, calURL, owner.AccessToken, plain.ID, "", plain.Etag, map[string]any{"title": "Late"})
		require.Equal(t, http.StatusPreconditionFailed, status, string(raw))

		status, raw, renamed := lbPatch(t, calURL, owner.AccessToken, plain.ID, "", labelled.Etag,
			map[string]any{"title": "Renamed"})
		require.Equal(t, http.StatusOK, status, string(raw))
		require.Equal(t, label.ID, renamed.label(), "an omitted labelId keeps the label")
		require.Equal(t, "#1A7F5A", renamed.Color)

		status, raw, cleared := lbPatch(t, calURL, owner.AccessToken, plain.ID, "", renamed.Etag,
			map[string]any{"labelId": nil})
		require.Equal(t, http.StatusOK, status, string(raw))
		require.Nil(t, cleared.LabelID)
		require.Equal(t, editorColor, cleared.Color)

		status, raw, _ = lbPatch(t, calURL, owner.AccessToken, created.ID, "", got.Etag, map[string]any{"labelId": ""})
		require.Equal(t, http.StatusOK, status, string(raw))
		got = lbGet(t, calURL, owner.AccessToken, created.ID)
		require.Nil(t, got.LabelID, "an empty labelId removes the label")
		require.Equal(t, ownerColor, got.Color)

		var listed []lbEvent
		getItems(t, http.MethodGet, calURL+"/events?start=2026-09-01&end=2026-09-01", owner.AccessToken, nil, &listed)
		require.Len(t, listed, 2)
		colors := map[string]string{created.ID: ownerColor, plain.ID: editorColor}
		for _, e := range listed {
			require.Nil(t, e.LabelID)
			require.Equal(t, colors[e.ID], e.Color)
		}
	})

	// A labelId that names no label of the event's calendar is refused with
	// its own code, on create and on patch.
	t.Run("foreign", func(t *testing.T) {
		t.Parallel()
		owner := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		var other struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars", owner.AccessToken,
			map[string]any{"name": "Other"}, &other)
		foreign := lbCreateLabel(t, testServerURL+"/calendars/"+other.ID, owner.AccessToken, "Elsewhere", "#C0392B")
		evt := lbCreateEvent(t, calURL, owner.AccessToken, nil)

		for name, id := range map[string]string{
			"another calendar's": foreign.ID,
			"unknown":            uuid.NewString(),
			"malformed":          "not-a-uuid",
		} {
			status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", owner.AccessToken,
				map[string]any{
					"title": "Refused", "allDay": false,
					"startAt": "2026-09-02T10:00:00+09:00", "endAt": "2026-09-02T11:00:00+09:00",
					"labelId": id,
				})
			require.Equal(t, http.StatusUnprocessableEntity, status, "create with %s: %s", name, raw)
			require.Equal(t, "EVENT.LABEL_UNKNOWN", adCode(t, raw), name)

			status, raw, _ = lbPatch(t, calURL, owner.AccessToken, evt.ID, "", evt.Etag, map[string]any{"labelId": id})
			require.Equal(t, http.StatusUnprocessableEntity, status, "patch with %s: %s", name, raw)
			require.Equal(t, "EVENT.LABEL_UNKNOWN", adCode(t, raw), name)
		}
		require.Nil(t, lbGet(t, calURL, owner.AccessToken, evt.ID).LabelID)
	})

	// Deleting a label leaves its events in place, in their owner's colour,
	// and an edit that does not name a label still goes through.
	t.Run("delete", func(t *testing.T) {
		t.Parallel()
		owner := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		ownerColor := lbOwnerColor(t, calURL, owner.AccessToken, owner.UserID)
		label := lbCreateLabel(t, calURL, owner.AccessToken, "Gone soon", "#F1C40F")
		evt := lbCreateEvent(t, calURL, owner.AccessToken, map[string]any{"labelId": label.ID})
		require.Equal(t, "#F1C40F", evt.Color)

		status, raw := lbSend(t, http.MethodDelete, calURL+"/labels/"+label.ID, owner.AccessToken, label.ETag, nil)
		require.Equal(t, http.StatusNoContent, status, string(raw))

		got := lbGet(t, calURL, owner.AccessToken, evt.ID)
		require.Nil(t, got.LabelID)
		require.Equal(t, ownerColor, got.Color)

		status, raw, renamed := lbPatch(t, calURL, owner.AccessToken, evt.ID, "", evt.Etag,
			map[string]any{"title": "Still here"})
		require.Equal(t, http.StatusOK, status, string(raw))
		require.Equal(t, "Still here", renamed.Title)
		require.Nil(t, renamed.LabelID)

		status, raw, _ = lbPatch(t, calURL, owner.AccessToken, evt.ID, "", renamed.Etag,
			map[string]any{"labelId": label.ID})
		require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
		require.Equal(t, "EVENT.LABEL_UNKNOWN", adCode(t, raw))
	})

	// A series' label reaches every occurrence and the overrides it already
	// had; a new override or a split takes the edited row's label unless the
	// body names one.
	t.Run("series", func(t *testing.T) {
		t.Parallel()
		owner := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		live := lbCreateLabel(t, calURL, owner.AccessToken, "Live", "#1A7F5A")
		lesson := lbCreateLabel(t, calURL, owner.AccessToken, "Lesson", "#D35400")

		var series lbEvent
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
			"title": "Weekly", "allDay": false,
			"startAt": "2026-03-02T10:00:00Z", "endAt": "2026-03-02T11:00:00Z", "timezone": "UTC",
			"recurrenceRule": weekly(map[string]any{"count": 10}),
		}, &series)

		// An override that exists before the series is labelled.
		occ := lbOccurrence(t, calURL, owner.AccessToken, 1)
		status, raw, _ := lbPatch(t, calURL, owner.AccessToken, occ.ID, "this", occ.Etag, map[string]any{"title": "Moved"})
		require.Equal(t, http.StatusOK, status, string(raw))

		status, raw, _ = lbPatch(t, calURL, owner.AccessToken, series.ID, "all",
			lbGet(t, calURL, owner.AccessToken, series.ID).Etag, map[string]any{"labelId": live.ID})
		require.Equal(t, http.StatusOK, status, string(raw))
		all := lbSeriesList(t, calURL, owner.AccessToken)
		require.Len(t, all, 10)
		for day, e := range all {
			require.Equal(t, live.ID, e.label(), day)
			require.Equal(t, "#1A7F5A", e.Color, day)
		}
		require.Equal(t, "Moved", all[flDay(1)].Title, "the existing override took the label, not the title")

		// scope=this without a labelId inherits the edited row's label.
		occ = lbOccurrence(t, calURL, owner.AccessToken, 3)
		status, raw, got := lbPatch(t, calURL, owner.AccessToken, occ.ID, "this", occ.Etag, map[string]any{"title": "Third"})
		require.Equal(t, http.StatusOK, status, string(raw))
		require.Equal(t, live.ID, got.label())
		require.Equal(t, "#1A7F5A", lbOccurrence(t, calURL, owner.AccessToken, 3).Color)

		// scope=this with a labelId takes that one, for that occurrence only.
		occ = lbOccurrence(t, calURL, owner.AccessToken, 4)
		status, raw, got = lbPatch(t, calURL, owner.AccessToken, occ.ID, "this", occ.Etag, map[string]any{"labelId": lesson.ID})
		require.Equal(t, http.StatusOK, status, string(raw))
		require.Equal(t, lesson.ID, got.label())
		require.Equal(t, "#D35400", got.Color)
		require.Equal(t, live.ID, lbOccurrence(t, calURL, owner.AccessToken, 5).label())

		// scope=following without a labelId keeps the series' label.
		occ = lbOccurrence(t, calURL, owner.AccessToken, 6)
		status, raw, _ = lbPatch(t, calURL, owner.AccessToken, occ.ID, "following", occ.SeriesEtag,
			map[string]any{"title": "Later"})
		require.Equal(t, http.StatusOK, status, string(raw))
		for _, n := range []int{6, 7} {
			e := lbOccurrence(t, calURL, owner.AccessToken, n)
			require.Equal(t, "Later", e.Title, n)
			require.Equal(t, live.ID, e.label(), n)
		}

		// scope=following with a labelId gives the new series that label.
		occ = lbOccurrence(t, calURL, owner.AccessToken, 8)
		status, raw, _ = lbPatch(t, calURL, owner.AccessToken, occ.ID, "following", occ.SeriesEtag,
			map[string]any{"labelId": lesson.ID})
		require.Equal(t, http.StatusOK, status, string(raw))
		for _, n := range []int{8, 9} {
			e := lbOccurrence(t, calURL, owner.AccessToken, n)
			require.Equal(t, lesson.ID, e.label(), n)
			require.Equal(t, "#D35400", e.Color, n)
		}
		require.Equal(t, live.ID, lbOccurrence(t, calURL, owner.AccessToken, 7).label(), "the earlier series keeps its label")
	})

	// A reader with the details hidden sees neither the label nor its colour,
	// and cannot write one.
	t.Run("hidden", func(t *testing.T) {
		t.Parallel()
		w := newVMWorld(t, "private", "")
		ownerColor := lbOwnerColor(t, w.calURL, w.owner.AccessToken, w.owner.UserID)
		label := lbCreateLabel(t, w.calURL, w.owner.AccessToken, "Shoot "+w.tag, "#C0392B")
		require.NotEqual(t, ownerColor, label.Color)

		stored := lbGet(t, w.calURL, w.owner.AccessToken, w.event.ID)
		status, raw, _ := lbPatch(t, w.calURL, w.owner.AccessToken, w.event.ID, "", stored.Etag,
			map[string]any{"labelId": label.ID})
		require.Equal(t, http.StatusOK, status, string(raw))
		require.Equal(t, label.ID, lbGet(t, w.calURL, w.owner.AccessToken, w.event.ID).label())

		masked := lbGet(t, w.calURL, w.manager.AccessToken, w.event.ID)
		require.True(t, masked.DetailsHidden)
		require.Nil(t, masked.LabelID)
		require.Equal(t, ownerColor, masked.Color)
		var listed []lbEvent
		getItems(t, http.MethodGet, w.calURL+"/events?start=2026-07-06&end=2026-07-06", w.manager.AccessToken, nil, &listed)
		require.Len(t, listed, 1)
		require.Nil(t, listed[0].LabelID)
		require.Equal(t, ownerColor, listed[0].Color)

		for name, value := range map[string]any{"set": label.ID, "clear": nil} {
			status, raw, _ := lbPatch(t, w.calURL, w.manager.AccessToken, w.event.ID, "", masked.Etag,
				map[string]any{"labelId": value})
			require.Equal(t, http.StatusForbidden, status, "%s: %s", name, raw)
			require.Equal(t, "EVENT.DETAILS_HIDDEN", adCode(t, raw), name)
		}
		require.Equal(t, label.ID, lbGet(t, w.calURL, w.owner.AccessToken, w.event.ID).label())
	})

	// A patch that changes nothing writes nothing and logs nothing, but is
	// still held to its If-Match.
	t.Run("noop", func(t *testing.T) {
		t.Parallel()
		owner := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		label := lbCreateLabel(t, calURL, owner.AccessToken, "Same", "#1A7F5A")
		url := calURL + "/labels/" + label.ID

		for name, body := range map[string]map[string]any{
			"empty":        {},
			"equal":        {"name": "Same", "color": "#1A7F5A"},
			"equal folded": {"name": " Same ", "color": "#1a7f5a"},
			"equal name":   {"name": "Same"},
			"equal colour": {"color": "#1A7F5A"},
		} {
			status, raw := lbSend(t, http.MethodPatch, url, owner.AccessToken, label.ETag, body)
			require.Equal(t, http.StatusOK, status, "%s: %s", name, raw)
			var got lbLabel
			require.NoError(t, json.Unmarshal(raw, &got))
			require.Equal(t, label, got, name)
		}
		require.Empty(t, lbLoggedLabelIDs(t, owner.CalendarID, "calendar.label.updated"))

		status, raw := lbSend(t, http.MethodPatch, url, owner.AccessToken, label.ETag, map[string]any{"name": "Different"})
		require.Equal(t, http.StatusOK, status, string(raw))
		require.Equal(t, []string{label.ID}, lbLoggedLabelIDs(t, owner.CalendarID, "calendar.label.updated"))

		status, raw = lbSend(t, http.MethodPatch, url, owner.AccessToken, label.ETag, map[string]any{})
		require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
		require.Equal(t, "LABEL.STALE", adCode(t, raw))
	})

	// Names are unique per calendar after trimming, a calendar holds at most
	// fifty labels, and a stale If-Match is refused.
	t.Run("limits", func(t *testing.T) {
		t.Parallel()
		owner := helpers.NewTenant(t, testServerURL)
		other := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID
		first := lbCreateLabel(t, calURL, owner.AccessToken, "ライブ", "#3366CC")

		for _, name := range []string{"ライブ", " ライブ "} {
			status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/labels", owner.AccessToken,
				map[string]any{"name": name, "color": "#000000"})
			require.Equal(t, http.StatusConflict, status, "%q: %s", name, raw)
			require.Equal(t, "LABEL.NAME_TAKEN", adCode(t, raw))
		}
		// Kana that differ in script or voicing are different names.
		second := lbCreateLabel(t, calURL, owner.AccessToken, "らいぶ", "#D35400")
		lbCreateLabel(t, calURL, owner.AccessToken, "バス", "#9B59B6")
		lbCreateLabel(t, calURL, owner.AccessToken, "パス", "#F1C40F")
		// The same name on another calendar is fine.
		lbCreateLabel(t, testServerURL+"/calendars/"+other.CalendarID, other.AccessToken, "ライブ", "#3366CC")

		status, raw := lbSend(t, http.MethodPatch, calURL+"/labels/"+second.ID, owner.AccessToken, second.ETag,
			map[string]any{"name": "ライブ"})
		require.Equal(t, http.StatusConflict, status, string(raw))
		require.Equal(t, "LABEL.NAME_TAKEN", adCode(t, raw))

		// Make the first label's tag stale.
		status, raw = lbSend(t, http.MethodPatch, calURL+"/labels/"+first.ID, owner.AccessToken, first.ETag,
			map[string]any{"color": "#000000"})
		require.Equal(t, http.StatusOK, status, string(raw))
		for method, body := range map[string]any{http.MethodPatch: map[string]any{"name": "Stale"}, http.MethodDelete: nil} {
			status, raw = lbSend(t, method, calURL+"/labels/"+first.ID, owner.AccessToken, first.ETag, body)
			require.Equal(t, http.StatusPreconditionFailed, status, "%s: %s", method, raw)
			require.Equal(t, "LABEL.STALE", adCode(t, raw), method)
		}

		for n := len(lbLabels(t, calURL, owner.AccessToken)); n < 50; n++ {
			lbCreateLabel(t, calURL, owner.AccessToken, "L"+strings.Repeat("x", n), "#7A7A7A")
		}
		require.Len(t, lbLabels(t, calURL, owner.AccessToken), 50)
		status, raw = helpers.DoJSONStatus(t, http.MethodPost, calURL+"/labels", owner.AccessToken,
			map[string]any{"name": "One too many", "color": "#000000"})
		require.Equal(t, http.StatusConflict, status, string(raw))
		require.Equal(t, "LABEL.LIMIT_REACHED", adCode(t, raw))
	})

	// The edges of a name and a colour.
	t.Run("input", func(t *testing.T) {
		t.Parallel()
		owner := helpers.NewTenant(t, testServerURL)
		calURL := testServerURL + "/calendars/" + owner.CalendarID

		fifty := strings.Repeat("あ", 50)
		padded := lbCreateLabel(t, calURL, owner.AccessToken, "  "+fifty+"　", "#3366CC")
		require.Equal(t, fifty, padded.Name, "the limit counts the trimmed name")

		for name, body := range map[string]map[string]any{
			"blank":             {"name": " 　 ", "color": "#000000"},
			"tab":               {"name": "a\tb", "color": "#000000"},
			"newline":           {"name": "a\nb", "color": "#000000"},
			"over fifty":        {"name": fifty + "い", "color": "#000000"},
			"padded over fifty": {"name": " " + fifty + "い ", "color": "#000000"},
		} {
			status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/labels", owner.AccessToken, body)
			require.Equal(t, http.StatusBadRequest, status, "%s: %s", name, raw)
		}
		for name, body := range map[string]map[string]any{
			"empty name":  {"name": "", "color": "#000000"},
			"short color": {"name": "Short", "color": "#FFF"},
			"named color": {"name": "Named", "color": "red"},
		} {
			status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/labels", owner.AccessToken, body)
			require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", name, raw)
		}

		url := calURL + "/labels/" + padded.ID
		for name, body := range map[string]map[string]any{
			"blank name":   {"name": "   "},
			"control name": {"name": "a\u0007b"},
		} {
			status, raw := lbSend(t, http.MethodPatch, url, owner.AccessToken, padded.ETag, body)
			require.Equal(t, http.StatusBadRequest, status, "%s: %s", name, raw)
		}
		// Neither member can be cleared.
		for name, body := range map[string]map[string]any{
			"null name":  {"name": nil},
			"null color": {"color": nil},
		} {
			status, raw := lbSend(t, http.MethodPatch, url, owner.AccessToken, padded.ETag, body)
			require.Equal(t, http.StatusUnprocessableEntity, status, "%s: %s", name, raw)
		}
		require.Equal(t, []lbLabel{padded}, lbLabels(t, calURL, owner.AccessToken), "nothing changed")
	})
}
