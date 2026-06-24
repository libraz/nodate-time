package users

import (
	"encoding/base64"
	"strings"
	"testing"
)

// makeIDToken builds an unsigned JWT-shaped string whose payload is the given
// JSON, sufficient for nonce extraction (the signature is never verified here).
func makeIDToken(payloadJSON string) string {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return enc(`{"alg":"none"}`) + "." + enc(payloadJSON) + ".sig"
}

func TestIDTokenNonce(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"valid nonce", makeIDToken(`{"nonce":"abc123","sub":"u1"}`), "abc123"},
		{"no nonce claim", makeIDToken(`{"sub":"u1"}`), ""},
		{"not a jwt", "garbage", ""},
		{"two segments", "a.b", ""},
		{"bad base64 payload", "aaa.!!!!.ccc", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := idTokenNonce(c.token); got != c.want {
				t.Fatalf("idTokenNonce(%q) = %q, want %q", c.token, got, c.want)
			}
		})
	}
}

func TestOAuthStateCookie(t *testing.T) {
	// Setting the cookie binds the flow: HttpOnly, scoped, and SameSite=Lax so it
	// still rides the top-level redirect back from the provider.
	set := oauthStateCookie("statevalue", true, 600)
	for _, want := range []string{
		"oauth_state=statevalue",
		"Path=/auth/oauth",
		"HttpOnly",
		"Secure",
		"SameSite=Lax",
		"Max-Age=600",
	} {
		if !strings.Contains(set, want) {
			t.Errorf("cookie %q missing %q", set, want)
		}
	}

	// Clearing uses a non-positive Max-Age and drops Secure when not HTTPS.
	clear := oauthStateCookie("", false, -1)
	if !strings.Contains(clear, "Max-Age=0") {
		t.Errorf("clearing cookie should expire immediately: %q", clear)
	}
	if strings.Contains(clear, "Secure") {
		t.Errorf("non-secure context must not set Secure: %q", clear)
	}
}
