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

// The reminders an import keeps, the ones the events API accepts and the ones
// the web picker offers are one set. A reminder outside it is invisible in the
// picker, so the import drops it and the API refuses it; if the copies drift,
// imported reminders are silently lost or kept where nothing can show them.
func TestReminderOffsetsAreTheSameForImportTheAPIAndTheClient(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	web := webNotificationOffsets(t)
	candidates := map[int]bool{1: true, 45: true, 90: true, 180: true, 720: true, 4320: true, 10080: true}
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
		require.Equal(t, web[v], status < 300, "API with offset %d: %d %s", v, status, raw)

		fmt.Fprintf(&file, "BEGIN:VEVENT\r\nUID:offset-%d@example.com\r\nSUMMARY:Offset %d\r\n"+
			"DTSTART:20261005T100000Z\r\nDTEND:20261005T110000Z\r\n"+
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:x\r\nTRIGGER:-PT%dM\r\nEND:VALARM\r\nEND:VEVENT\r\n",
			v, v, v)
	}
	file.WriteString("END:VCALENDAR\r\n")
	require.Equal(t, len(offsets), importOK(t, importURL, tt.AccessToken, file.String()).Imported)

	var imported []struct {
		Title              string `json:"title"`
		NotificationOffset *int   `json:"notificationOffset"`
	}
	helpers.DoJSON(t, http.MethodGet, importURL+"/events?start=2026-10-05&end=2026-10-05&tz=UTC",
		tt.AccessToken, nil, &imported)
	require.Len(t, imported, len(offsets))
	for _, e := range imported {
		v, err := strconv.Atoi(strings.TrimPrefix(e.Title, "Offset "))
		require.NoError(t, err)
		require.Equal(t, web[v], e.NotificationOffset != nil, "import with offset %d", v)
	}
}
