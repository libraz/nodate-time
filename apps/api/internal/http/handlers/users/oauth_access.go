package users

import (
	"context"
	"strings"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// emailDomainAllowed reports whether email's domain is in allowedDomains.
// An empty allowedDomains slice means unrestricted (always true).
func emailDomainAllowed(email string, allowedDomains []string) bool {
	if len(allowedDomains) == 0 {
		return true
	}
	at := strings.LastIndexByte(email, '@')
	if at < 0 || at == len(email)-1 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range allowedDomains {
		if domain == d {
			return true
		}
	}
	return false
}

// emailAllowedToSignIn is the one admission decision every session-issuing
// path makes: registration, password login and each provider sign-in. The
// domain allowlist applies first; if the domain is not allowed, the
// individually allow-listed addresses (managed in the admin panel) are
// consulted as a fallback. An empty domain allowlist means unrestricted.
//
// verifiedEmail must be an address the caller has evidence the user owns.
// A path with no such address passes "", which a restricted deployment
// refuses: an unproven address satisfying either list would let anyone claim
// an allowed domain.
func emailAllowedToSignIn(ctx context.Context, q *generated.Queries, allowedDomains []string, verifiedEmail string) (bool, error) {
	if emailDomainAllowed(verifiedEmail, allowedDomains) {
		return true, nil
	}
	if verifiedEmail == "" {
		return false, nil
	}
	return q.IsEmailAllowed(ctx, strings.ToLower(strings.TrimSpace(verifiedEmail)))
}
