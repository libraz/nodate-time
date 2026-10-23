package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// Whatever bytes the client sends, the stored hint is valid UTF-8 within the
// length bound, so the session insert cannot refuse it.
func TestClientIPMiddlewareStoresAStorableUserAgent(t *testing.T) {
	cases := map[string]string{
		"cut would split a rune": strings.Repeat("a", maxUserAgentLen-1) + "あいう",
		"invalid bytes":          "Mozilla/5.0 \xff\xfe agent",
		"invalid and long":       strings.Repeat("\xe3\x81", 200) + strings.Repeat("é", 200),
		"short and clean":        "Mozilla/5.0",
	}
	for name, ua := range cases {
		t.Run(name, func(t *testing.T) {
			var got string
			h := ClientIPMiddleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got, _ = UserAgentFromContext(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("User-Agent", ua)
			h.ServeHTTP(httptest.NewRecorder(), req)

			if !utf8.ValidString(got) {
				t.Fatalf("stored user agent is not valid UTF-8: %q", got)
			}
			if len(got) > maxUserAgentLen {
				t.Fatalf("stored user agent is %d bytes, over %d", len(got), maxUserAgentLen)
			}
			if utf8.ValidString(ua) && len(ua) <= maxUserAgentLen && got != ua {
				t.Fatalf("a clean short header was changed: %q", got)
			}
		})
	}
}
