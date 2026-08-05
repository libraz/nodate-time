package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// publicShareToken publishes a calendar read-only and returns the link token.
func publicShareToken(t *testing.T, calURL, ownerToken string) string {
	t.Helper()
	var inv struct {
		Token string `json:"token"`
	}
	isPublic := true
	helpers.DoJSON(t, http.MethodPost, calURL+"/invites", ownerToken,
		map[string]any{"role": "viewer", "isPublic": isPublic}, &inv)
	require.NotEmpty(t, inv.Token)
	return inv.Token
}

type publicEvent struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Private bool   `json:"private"`
}

// TestVisibilityGovernsThePublicFeed verifies that an event's visibility is
// settable, is honoured by the anonymous share feed, and that the two levels
// differ in kind: private withholds the details, confidential withholds the
// event.
func TestVisibilityGovernsThePublicFeed(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	token := publicShareToken(t, calURL, owner.AccessToken)

	create := func(title, visibility string, day string) string {
		var evt struct {
			ID         string `json:"id"`
			Visibility string `json:"visibility"`
		}
		body := map[string]any{
			"title":   title,
			"allDay":  false,
			"startAt": day + "T10:00:00+09:00",
			"endAt":   day + "T11:00:00+09:00",
		}
		if visibility != "" {
			body["visibility"] = visibility
		}
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, body, &evt)
		if visibility != "" {
			require.Equal(t, visibility, evt.Visibility)
		}
		return evt.ID
	}

	openID := create("Team lunch", "", "2026-07-06")
	privateID := create("Counselling", "private", "2026-07-07")
	create("Legal consultation", "confidential", "2026-07-08")

	var feed []publicEvent
	helpers.DoJSON(t, http.MethodGet,
		testServerURL+"/share/"+token+"/events?start=2026-07-01&end=2026-07-31", "", nil, &feed)

	byID := map[string]publicEvent{}
	for _, e := range feed {
		byID[e.ID] = e
	}
	require.Len(t, feed, 2, "the confidential event must not be published at all")

	require.Equal(t, "Team lunch", byID[openID].Title)
	require.False(t, byID[openID].Private)

	// Private keeps the time but nothing that says what it is.
	require.Contains(t, byID, privateID)
	require.Empty(t, byID[privateID].Title)
	require.True(t, byID[privateID].Private)

	// Nothing anywhere in the response names the confidential event.
	for _, e := range feed {
		require.NotEqual(t, "Legal consultation", e.Title)
	}

	// Members still see everything.
	var mine []struct {
		Title      string `json:"title"`
		Visibility string `json:"visibility"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/events?start=2026-07-01&end=2026-07-31",
		owner.AccessToken, nil, &mine)
	require.Len(t, mine, 3)
}

// TestOmittedFieldsSurviveAnEdit verifies that a client which only means to
// move an event does not reset the axes it did not mention.
func TestOmittedFieldsSurviveAnEdit(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":       "Out of office",
			"allDay":      false,
			"startAt":     "2026-07-14T10:00:00+09:00",
			"endAt":       "2026-07-14T11:00:00+09:00",
			"showAs":      "oof",
			"flexibility": "negotiable",
			"visibility":  "private",
		}, &evt)

	var moved struct {
		ShowAs      string `json:"showAs"`
		Flexibility string `json:"flexibility"`
		Visibility  string `json:"visibility"`
	}
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID, owner.AccessToken,
		eventBody("Out of office", "2026-07-15T10:00:00+09:00", "2026-07-15T11:00:00+09:00"), &moved)

	require.Equal(t, "oof", moved.ShowAs)
	require.Equal(t, "negotiable", moved.Flexibility)
	require.Equal(t, "private", moved.Visibility)

	// Naming a value still changes it.
	body := eventBody("Out of office", "2026-07-15T10:00:00+09:00", "2026-07-15T11:00:00+09:00")
	body["showAs"] = "free"
	body["visibility"] = "default"
	var changed struct {
		ShowAs     string `json:"showAs"`
		Visibility string `json:"visibility"`
	}
	helpers.DoJSON(t, http.MethodPut, calURL+"/events/"+evt.ID, owner.AccessToken, body, &changed)
	require.Equal(t, "free", changed.ShowAs)
	require.Equal(t, "default", changed.Visibility)
}

// TestExportCarriesTheClassification verifies that the classification survives
// a round trip through an .ics file rather than being silently dropped.
func TestExportCarriesTheClassification(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		map[string]any{
			"title":      "Counselling",
			"allDay":     false,
			"startAt":    "2026-07-20T10:00:00+09:00",
			"endAt":      "2026-07-20T11:00:00+09:00",
			"visibility": "private",
			"showAs":     "free",
		}, nil)

	ics := fetchICS(t, calURL, owner.AccessToken)
	require.Contains(t, ics, "CLASS:PRIVATE")
	require.Contains(t, ics, "TRANSP:TRANSPARENT")

	target := helpers.NewTenant(t, testServerURL)
	targetURL := testServerURL + "/calendars/" + target.CalendarID
	var res importResult
	helpers.DoJSON(t, http.MethodPost, targetURL+"/import", target.AccessToken,
		map[string]any{"ics": ics}, &res)
	require.Equal(t, 1, res.Imported)

	var imported []struct {
		Title      string `json:"title"`
		Visibility string `json:"visibility"`
		ShowAs     string `json:"showAs"`
	}
	helpers.DoJSON(t, http.MethodGet, targetURL+"/events?start=2026-07-01&end=2026-07-31",
		target.AccessToken, nil, &imported)
	require.Len(t, imported, 1)
	require.Equal(t, "private", imported[0].Visibility)
	require.Equal(t, "free", imported[0].ShowAs)
	require.True(t, strings.Contains(imported[0].Title, "Counselling"))
}
