package e2e

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"testing"

	goical "github.com/emersion/go-ical"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// davVEvents keys each VEVENT of cal by UID and RECURRENCE-ID and renders the
// rest of it, DTSTAMP and UID left out, as sorted text.
func davVEvents(t *testing.T, cal *goical.Calendar, into map[string]string) {
	t.Helper()
	for _, child := range cal.Children {
		if child.Name != goical.CompEvent {
			continue
		}
		key := child.Props.Get(goical.PropUID).Value
		if rid := child.Props.Get(goical.PropRecurrenceID); rid != nil {
			key += "|" + rid.Value
		}
		_, dup := into[key]
		require.False(t, dup, "VEVENT %s appears twice", key)
		into[key] = davComparable(child)
	}
}

func davComparable(c *goical.Component) string {
	var lines []string
	for name, props := range c.Props {
		if name == goical.PropDateTimeStamp || name == goical.PropUID {
			continue
		}
		for _, p := range props {
			var params []string
			for k, v := range p.Params {
				params = append(params, k+"="+strings.Join(v, ","))
			}
			sort.Strings(params)
			lines = append(lines, name+";"+strings.Join(params, ";")+":"+p.Value)
		}
	}
	for _, child := range c.Children {
		lines = append(lines, "BEGIN:"+child.Name+"\n"+davComparable(child)+"\nEND:"+child.Name)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestDAVVisibilityMatchesTheExport verifies that every reader sees over
// CalDAV exactly what their own export holds, VEVENT by VEVENT: masked where
// the export masks, absent where it is absent, a changed occurrence on its own
// where only it is visible.
func TestDAVVisibilityMatchesTheExport(t *testing.T) {
	bootstrap(t)
	t.Parallel()
	ctx := context.Background()

	owner := helpers.NewTenant(t, testServerURL)
	member := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID
	joinAs(t, calURL, owner.AccessToken, member, "editor")

	series := func(title, visibility string, participants []string, overrideVisibility string) string {
		t.Helper()
		if participants == nil {
			participants = []string{}
		}
		var evt struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken, map[string]any{
			"title": title, "allDay": false, "timezone": "Asia/Tokyo",
			"startAt": "2026-06-05T10:00:00+09:00", "endAt": "2026-06-05T11:00:00+09:00",
			"location": "Room 4", "memo": "notes for " + title, "url": "https://example.com/" + title,
			"visibility": visibility, "participantIds": participants,
			"notificationOffset": 15,
			"recurrenceRule":     map[string]any{"freq": "weekly", "interval": 1, "byDay": []string{"FR"}, "until": "2026-06-30T23:59:59+09:00"},
		}, &evt)
		override := map[string]any{
			"title":   title + " (moved)",
			"startAt": "2026-06-12T15:00:00+09:00", "endAt": "2026-06-12T16:00:00+09:00",
		}
		if overrideVisibility != "" {
			override["visibility"] = overrideVisibility
		}
		doAnyVersion(t, http.MethodPatch, calURL+"/events/"+evt.ID+"_20260612?scope=this", owner.AccessToken, override, nil)
		return evt.ID
	}

	public := series("public", "public", nil, "")
	private := series("private", "private", nil, "")
	confidential := series("confidential", "confidential", nil, "")
	onlyOverride := series("only-override", "confidential", nil, "public")
	attended := series("attended", "confidential", []string{member.UserID}, "")

	for _, c := range []struct {
		name    string
		reader  *helpers.TestTenant
		visible []string
	}{
		{"party", owner, []string{public, private, confidential, onlyOverride, attended}},
		{"member", member, []string{public, private, onlyOverride, attended}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := newDAVAccount(t, c.reader)
			calPath := a.calendar(owner.CalendarID)

			want := map[string]string{}
			exported, err := goical.NewDecoder(strings.NewReader(fetchICS(t, calURL, c.reader.AccessToken))).Decode()
			require.NoError(t, err)
			davVEvents(t, exported, want)

			objs := a.objects(t, calPath)
			wantPaths := map[string]bool{}
			for _, id := range c.visible {
				wantPaths[davObjectPath(calPath, id)] = true
			}
			require.Equal(t, wantPaths, davPaths(objs))

			got := map[string]string{}
			for _, o := range objs {
				fetched, err := a.client.GetCalendarObject(ctx, o.Path)
				require.NoError(t, err)
				require.Equal(t, o.ETag, fetched.ETag, o.Path)
				davVEvents(t, fetched.Data, got)
			}
			require.Equal(t, want, got)
		})
	}

	// What a reader cannot see is not there to fetch by name either.
	m := newDAVAccount(t, member)
	status, _, _ := m.do(t, http.MethodGet, davObjectPath(m.calendar(owner.CalendarID), confidential), nil, "")
	require.Equal(t, http.StatusNotFound, status)
}
