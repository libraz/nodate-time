package mailer

import (
	"html"
	"regexp"
	"strings"
)

// Brand is what the HTML part's header shows: the logo, else the icon with the
// name, else the name. Only https images are used; a data: URI counts as unset
// because mail clients do not render it.
type Brand struct {
	Name    string
	LogoURL string
	IconURL string
}

var (
	urlRun         = regexp.MustCompile(`https?://[^\s<>"]+`)
	paragraphBreak = regexp.MustCompile(`\n(?:[ \t]*\n)+`)
)

const urlTrailingPunct = ".,;:!?)"

func httpsImage(u string) bool {
	return len(u) >= 8 && strings.EqualFold(u[:8], "https://")
}

// renderHTML builds the HTML alternative of a plain-text body. The raw text is
// split into URL runs and the rest before anything is escaped, so a URL's own
// characters never pass through the escaper twice.
func renderHTML(b Brand, body string) string {
	var out strings.Builder
	out.WriteString("<!DOCTYPE html>\n<html><body style=\"font-family:sans-serif;line-height:1.5\">\n")
	out.WriteString(renderBrandHeader(b))
	body = strings.ReplaceAll(body, "\r\n", "\n")
	for _, para := range paragraphBreak.Split(strings.TrimSpace(body), -1) {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		out.WriteString("<p>")
		out.WriteString(strings.ReplaceAll(linkify(para), "\n", "<br>"))
		out.WriteString("</p>\n")
	}
	out.WriteString("</body></html>\n")
	return out.String()
}

func linkify(text string) string {
	var out strings.Builder
	last := 0
	for _, loc := range urlRun.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		end = start + len(strings.TrimRight(text[start:end], urlTrailingPunct))
		if end == start {
			continue
		}
		out.WriteString(html.EscapeString(text[last:start]))
		u := html.EscapeString(text[start:end])
		out.WriteString(`<a href="` + u + `">` + u + `</a>`)
		last = end
	}
	out.WriteString(html.EscapeString(text[last:]))
	return out.String()
}

func renderBrandHeader(b Brand) string {
	name := html.EscapeString(b.Name)
	switch {
	case httpsImage(b.LogoURL):
		return `<div style="margin-bottom:24px"><img src="` + html.EscapeString(b.LogoURL) +
			`" alt="` + name + `" height="40" style="height:40px;max-width:240px"></div>` + "\n"
	case httpsImage(b.IconURL):
		return `<div style="margin-bottom:24px"><img src="` + html.EscapeString(b.IconURL) +
			`" alt="" width="32" height="32" style="height:32px;width:32px;vertical-align:middle">` +
			` <span style="font-size:18px;font-weight:bold;vertical-align:middle">` + name + `</span></div>` + "\n"
	case b.Name != "":
		return `<div style="margin-bottom:24px;font-size:18px;font-weight:bold">` + name + `</div>` + "\n"
	}
	return ""
}
