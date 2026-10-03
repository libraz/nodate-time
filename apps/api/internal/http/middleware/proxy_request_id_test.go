package middleware

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// RequestID adopts the id a trusted proxy forwards, so the shipped proxy has to
// forward one it generated: every location that passes to the API overwrites
// whatever X-Request-Id the client sent.
func TestShippedProxyGeneratesTheRequestID(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "apps", "web", "nginx", "app.conf"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile(`(?s)location [^{]+\{.*?\n\}`).FindAllString(string(raw), -1)
	proxied := 0
	for _, b := range blocks {
		if !strings.Contains(b, "proxy_pass") {
			continue
		}
		proxied++
		if !regexp.MustCompile(`(?m)^\s*proxy_set_header X-Request-Id \$request_id;`).MatchString(b) {
			t.Errorf("a proxied location forwards the client's X-Request-Id:\n%s", b)
		}
	}
	if proxied == 0 {
		t.Fatal("no proxied location found in app.conf")
	}
}
