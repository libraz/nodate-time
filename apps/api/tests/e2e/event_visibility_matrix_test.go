package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// vmAccess is what a reader may see of an event.
type vmAccess int

const (
	vmFull vmAccess = iota
	vmMasked
	vmNone
)

func (a vmAccess) String() string { return [...]string{"full", "masked", "none"}[a] }

// vmEvent is an event as a reader sees it, including the fields a masked
// reader must not receive.
type vmEvent struct {
	ID            string  `json:"id"`
	Title         *string `json:"title"`
	AllDay        bool    `json:"allDay"`
	Kind          string  `json:"kind"`
	StartAt       string  `json:"startAt"`
	EndAt         string  `json:"endAt"`
	Location      string  `json:"location"`
	Memo          string  `json:"memo"`
	URL           string  `json:"url"`
	ShowAs        string  `json:"showAs"`
	OwnerID       string  `json:"ownerId"`
	Etag          string  `json:"etag"`
	DetailsHidden bool    `json:"detailsHidden"`
	Attendees     []struct {
		UserID string `json:"userId"`
		Rsvp   string `json:"rsvp"`
	} `json:"attendees"`
}

func (e vmEvent) title() string {
	if e.Title == nil {
		return ""
	}
	return *e.Title
}

type vmList struct {
	Items   []vmEvent `json:"items"`
	HasMore bool      `json:"hasMore"`
}

// vmWorld is one calendar with every reader class the visibility rules tell
// apart: the owner, a different creator, an attendee, two members who are
// none of those, and an anonymous share link.
type vmWorld struct {
	calURL   string
	owner    *helpers.TestTenant
	creator  *helpers.TestTenant // manager who created the event on the owner's behalf
	attendee *helpers.TestTenant
	viewer   *helpers.TestTenant // read-only member with no tie to the event
	manager  *helpers.TestTenant // manager with no tie to the event
	share    string

	tag      string
	event    vmEvent
	location string
	memo     string
	url      string
	comment  string
}

func newVMWorld(t *testing.T, visibility string, calendarDefault string) *vmWorld {
	t.Helper()
	w := &vmWorld{
		owner:    helpers.NewTenant(t, testServerURL),
		creator:  helpers.NewTenant(t, testServerURL),
		attendee: helpers.NewTenant(t, testServerURL),
		viewer:   helpers.NewTenant(t, testServerURL),
		manager:  helpers.NewTenant(t, testServerURL),
		tag:      strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
	}
	w.calURL = testServerURL + "/calendars/" + w.owner.CalendarID
	joinAs(t, w.calURL, w.owner.AccessToken, w.creator, "editor")
	require.Equal(t, http.StatusOK, promote(t, w.calURL, w.owner.AccessToken, w.creator, "manager"))
	joinAs(t, w.calURL, w.owner.AccessToken, w.attendee, "editor")
	joinAs(t, w.calURL, w.owner.AccessToken, w.viewer, "viewer")
	joinAs(t, w.calURL, w.owner.AccessToken, w.manager, "editor")
	require.Equal(t, http.StatusOK, promote(t, w.calURL, w.owner.AccessToken, w.manager, "manager"))
	w.share = publicShareToken(t, w.calURL, w.owner.AccessToken)

	if calendarDefault != "" {
		_, err := testDB.ExecContext(t.Context(),
			`UPDATE calendars SET default_event_visibility = ? WHERE public_id = UUID_TO_BIN(?)`,
			calendarDefault, w.owner.CalendarID)
		require.NoError(t, err)
	}

	w.location = "LOC-" + w.tag
	w.memo = "MEMO-" + w.tag
	w.url = "https://example.test/" + w.tag
	w.comment = "COMMENT-" + w.tag
	body := map[string]any{
		"title": "Visible title " + w.tag, "allDay": false,
		"startAt": "2026-07-06T10:00:00+09:00", "endAt": "2026-07-06T11:00:00+09:00",
		"location": w.location, "memo": w.memo, "url": w.url,
		"ownerId":        w.owner.UserID,
		"participantIds": []string{w.attendee.UserID},
	}
	if visibility != "" {
		body["visibility"] = visibility
	}
	helpers.DoJSON(t, http.MethodPost, w.calURL+"/events", w.creator.AccessToken, body, &w.event)
	require.NotEmpty(t, w.event.ID)

	helpers.DoJSON(t, http.MethodPost, w.calURL+"/events/"+w.event.ID+"/activities", w.owner.AccessToken,
		map[string]any{"body": w.comment}, nil)
	helpers.DoJSON(t, http.MethodPost, w.calURL+"/events/"+w.event.ID+"/checklist", w.owner.AccessToken,
		map[string]any{"title": "CHECK-" + w.tag}, nil)
	return w
}

func (w *vmWorld) token(reader string) string {
	switch reader {
	case "owner":
		return w.owner.AccessToken
	case "creator":
		return w.creator.AccessToken
	case "attendee":
		return w.attendee.AccessToken
	case "viewer":
		return w.viewer.AccessToken
	case "manager":
		return w.manager.AccessToken
	}
	panic("unknown reader " + reader)
}

var vmMemberReaders = []string{"owner", "creator", "attendee", "viewer", "manager"}

// vmExpect is the visibility table: the effective value against the reader.
func vmExpect(effective, reader string) vmAccess {
	related := reader == "owner" || reader == "creator" || reader == "attendee"
	switch effective {
	case "public":
		return vmFull
	case "private":
		if related {
			return vmFull
		}
		return vmMasked
	default: // confidential
		if related {
			return vmFull
		}
		return vmNone
	}
}

func (w *vmWorld) secrets() []string {
	return []string{w.location, w.memo, w.url, w.comment, "CHECK-" + w.tag}
}

func vmRequireAbsent(t *testing.T, label string, raw []byte, needles ...string) {
	t.Helper()
	for _, n := range needles {
		require.NotContains(t, string(raw), n, "%s leaks %q", label, n)
	}
}

// The visibility table, end to end: every read path against every reader
// class for each way an event can come to be public, private or confidential.
func TestEventVisibilityMatrix(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	scenarios := []struct {
		name            string
		visibility      string
		calendarDefault string
		effective       string
	}{
		{"explicit public", "public", "", "public"},
		{"explicit private", "private", "", "private"},
		{"explicit confidential", "confidential", "", "confidential"},
		{"default on a public calendar", "default", "public", "public"},
		{"default on a private calendar", "", "private", "private"},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			w := newVMWorld(t, sc.visibility, sc.calendarDefault)

			for _, reader := range vmMemberReaders {
				want := vmExpect(sc.effective, reader)
				tok := w.token(reader)
				label := fmt.Sprintf("%s/%s", sc.effective, reader)

				t.Run("get "+label+" "+want.String(), func(t *testing.T) {
					status, raw := helpers.DoJSONStatus(t, http.MethodGet, w.calURL+"/events/"+w.event.ID, tok, nil)
					if want == vmNone {
						require.Equal(t, http.StatusNotFound, status, string(raw))
						vmRequireAbsent(t, label, raw, w.tag)
						return
					}
					require.Equal(t, http.StatusOK, status, string(raw))
					var got vmEvent
					require.NoError(t, json.Unmarshal(raw, &got))
					vmRequireShape(t, w, want, got, raw)
				})

				t.Run("list "+label+" "+want.String(), func(t *testing.T) {
					out := vmListAs(t, w.calURL, tok)
					if want == vmNone {
						require.Empty(t, out.Items)
						return
					}
					require.Len(t, out.Items, 1)
					vmRequireShape(t, w, want, out.Items[0], nil)
				})

				t.Run("sub-resources "+label+" "+want.String(), func(t *testing.T) {
					for _, sub := range []string{"/activities", "/checklist", "/attachments"} {
						status, raw := helpers.DoJSONStatus(t, http.MethodGet, w.calURL+"/events/"+w.event.ID+sub, tok, nil)
						if want == vmFull {
							require.Equal(t, http.StatusOK, status, "%s: %s", sub, raw)
						} else {
							require.Equal(t, http.StatusNotFound, status, "%s: %s", sub, raw)
							vmRequireAbsent(t, sub, raw, w.secrets()...)
						}
					}
				})

				t.Run("history and activity "+label+" "+want.String(), func(t *testing.T) {
					status, raw := helpers.DoJSONStatus(t, http.MethodGet, w.calURL+"/events/"+w.event.ID+"/history", tok, nil)
					switch want {
					case vmNone:
						require.Equal(t, http.StatusNotFound, status, string(raw))
					default:
						require.Equal(t, http.StatusOK, status, string(raw))
						if want == vmMasked {
							vmRequireAbsent(t, "history", raw, w.secrets()...)
						}
					}

					status, raw = helpers.DoJSONStatus(t, http.MethodGet, w.calURL+"/activity?limit=200", tok, nil)
					require.Equal(t, http.StatusOK, status, string(raw))
					switch want {
					case vmNone:
						vmRequireAbsent(t, "activity", raw, w.event.ID, w.tag)
					case vmMasked:
						require.Contains(t, string(raw), w.tag, "the title survives in the log")
						vmRequireAbsent(t, "activity", raw, w.secrets()...)
					default:
						require.Contains(t, string(raw), w.tag)
					}
				})

				t.Run("export "+label+" "+want.String(), func(t *testing.T) {
					ics := fetchICS(t, w.calURL, tok)
					switch want {
					case vmNone:
						vmRequireAbsent(t, "export", []byte(ics), w.tag)
					case vmMasked:
						require.Contains(t, ics, "SUMMARY:Visible title "+w.tag)
						vmRequireAbsent(t, "export", []byte(ics), w.location, w.memo, w.url)
					default:
						require.Contains(t, ics, "SUMMARY:Visible title "+w.tag)
						require.Contains(t, ics, w.location)
					}
				})
			}

			t.Run("anonymous share "+sc.effective, func(t *testing.T) {
				status, raw := helpers.DoJSONStatus(t, http.MethodGet,
					testServerURL+"/share/"+w.share+"/events?start=2026-07-01&end=2026-07-31", "", nil)
				require.Equal(t, http.StatusOK, status, string(raw))
				var out struct {
					Items []map[string]any `json:"items"`
				}
				require.NoError(t, json.Unmarshal(raw, &out))
				vmRequireAbsent(t, "share", raw, w.memo, w.url, w.comment)

				switch sc.effective {
				case "public":
					require.Len(t, out.Items, 1)
					require.Equal(t, "Visible title "+w.tag, out.Items[0]["title"])
					require.Equal(t, w.location, out.Items[0]["location"])
					require.Equal(t, false, out.Items[0]["allDay"])
				case "private":
					require.Len(t, out.Items, 1)
					require.Nil(t, out.Items[0]["title"], "a private event has no title for an anonymous reader")
					require.Contains(t, []any{nil, ""}, out.Items[0]["location"])
					require.Equal(t, true, out.Items[0]["detailsHidden"])
					require.NotEmpty(t, out.Items[0]["startAt"])
					require.NotEmpty(t, out.Items[0]["endAt"])
					vmRequireAbsent(t, "share", raw, w.tag)
				default:
					require.Empty(t, out.Items)
					vmRequireAbsent(t, "share", raw, w.tag)
				}
			})
		})
	}
}

func vmListAs(t *testing.T, calURL, token string) vmList {
	t.Helper()
	var out vmList
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-07-01&end=2026-07-31", token, nil, &out)
	return out
}

// vmRequireShape checks one event against the access level its reader has.
func vmRequireShape(t *testing.T, w *vmWorld, want vmAccess, got vmEvent, raw []byte) {
	t.Helper()
	require.Equal(t, w.event.ID, got.ID)
	require.Equal(t, "Visible title "+w.tag, got.title(), "title is visible at every level that sees the event")
	require.Equal(t, "2026-07-06T01:00:00Z", got.StartAt)
	require.Equal(t, "2026-07-06T02:00:00Z", got.EndAt)
	require.False(t, got.AllDay)
	require.Equal(t, w.owner.UserID, got.OwnerID)
	require.Len(t, got.Attendees, 1, "the participant list is not a detail")
	require.Equal(t, w.attendee.UserID, got.Attendees[0].UserID)
	if want == vmFull {
		require.Equal(t, w.location, got.Location)
		require.Equal(t, w.memo, got.Memo)
		require.Equal(t, w.url, got.URL)
		require.False(t, got.DetailsHidden)
		return
	}
	require.Empty(t, got.Location)
	require.Empty(t, got.Memo)
	require.Empty(t, got.URL)
	require.True(t, got.DetailsHidden)
	if raw != nil {
		vmRequireAbsent(t, "event", raw, w.location, w.memo, w.url)
	}
}

// A member who may see an event only masked cannot write through the mask:
// touching a hidden field is refused, an ordinary edit is not.
func TestMaskedMemberWritesTheOrdinaryFieldsOnly(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	w := newVMWorld(t, "private", "")
	etag := func(t *testing.T) string {
		t.Helper()
		var e vmEvent
		helpers.DoJSON(t, http.MethodGet, w.calURL+"/events/"+w.event.ID, w.manager.AccessToken, nil, &e)
		require.NotEmpty(t, e.Etag)
		return e.Etag
	}
	patch := func(t *testing.T, body map[string]any) (int, []byte) {
		t.Helper()
		return helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, w.calURL+"/events/"+w.event.ID,
			w.manager.AccessToken, body, map[string]string{"If-Match": etag(t)})
	}

	for name, body := range map[string]map[string]any{
		"location":     {"location": "elsewhere"},
		"memo":         {"memo": "overwritten"},
		"url":          {"url": "https://other.test/"},
		"visibility":   {"visibility": "public"},
		"owner":        {"ownerId": w.manager.UserID},
		"participants": {"participantIds": []string{}},
		"mixed":        {"title": "fine", "memo": "not fine"},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			status, raw := patch(t, body)
			require.Equal(t, http.StatusForbidden, status, string(raw))
			require.Equal(t, "EVENT.DETAILS_HIDDEN", adCode(t, raw))
		})
	}

	// Nothing from the refused attempts landed.
	var stored vmEvent
	helpers.DoJSON(t, http.MethodGet, w.calURL+"/events/"+w.event.ID, w.owner.AccessToken, nil, &stored)
	require.Equal(t, "Visible title "+w.tag, stored.title())
	require.Equal(t, w.location, stored.Location)
	require.Equal(t, w.memo, stored.Memo)
	require.Equal(t, w.url, stored.URL)
	require.Equal(t, "private", vmVisibility(t, w.calURL, w.event.ID, w.owner.AccessToken))
	require.Len(t, stored.Attendees, 1)

	status, raw := patch(t, map[string]any{"title": "Renamed " + w.tag})
	require.Equal(t, http.StatusOK, status, string(raw))
	helpers.DoJSON(t, http.MethodGet, w.calURL+"/events/"+w.event.ID, w.owner.AccessToken, nil, &stored)
	require.Equal(t, "Renamed "+w.tag, stored.title())
	require.Equal(t, w.memo, stored.Memo, "the hidden fields are untouched by a title edit")
}

func vmVisibility(t *testing.T, calURL, id, token string) string {
	t.Helper()
	var e struct {
		Visibility string `json:"visibility"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, token, nil, &e)
	return e.Visibility
}

// A reader for whom a confidential event does not exist gets the answer an
// absent event gets, on every write path.
func TestConfidentialEventDoesNotExistToAnOutsiderOnAnyWritePath(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	w := newVMWorld(t, "confidential", "")
	base := w.calURL + "/events/" + w.event.ID
	anyVersion := map[string]string{"If-Match": "*"}
	tok := w.manager.AccessToken

	missing, _ := helpers.DoJSONStatus(t, http.MethodGet,
		w.calURL+"/events/"+uuid.NewString(), tok, nil)
	require.Equal(t, http.StatusNotFound, missing)

	for _, c := range []struct {
		name, method, path string
		body               any
	}{
		{"patch", http.MethodPatch, base, map[string]any{"title": "x"}},
		{"delete", http.MethodDelete, base, nil},
		{"rsvp", http.MethodPut, base + "/rsvp", map[string]any{"rsvp": "accepted"}},
		{"can-edit", http.MethodPut, base + "/attendees/" + w.attendee.UserID, map[string]any{"canEdit": true}},
		{"restore", http.MethodPost, base + "/restore", nil},
		{"comment", http.MethodPost, base + "/activities", map[string]any{"body": "hello"}},
		{"checklist", http.MethodPost, base + "/checklist", map[string]any{"title": "hello"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			status, raw := helpers.DoJSONStatusWithHeaders(t, c.method, c.path, tok, c.body, anyVersion)
			require.Equal(t, http.StatusNotFound, status, string(raw))
		})
	}

	// The related readers' event is intact.
	var stored vmEvent
	helpers.DoJSON(t, http.MethodGet, base, w.owner.AccessToken, nil, &stored)
	require.Equal(t, "Visible title "+w.tag, stored.title())
}

// A recurring event is judged occurrence by occurrence: being an attendee of
// one occurrence reveals that occurrence and none of its siblings.
func TestConfidentialSeriesIsVisibleOnlyOnTheOccurrencesOneAttends(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, guest, "viewer")

	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "Closed series", "allDay": false, "visibility": "confidential",
		"startAt": "2026-08-03T10:00:00Z", "endAt": "2026-08-03T11:00:00Z",
		"recurrenceRule": map[string]any{"freq": "daily", "interval": 1, "count": 3},
	}, nil)

	mine := vmListIn(t, calURL, owner.AccessToken, "2026-08-01", "2026-08-31")
	require.Len(t, mine.Items, 3)
	var second vmEvent
	for _, e := range mine.Items {
		if strings.HasPrefix(e.StartAt, "2026-08-04") {
			second = e
		}
	}
	require.NotEmpty(t, second.ID)

	require.Empty(t, vmListIn(t, calURL, guest.AccessToken, "2026-08-01", "2026-08-31").Items,
		"before being invited, no occurrence exists for the guest")

	status, raw := helpers.DoJSONStatusWithHeaders(t, http.MethodPatch, calURL+"/events/"+second.ID+"?scope=this",
		owner.AccessToken, map[string]any{"participantIds": []string{guest.UserID}},
		map[string]string{"If-Match": second.Etag})
	require.Equal(t, http.StatusOK, status, string(raw))

	seen := vmListIn(t, calURL, guest.AccessToken, "2026-08-01", "2026-08-31")
	require.Len(t, seen.Items, 1)
	require.True(t, strings.HasPrefix(seen.Items[0].StartAt, "2026-08-04"))
	require.Equal(t, second.ID, seen.Items[0].ID)
	require.False(t, seen.HasMore)

	status, _ = helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+second.ID, guest.AccessToken, nil)
	require.Equal(t, http.StatusOK, status)
	for _, e := range mine.Items {
		if e.ID == second.ID {
			continue
		}
		status, raw = helpers.DoJSONStatus(t, http.MethodGet, calURL+"/events/"+e.ID, guest.AccessToken, nil)
		require.Equal(t, http.StatusNotFound, status, "sibling %s: %s", e.StartAt, raw)
	}
}

func vmListIn(t *testing.T, calURL, token, start, end string) vmList {
	t.Helper()
	var out vmList
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start="+start+"&end="+end, token, nil, &out)
	return out
}

// Hidden occurrences are dropped before the listing's cap is applied: a window
// holding more hidden rows than the cap and one visible row returns that row
// and does not claim there is more.
func TestHiddenRowsDoNotUseUpTheListingCap(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	reader := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, reader, "viewer")

	conn, err := testDB.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(t.Context(), `SET SESSION cte_max_recursion_depth = 10000`)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO calendar_events
		(public_id, workspace_id, calendar_id, kind, visibility, title, all_day, start_at, end_at, timezone,
		 owner_user_id, created_by_user_id)
		WITH RECURSIVE n (i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 5001)
		SELECT UUID_TO_BIN(UUID()), c.workspace_id, c.id, 'event', 'confidential', CONCAT('hidden ', n.i), FALSE,
		       TIMESTAMP('2026-10-01 00:00:00') + INTERVAL n.i MINUTE,
		       TIMESTAMP('2026-10-01 00:00:00') + INTERVAL n.i MINUTE + INTERVAL 1 MINUTE,
		       'UTC', u.id, u.id
		FROM n JOIN calendars c ON c.public_id = UUID_TO_BIN(?) JOIN users u ON u.public_id = UUID_TO_BIN(?)`,
		owner.CalendarID, owner.UserID)
	require.NoError(t, err)

	var visible vmEvent
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
		"title": "The one you can see", "allDay": false, "visibility": "public",
		"startAt": "2026-10-06T10:00:00Z", "endAt": "2026-10-06T11:00:00Z"}, &visible)

	got := vmListIn(t, calURL, reader.AccessToken, "2026-10-01", "2026-10-07")
	require.Len(t, got.Items, 1)
	require.Equal(t, visible.ID, got.Items[0].ID)
	require.False(t, got.HasMore, "rows the reader cannot see must not make the listing look truncated")
}

// An import that matches a UID held by an event the importer cannot see
// neither updates it nor reports it as a duplicate they could learn from.
func TestImportSkipsAUIDHeldByAnInvisibleEvent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	importer := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, importer, "editor")
	require.Equal(t, http.StatusOK, promote(t, calURL, owner.AccessToken, importer, "manager"))

	id, _ := ctInsertEvent(t, owner.CalendarID, owner.UserID, ctEventRow{
		Title: "Sealed", Visibility: "confidential", SourceUID: "sealed-uid@example.test",
		Start: "2026-07-10 10:00:00", End: "2026-07-10 11:00:00"})

	ics := strings.Join([]string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//test//EN", "BEGIN:VEVENT",
		"UID:sealed-uid@example.test", "DTSTAMP:20260101T000000Z",
		"DTSTART:20260710T100000Z", "DTEND:20260710T110000Z", "SUMMARY:Overwrite attempt",
		"END:VEVENT", "END:VCALENDAR", ""}, "\r\n")
	var res importResult
	helpers.DoJSON(t, http.MethodPost, calURL+"/import", importer.AccessToken, map[string]any{"ics": ics}, &res)
	require.Equal(t, 0, res.Imported)
	require.Equal(t, 1, res.Skipped, "counted as a skip, like any UID that is not theirs to touch")

	var stored vmEvent
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, owner.AccessToken, nil, &stored)
	require.Equal(t, "Sealed", stored.title())
	require.Empty(t, vmListIn(t, calURL, importer.AccessToken, "2026-07-01", "2026-07-31").Items)
}
