package ical

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// forbiddenControl reports whether r is a character RFC 5545 does not allow
// inside a value: every C0 control but HTAB, and DEL.
//
// A line break is left out because a value may legally contain one, written as
// an escape. The rest are not text: a terminal executes an escape sequence
// rather than showing it, so a title carrying one runs a command in the shell
// of whoever reads the file.
func forbiddenControl(r rune) bool {
	return r == 0x7f || (r < 0x20 && r != '\t' && r != '\n')
}

// DropForbiddenControl removes the characters RFC 5545 forbids inside a value.
// They are dropped rather than costing the value: nothing in a calendar means
// anything by them, and one rule both ways keeps an import and the export after
// it agreeing on what an event says.
func DropForbiddenControl(s string) string {
	if !strings.ContainsFunc(s, forbiddenControl) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if forbiddenControl(r) {
			return -1
		}
		return r
	}, s)
}

// escapeText renders a TEXT value.
func escapeText(s string) string {
	// Every newline variant becomes \n first, so a bare CR cannot leak out raw.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = DropForbiddenControl(s)
	r := strings.NewReplacer(
		"\\", `\\`,
		";", `\;`,
		",", `\,`,
		"\n", `\n`,
	)
	return r.Replace(s)
}

// escapeURI renders a URI value. URIs are not TEXT-escaped, so commas and
// semicolons stay; line breaks are stripped so the value cannot start another
// content line.
func escapeURI(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return DropForbiddenControl(s)
}

func unescapeText(s string) string {
	r := strings.NewReplacer(
		`\\`, "\\",
		`\;`, ";",
		`\,`, ",",
		`\n`, "\n",
		`\N`, "\n",
	)
	return r.Replace(s)
}

// exceptionString reads one stored exclusion element as its string form.
func exceptionString(el json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(el, &s) != nil {
		return "", false
	}
	return s, true
}

// writeFolded writes a content line, folding at 75 octets per RFC 5545 §3.1.
// A continuation's leading space counts against the same limit, leaving 74
// octets for its text.
//
// §3.1 also forbids splitting a multi-octet character across the fold. Cutting
// at the octet count alone leaves a byte sequence that is not a character, and
// every reader substitutes or drops it.
func writeFolded(b *strings.Builder, line string) {
	const limit = 75
	const cont = limit - 1

	n := foldPoint(line, limit)
	b.WriteString(line[:n])
	b.WriteString("\r\n")

	rest := line[n:]
	for rest != "" {
		n = foldPoint(rest, cont)
		b.WriteByte(' ')
		b.WriteString(rest[:n])
		b.WriteString("\r\n")
		rest = rest[n:]
	}
}

// foldPoint returns how many octets of s fit in limit without ending inside a
// character.
func foldPoint(s string, limit int) int {
	if len(s) <= limit {
		return len(s)
	}
	n := limit
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	if n == 0 {
		// Only reachable for text that is not UTF-8 to begin with; writing the
		// full run keeps this from folding nothing forever.
		return limit
	}
	return n
}

func unfold(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	// A file separated by bare CR is one long line otherwise.
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\n ", "")
	text = strings.ReplaceAll(text, "\n\t", "")
	return text
}

// truncateRunes cuts s to at most n characters, never inside one.
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}
