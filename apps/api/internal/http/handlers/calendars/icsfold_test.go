package calendars

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// foldedLines splits what writeFolded produced back into the physical lines a
// reader sees. The trailing break is dropped so an empty last element does not
// count as a line.
func foldedLines(line string) []string {
	var b strings.Builder
	writeFolded(&b, line)
	return strings.Split(strings.TrimSuffix(b.String(), "\r\n"), "\r\n")
}

// RFC 5545 §3.1 asks for two things at once: no line longer than 75 octets,
// and no multi-octet character split by the fold. Counting characters instead
// of octets breaks the first, and counting octets without regard for where a
// character begins breaks the second -- the file then holds a byte sequence
// that is not a character at all, and every reader but this one sees a
// mangled title.
//
// The padding is grown one octet at a time so the boundary lands at every
// offset inside a three-octet character and a four-octet one.
func TestFoldingKeepsEveryLineShortAndEveryCharacterWhole(t *testing.T) {
	for pad := 55; pad <= 80; pad++ {
		line := "SUMMARY:" + strings.Repeat("a", pad) +
			strings.Repeat("あ", 12) + strings.Repeat("🙂", 6)

		lines := foldedLines(line)
		for i, out := range lines {
			require.LessOrEqual(t, len(out), 75,
				"pad %d line %d is over the octet limit: %q", pad, i, out)
			require.True(t, utf8.ValidString(out),
				"pad %d line %d ends inside a character: %q", pad, i, out)
		}
		require.Greater(t, len(lines), 1, "pad %d: the input was meant to fold", pad)
	}
}

// A continuation is marked by one leading space, and unfolding takes exactly
// that space back. Anything else silently edits the value: a lost space joins
// two words, a kept one prefixes the continuation.
func TestFoldedLinesSurviveUnfoldingUnchanged(t *testing.T) {
	cases := []string{
		"SUMMARY:short enough to stay on one line",
		"SUMMARY:" + strings.Repeat("a", 200),
		"SUMMARY:" + strings.Repeat("あ", 100),
		"SUMMARY:" + strings.Repeat("🙂", 60),
		// A value whose own text starts a continuation with a space: the space
		// that belongs to the title has to outlive the one that marks the fold.
		"DESCRIPTION:" + strings.Repeat("a", 70) + " " + strings.Repeat("b", 70),
		// Mixed widths, so the fold falls inside characters of both sizes.
		"LOCATION:" + strings.Repeat("東京都渋谷区", 20) + strings.Repeat("🗼", 10),
	}
	for _, line := range cases {
		var b strings.Builder
		writeFolded(&b, line)
		got := strings.TrimSuffix(unfoldICS(b.String()), "\n")
		require.Equal(t, line, got, "folding and unfolding must be each other's inverse")
	}
}

// The first line carries 75 octets and a continuation 74, because the leading
// space counts against the same limit. Getting that wrong is invisible until a
// strict reader rejects the file.
func TestFoldingFillsTheLineItIsAllowed(t *testing.T) {
	lines := foldedLines(strings.Repeat("a", 300))
	require.Equal(t, 75, len(lines[0]))
	for i, out := range lines[1:] {
		require.Equal(t, byte(' '), out[0], "a continuation begins with one space")
		if i < len(lines)-2 {
			require.Equal(t, 75, len(out), "a continuation fills its line too")
		}
	}
}
