package auth

import "testing"

// TestS256ChallengeRFCVector checks the S256 transform against the worked
// example in RFC 7636 Appendix B, so the challenge we send matches what
// providers compute from the verifier.
func TestS256ChallengeRFCVector(t *testing.T) {
	const (
		verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	)
	if got := S256Challenge(verifier); got != challenge {
		t.Fatalf("S256Challenge = %q, want %q", got, challenge)
	}
}

// TestGeneratePKCE verifies the generated verifier and challenge are consistent
// and free of base64 padding (required by the spec).
func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if verifier == "" || challenge == "" {
		t.Fatal("expected non-empty verifier and challenge")
	}
	if S256Challenge(verifier) != challenge {
		t.Fatal("challenge does not match verifier")
	}
	for _, s := range []string{verifier, challenge} {
		for _, c := range s {
			if c == '=' || c == '+' || c == '/' {
				t.Fatalf("value %q contains non-URL-safe or padding character %q", s, c)
			}
		}
	}
	// A fresh call must not repeat the verifier.
	v2, _, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if v2 == verifier {
		t.Fatal("expected distinct verifiers across calls")
	}
}
