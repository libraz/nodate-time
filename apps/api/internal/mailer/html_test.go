package mailer

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestRenderHTMLEscapesText(t *testing.T) {
	got := renderHTML(Brand{}, `a <b> & "c"`)
	if !strings.Contains(got, "<p>a &lt;b&gt; &amp; &#34;c&#34;</p>") {
		t.Errorf("text not escaped: %s", got)
	}
}

func TestRenderHTMLLinksURLsAndDropsTrailingPunctuation(t *testing.T) {
	got := renderHTML(Brand{}, "Open https://example.com/a?x=1&y=2. Then (see http://b.example/c), ok!")
	for _, want := range []string{
		`<a href="https://example.com/a?x=1&amp;y=2">https://example.com/a?x=1&amp;y=2</a>.`,
		`(see <a href="http://b.example/c">http://b.example/c</a>), ok!`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestRenderHTMLURLEndsAtDelimiters(t *testing.T) {
	got := renderHTML(Brand{}, `x https://a.example/p"y <https://b.example/q>z`)
	for _, want := range []string{
		`<a href="https://a.example/p">https://a.example/p</a>&#34;y`,
		`&lt;<a href="https://b.example/q">https://b.example/q</a>&gt;z`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestRenderHTMLParagraphsAndBreaks(t *testing.T) {
	got := renderHTML(Brand{}, "one\ntwo\n\nthree\r\n\r\nfour")
	for _, want := range []string{"<p>one<br>two</p>", "<p>three</p>", "<p>four</p>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestRenderHTMLHeader(t *testing.T) {
	cases := []struct {
		name  string
		brand Brand
		want  []string
		not   []string
	}{
		{"logo", Brand{Name: "A&B", LogoURL: "https://cdn.example/l.svg", IconURL: "https://cdn.example/i.svg"},
			[]string{`<img src="https://cdn.example/l.svg" alt="A&amp;B"`, "height:40px", "max-width:240px"}, []string{"i.svg"}},
		{"icon", Brand{Name: "Acme", IconURL: "https://cdn.example/i.svg"},
			[]string{`<img src="https://cdn.example/i.svg" alt=""`, "32px", ">Acme</span>"}, nil},
		{"name only", Brand{Name: "Acme"}, []string{">Acme</div>"}, []string{"<img"}},
		{"data logo is absent", Brand{Name: "Acme", LogoURL: "data:image/png;base64,AAAA", IconURL: "https://cdn.example/i.svg"},
			[]string{"i.svg", ">Acme</span>"}, []string{"data:"}},
		{"upper DATA is absent", Brand{Name: "Acme", LogoURL: "DATA:image/png;base64,AAAA", IconURL: "DATA:image/png;base64,AAAA"},
			[]string{">Acme</div>"}, []string{"<img", "DATA:"}},
		{"upper HTTPS is used", Brand{Name: "Acme", LogoURL: "HTTPS://cdn.example/l.svg"},
			[]string{`src="HTTPS://cdn.example/l.svg"`}, nil},
		{"src is escaped", Brand{Name: "Acme", LogoURL: `https://cdn.example/l.svg?a=1&b="2"`},
			[]string{`src="https://cdn.example/l.svg?a=1&amp;b=&#34;2&#34;"`}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderHTML(c.brand, "body")
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in %s", w, got)
				}
			}
			for _, n := range c.not {
				if strings.Contains(got, n) {
					t.Errorf("unexpected %q in %s", n, got)
				}
			}
		})
	}
}

type mimePart struct{ ctype, cte, body string }

// readParts decodes the multipart/alternative body of msg into its parts.
func readParts(t *testing.T, msg *mail.Message) []mimePart {
	t.Helper()
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" {
		t.Fatalf("Content-Type = %q (%v)", msg.Header.Get("Content-Type"), err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var parts []mimePart
	for {
		p, err := mr.NextRawPart()
		if err == io.EOF {
			return parts
		}
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		pt, pp, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if pp["charset"] != "utf-8" {
			t.Errorf("%s charset = %q", pt, pp["charset"])
		}
		raw, err := io.ReadAll(quotedprintable.NewReader(p))
		if err != nil {
			t.Fatalf("decode %s: %v", pt, err)
		}
		parts = append(parts, mimePart{pt, p.Header.Get("Content-Transfer-Encoding"), string(raw)})
	}
}

func TestSendIsMultipartAlternativeWithBrandedHTML(t *testing.T) {
	host, port, data := scriptedRelay(t, "250 ok")
	m := SMTPMailer{cfg: SMTPConfig{
		Host: host, Port: port, From: "no-reply@example.com", Timeout: 5 * time.Second,
		Brand: Brand{Name: "Acme 時間", LogoURL: "https://cdn.example/l.svg"},
	}}
	const text = "こんにちは\nOpen https://example.com/reset?token=abc.\n\nBye"
	if err := m.Send(context.Background(), Message{To: "user@example.com", Subject: "s", Text: text}); err != nil {
		t.Fatalf("send: %v", err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(<-data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	parts := readParts(t, msg)
	if len(parts) != 2 || parts[0].ctype != "text/plain" || parts[1].ctype != "text/html" {
		t.Fatalf("parts = %+v", parts)
	}
	if got := strings.ReplaceAll(parts[0].body, "\r\n", "\n"); got != text {
		t.Errorf("text part = %q, want %q", got, text)
	}
	for _, want := range []string{
		`alt="Acme 時間"`,
		`<a href="https://example.com/reset?token=abc">`,
		"<p>Bye</p>",
	} {
		if !strings.Contains(parts[1].body, want) {
			t.Errorf("html part missing %q:\n%s", want, parts[1].body)
		}
	}
}
