package timetree

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTT serves the TimeTree endpoints the client uses.
func fakeTT(t *testing.T, handler http.HandlerFunc) (*client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := newClient(srv.URL)
	require.NoError(t, err)
	return c, srv
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestLoginSendsTheDocumentedRequestAndKeepsTheSession(t *testing.T) {
	var seen *http.Request
	var body map[string]string
	c, _ := fakeTT(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/email/signin":
			seen = r.Clone(r.Context())
			_ = json.NewDecoder(r.Body).Decode(&body)
			http.SetCookie(w, &http.Cookie{Name: "_session_id", Value: "abc", Path: "/"})
			writeJSON(w, 200, `{}`)
		case "/api/v1/calendars":
			ck, err := r.Cookie("_session_id")
			if err != nil || ck.Value != "abc" {
				writeJSON(w, 401, `{}`)
				return
			}
			writeJSON(w, 200, `{"calendars":[]}`)
		}
	})
	require.NoError(t, c.login(context.Background(), "me@example.com", "secret"))
	assert.Equal(t, http.MethodPut, seen.Method)
	assert.Equal(t, "application/json", seen.Header.Get("Content-Type"))
	assert.Equal(t, "web/2.1.0/ja", seen.Header.Get("X-Timetreea"))
	assert.Equal(t, "me@example.com", body["uid"])
	assert.Equal(t, "secret", body["password"])
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), body["uuid"])

	cals, err := c.calendars(context.Background())
	require.NoError(t, err, "the session cookie is sent on later requests")
	assert.Empty(t, cals)
}

func TestLoginFailureReportsTheTimeTreeErrorCode(t *testing.T) {
	for _, code := range []int{-702, -495} {
		c, _ := fakeTT(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 401, `{"code":`+itoa(code)+`,"message":"no"}`)
		})
		err := c.login(context.Background(), "me@example.com", "pw")
		require.Error(t, err)
		assert.Contains(t, err.Error(), itoa(code))
		assert.NotContains(t, err.Error(), "pw")
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestLoginWithoutAnErrorBodyOrSessionCookieFails(t *testing.T) {
	c, _ := fakeTT(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 500, `oops`) })
	assert.ErrorContains(t, c.login(context.Background(), "e", "p"), "HTTP 500")
	c, _ = fakeTT(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, `{}`) })
	assert.ErrorContains(t, c.login(context.Background(), "e", "p"), "session cookie")
}

func TestCalendarsDropsDeactivatedOnes(t *testing.T) {
	c, _ := fakeTT(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "0", r.URL.Query().Get("since"))
		writeJSON(w, 200, `{"calendars":[
			{"id":1,"name":"live","alias_code":"a","deactivated_at":null},
			{"id":2,"name":"gone","alias_code":"b","deactivated_at":1700000000000},
			{"id":"3","name":"no field"}]}`)
	})
	cals, err := c.calendars(context.Background())
	require.NoError(t, err)
	require.Len(t, cals, 2)
	assert.Equal(t, flexID("1"), cals[0].ID)
	assert.Equal(t, flexID("3"), cals[1].ID)
}

func TestLabels(t *testing.T) {
	c, _ := fakeTT(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/calendar/42/labels", r.URL.Path)
		writeJSON(w, 200, `{"calendar_labels":[{"id":7,"calendar_id":42,"name":"花子","color":"x"}]}`)
	})
	labels, err := c.labels(context.Background(), "42")
	require.NoError(t, err)
	assert.Equal(t, []ttLabel{{ID: "7", Name: "花子"}}, labels)
}

func TestEventsFollowsTheChunkCursor(t *testing.T) {
	var queries []string
	c, _ := fakeTT(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/calendar/42/events/sync", r.URL.Path)
		queries = append(queries, r.URL.RawQuery)
		switch r.URL.Query().Get("since") {
		case "":
			writeJSON(w, 200, `{"events":[{"id":"a","title":"A","start_at":1}],"since":100,"chunk":true}`)
		case "100":
			writeJSON(w, 200, `{"events":[{"id":"b","title":"B"}],"since":200,"chunk":true}`)
		case "200":
			writeJSON(w, 200, `{"events":[{"id":"c","title":"C","recurrences":["RRULE:FREQ=DAILY"]}],"since":300,"chunk":false}`)
		}
	})
	events, err := c.events(context.Background(), "42")
	require.NoError(t, err)
	assert.Equal(t, []string{"", "since=100", "since=200"}, queries)
	require.Len(t, events, 3)
	assert.Equal(t, "c", events[2].ID)
	assert.Equal(t, []string{"RRULE:FREQ=DAILY"}, events[2].Recurrences)
	assert.Zero(t, events[1].StartAt, "optional fields may be missing")
}

func TestEventsStopsOnAStuckCursor(t *testing.T) {
	c, _ := fakeTT(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"events":[],"since":5,"chunk":true}`)
	})
	_, err := c.events(context.Background(), "1")
	assert.ErrorContains(t, err, "did not advance")
}

func TestFetchAbortsOnNon200OrMissingKeys(t *testing.T) {
	c, _ := fakeTT(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 503, `{}`) })
	_, err := c.calendars(context.Background())
	assert.ErrorContains(t, err, "HTTP 503")
	_, err = c.labels(context.Background(), "1")
	assert.ErrorContains(t, err, "HTTP 503")
	_, err = c.events(context.Background(), "1")
	assert.ErrorContains(t, err, "HTTP 503")

	c, _ = fakeTT(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, `{"other":[]}`) })
	_, err = c.calendars(context.Background())
	assert.ErrorContains(t, err, `no "calendars"`)
	_, err = c.labels(context.Background(), "1")
	assert.ErrorContains(t, err, `no "calendar_labels"`)
	_, err = c.events(context.Background(), "1")
	assert.ErrorContains(t, err, `no "events"`)

	c, _ = fakeTT(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, `not json`) })
	_, err = c.calendars(context.Background())
	assert.Error(t, err)
}

func TestFetchStopsBeforeAnyWriteWhenLoginFails(t *testing.T) {
	_, srv := fakeTT(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 401, `{"code":-702}`) })
	_, err := fetch(context.Background(), Options{BaseURL: srv.URL, Email: "e", Password: "p"})
	assert.ErrorContains(t, err, "-702")
}

func TestLatestByIDKeepsTheLastCopyInTheFirstPlace(t *testing.T) {
	in := []ttEvent{{ID: "a", Title: "a1"}, {ID: "b"}, {ID: "a", Title: "a2"}, {ID: ""}, {ID: ""}, {ID: "a", Title: "a3"}}
	out, repeated := latestByID(in)
	assert.Equal(t, 2, repeated)
	assert.Equal(t, []ttEvent{{ID: "a", Title: "a3"}, {ID: "b"}, {ID: ""}, {ID: ""}}, out, "an empty ID is never folded")
}
