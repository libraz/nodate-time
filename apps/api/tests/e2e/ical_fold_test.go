package e2e

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// A title long enough to be folded several times, written in characters that
// are three and four octets wide so the 75-octet boundary cannot help but land
// inside one of them.
const foldedTitle = "年次総会の議事録と来期予算の確認、および新体制の発表について🗼🙂" +
	"関係各位のご出席をお願いいたします。会場は東京都渋谷区の本社大会議室です🙂"

// An export is a backup, so what it is worth is decided by whether anything
// else can read it. Folding at a fixed octet count without regard for where a
// character begins writes a byte sequence that is not a character, and the
// title is mangled from the moment the file is written -- in the copy the
// owner keeps.
func TestExportedICSFoldsWithoutBreakingCharacters(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Folding")

	helpers.DoJSON(t, http.MethodPost, calURL+"/events", tt.AccessToken,
		map[string]any{
			"title":    foldedTitle,
			"allDay":   false,
			"startAt":  "2026-06-11T09:00:00+09:00",
			"endAt":    "2026-06-11T10:00:00+09:00",
			"location": foldedTitle,
			"memo":     foldedTitle,
		}, nil)

	ics := fetchICS(t, calURL, tt.AccessToken)
	for i, line := range strings.Split(strings.TrimSuffix(ics, "\r\n"), "\r\n") {
		require.True(t, utf8.ValidString(line),
			"line %d ends inside a character, so every reader but this one sees mangled text: %q",
			i, line)
		require.LessOrEqual(t, len(line), 75,
			"line %d is over the octet limit RFC 5545 3.1 sets: %q", i, line)
	}
}

// The reader has to give back exactly what the writer folded, including for
// titles whose fold points fall inside a character. A round trip that changes
// the text is a backup that restores something else.
func TestFoldedTextSurvivesExportAndImport(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	sourceURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Folding source")

	helpers.DoJSON(t, http.MethodPost, sourceURL+"/events", tt.AccessToken,
		map[string]any{
			"title":   foldedTitle,
			"allDay":  false,
			"startAt": "2026-06-12T09:00:00+09:00",
			"endAt":   "2026-06-12T10:00:00+09:00",
			"memo":    foldedTitle,
		}, nil)

	targetURL := testServerURL + "/calendars/" + newCalendar(t, tt, "Folding target")
	var res importResult
	helpers.DoJSON(t, http.MethodPost, targetURL+"/import", tt.AccessToken,
		map[string]any{"ics": fetchICS(t, sourceURL, tt.AccessToken)}, &res)
	require.Equal(t, 1, res.Imported)

	var listed []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet,
		targetURL+"/events?start=2026-06-01&end=2026-06-30", tt.AccessToken, nil, &listed)
	require.Len(t, listed, 1)
	require.Equal(t, foldedTitle, listed[0].Title,
		"the title has to come back the way it went out, character for character")

	var got struct {
		Memo string `json:"memo"`
	}
	helpers.DoJSON(t, http.MethodGet, targetURL+"/events/"+listed[0].ID, tt.AccessToken, nil, &got)
	require.Equal(t, foldedTitle, got.Memo)
}
