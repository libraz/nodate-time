package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// An internationalised URL is a URL. RFC 3987 lets one carry non-ASCII
// directly, a browser shows and copies it that way, and any calendar written
// outside the Latin-1 world eventually contains one.
//
// What makes it worth a test of its own is where the loss lands. Import writes
// each event in its own transaction, so a URL the column cannot hold does not
// cost the URL -- the insert rolls back and the entire event vanishes, counted
// as failed. A file with nothing wrong with it arrives short, and what it was
// short by is an optional property nobody would think to look at.
const utf8EventURL = "https://example.com/会議/2026?題名=年次総会"

// utf8URLEvent renders a VEVENT that is unremarkable apart from its URL.
func utf8URLEvent() string {
	return vevent(
		"UID:utf8-url@example.com",
		"SUMMARY:Annual meeting",
		"DTSTART:20260610T090000Z",
		"DTEND:20260610T100000Z",
		"URL:"+utf8EventURL,
	)
}

func TestImportKeepsAnEventCarryingANonASCIIURL(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := hostileCalendar(t, tt, "Internationalised links")

	res := importOK(t, calURL, tt.AccessToken, wrap(utf8URLEvent()+oneGoodEvent()))
	require.Zero(t, res.Failed,
		"an event must not be lost over the one property that happened to be non-ASCII")
	require.Equal(t, 2, res.Imported)

	listed := listHostile(t, calURL, tt.AccessToken)
	t.Cleanup(func() { deleteEverything(t, calURL, tt.AccessToken, listed) })

	var id string
	for _, e := range listed {
		if e.Title == "Annual meeting" {
			id = e.ID
		}
	}
	require.NotEmpty(t, id, "the imported event must be on the calendar")

	var got struct {
		URL string `json:"url"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+id, tt.AccessToken, nil, &got)
	require.Equal(t, utf8EventURL, got.URL,
		"the link must come back the way it went in, not transliterated or truncated")

	require.Contains(t, fetchICS(t, calURL, tt.AccessToken), "URL:"+utf8EventURL,
		"an export that cannot carry the link back out is not a backup of it")
}

// The same column serves anyone pasting a link into the event form, where the
// failure is a refused save rather than a missing row.
func TestANonASCIIURLSurvivesBeingTypedIntoAnEvent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	var evt struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	status, raw := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   "Annual meeting",
			"allDay":  false,
			"startAt": "2026-06-11T09:00:00+09:00",
			"endAt":   "2026-06-11T10:00:00+09:00",
			"url":     utf8EventURL,
		})
	require.Equal(t, http.StatusCreated, status,
		"a link the user can paste must be a link the event can hold: %s", cut(string(raw), 400))
	require.NoError(t, json.Unmarshal(raw, &evt))
	require.Equal(t, utf8EventURL, evt.URL)

	var got struct {
		URL string `json:"url"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events/"+evt.ID, tt.AccessToken, nil, &got)
	require.Equal(t, utf8EventURL, got.URL, "the stored link must not lose its non-ASCII part")
	require.True(t, strings.Contains(got.URL, "年次総会"),
		"a lossy charset conversion shows up as replacement characters, not an error")
}
