package e2e

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// maxNotificationOffset is the longest reminder, in minutes, the API accepts.
const maxNotificationOffset = 10080

// webNotificationOffsets reads the reminder offsets the web picker offers
// straight from its source, so the test follows the client rather than a copy.
func webNotificationOffsets(t *testing.T) map[int]bool {
	t.Helper()
	src, err := os.ReadFile("../../../web/src/types/calendar.ts")
	require.NoError(t, err)
	block := regexp.MustCompile(`(?s)export const NOTIFICATION_OFFSETS = \[(.*?)\] as const;`).FindSubmatch(src)
	require.NotNil(t, block, "NOTIFICATION_OFFSETS not found in the web client")
	out := map[int]bool{}
	for _, m := range regexp.MustCompile(`value: (\d+)`).FindAllSubmatch(block[1], -1) {
		n, err := strconv.Atoi(string(m[1]))
		require.NoError(t, err)
		out[n] = true
	}
	require.NotEmpty(t, out)
	return out
}

// The reminders an import keeps and the ones the events API accepts are one
// set: every offset from 0 to 10080 minutes, including the presets the web
// picker offers. A value past the range is refused by the API and left off by
// the import, which counts it; if the two drift, imported reminders are
// silently lost or kept where the API would have refused them.
func TestReminderOffsetsAreTheSameForImportTheAPIAndTheClient(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	web := webNotificationOffsets(t)
	candidates := map[int]bool{0: true, 1: true, 45: true, 90: true, 180: true, 720: true, 4320: true,
		10080: true, 10081: true, 20160: true}
	for v := range web {
		candidates[v] = true
	}
	offsets := make([]int, 0, len(candidates))
	for v := range candidates {
		offsets = append(offsets, v)
	}
	sort.Ints(offsets)

	tt := helpers.NewTenant(t, testServerURL)
	apiURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Reminders by API")
	importURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Reminders by import")

	var file strings.Builder
	file.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n")
	for _, v := range offsets {
		status, raw := helpers.DoJSONStatus(t, http.MethodPost, apiURL+"/events", tt.AccessToken, map[string]any{
			"title": fmt.Sprintf("Offset %d", v), "allDay": false,
			"startAt": "2026-10-05T10:00:00Z", "endAt": "2026-10-05T11:00:00Z",
			"notificationOffset": v,
		})
		require.Equal(t, v <= maxNotificationOffset, status < 300, "API with offset %d: %d %s", v, status, raw)

		fmt.Fprintf(&file, "BEGIN:VEVENT\r\nUID:offset-%d@example.com\r\nSUMMARY:Offset %d\r\n"+
			"DTSTART:20261005T100000Z\r\nDTEND:20261005T110000Z\r\n"+
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:x\r\nTRIGGER:-PT%dM\r\nEND:VALARM\r\nEND:VEVENT\r\n",
			v, v, v)
	}
	file.WriteString("END:VCALENDAR\r\n")
	outOfRange := 0
	for _, v := range offsets {
		if v > maxNotificationOffset {
			outOfRange++
		}
	}
	res := importOK(t, importURL, tt.AccessToken, file.String())
	require.Equal(t, len(offsets), res.Imported)
	require.Equal(t, outOfRange, res.DroppedReminders, "a reminder past the range is counted, not lost quietly")

	var imported []struct {
		Title              string `json:"title"`
		NotificationOffset *int   `json:"notificationOffset"`
	}
	listEventItems(t, importURL+"/events?start=2026-10-05&end=2026-10-05&tz=UTC",
		tt.AccessToken, &imported)
	require.Len(t, imported, len(offsets))
	for _, e := range imported {
		v, err := strconv.Atoi(strings.TrimPrefix(e.Title, "Offset "))
		require.NoError(t, err)
		if v > maxNotificationOffset {
			require.Nil(t, e.NotificationOffset, "import with offset %d", v)
			continue
		}
		require.NotNil(t, e.NotificationOffset, "import with offset %d", v)
		require.Equal(t, v, *e.NotificationOffset)
	}
	for v := range web {
		require.LessOrEqual(t, v, maxNotificationOffset, "a preset the picker offers must be one the API accepts")
	}
}
