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

// emailAllowedToSignIn decides whether an account with the given email may
// access the platform, whether via OAuth sign-in or password registration.
// The domain allowlist applies first; if the domain is not allowed, the
// individually allow-listed addresses (managed in the admin panel) are
// consulted as a fallback. An empty domain allowlist means unrestricted.
func emailAllowedToSignIn(ctx context.Context, q *generated.Queries, allowedDomains []string, email string) (bool, error) {
	if emailDomainAllowed(email, allowedDomains) {
		return true, nil
	}
	return q.IsEmailAllowed(ctx, strings.ToLower(strings.TrimSpace(email)))
}
