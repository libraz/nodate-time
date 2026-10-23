package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// Import writes events many to a transaction. What a file gets back must not
// depend on where in it the transactions happen to start and end: a refused
// row costs only itself, and two events sharing a UID are told apart the same
// way whether or not a commit falls between them.
func TestICalImportOutcomesDoNotDependOnWhereTransactionsSplitTheFile(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calID := newCalendar(t, tt, "Chunked import")
	calURL := testServerURL + "/calendars/" + calID

	const total = 250
	twins := map[int]bool{100: true, 121: true, 211: true}
	var b strings.Builder
	for i := range total {
		uid := fmt.Sprintf("chunk-%d@example.com", i)
		if twins[i] {
			uid = fmt.Sprintf("chunk-%d@example.com", i-1)
		}
		summary := fmt.Sprintf("SUMMARY:Event %d", i)
		switch i {
		case 5:
			// A changed occurrence placed well before the series it belongs to.
			b.WriteString(vevent("UID:series@example.com", "SUMMARY:Moved",
				"RECURRENCE-ID:20260608T100000Z",
				"DTSTART:20260608T150000Z", "DTEND:20260608T160000Z"))
			continue
		case 150:
			summary = "SUMMARY:" + strings.Repeat("A", 200000)
		case 230:
			b.WriteString(vevent("UID:series@example.com", "SUMMARY:Weekly",
				"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z",
				"RRULE:FREQ=WEEKLY;COUNT=2"))
			continue
		}
		b.WriteString(vevent("UID:"+uid, summary,
			"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
	}
	ics := wrap(b.String())

	require.Equal(t, importReport{Imported: total - 1, Failed: 1, Rejected: 1},
		importOK(t, calURL, tt.AccessToken, ics),
		"only the refused event may go missing, and every twin lands")
	require.Len(t, listHostile(t, calURL, tt.AccessToken), total-1,
		"the calendar must hold exactly what the response said it imported")

	// The second of each twin was written without an identity, so it is the
	// only part of the file a second upload writes again.
	require.Equal(t, importReport{
		Imported: len(twins), Duplicates: total - 1 - len(twins), Failed: 1, Rejected: 1,
	}, importOK(t, calURL, tt.AccessToken, ics))

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)
}

// A file at the event cap is one the endpoint accepts, so its report has to
// come back inside the write timeout the server is deployed with. Past it the
// events are stored but the response is cut off, and the caller never learns
// what landed.
func TestICalImportAtTheEventCapAnswersWithinTheWriteTimeout(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calID := newCalendar(t, tt, "Import at the cap")
	calURL := testServerURL + "/calendars/" + calID

	const capacity = 5000
	var b strings.Builder
	for i := range capacity {
		b.WriteString(vevent(fmt.Sprintf("UID:cap-%d@example.com", i),
			fmt.Sprintf("SUMMARY:Event %d", i),
			"DTSTART:20260601T100000Z", "DTEND:20260601T110000Z"))
	}

	began := time.Now()
	res := importOK(t, calURL, tt.AccessToken, wrap(b.String()))
	elapsed := time.Since(began)
	require.Equal(t, importReport{Imported: capacity}, res)
	require.Less(t, elapsed, app.DefaultTimeouts().Write)
	t.Logf("%d events imported in %s", capacity, elapsed)

	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL, tt.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status)
}
