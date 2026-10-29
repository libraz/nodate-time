package users

import (
	"context"
	"strings"

	"github.com/libraz/nodate-time/apps/api/internal/auth"
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
// path makes: registration, password login and each provider sign-in.
//
// The restriction is on when the configured domain list is non-empty or the
// oauth_signin_allowlist table holds a live row of any kind. When on, an
// address is admitted if its domain is in the configured list or a live table
// entry matches it, as an email or as its domain. When off, everyone is.
//
// verifiedEmail must be an address the caller has evidence the user owns.
// A path with no such address passes "", which a restricted deployment
// refuses: an unproven address satisfying either list would let anyone claim
// an allowed domain.
func emailAllowedToSignIn(ctx context.Context, q *generated.Queries, allowedDomains []string, verifiedEmail string) (bool, error) {
	if len(allowedDomains) > 0 && emailDomainAllowed(verifiedEmail, allowedDomains) {
		return true, nil
	}
	entries, err := q.CountEnabledOauthSigninAllowlistEntries(ctx)
	if err != nil {
		return false, err
	}
	if len(allowedDomains) == 0 && entries == 0 {
		return true, nil
	}
	email := strings.ToLower(strings.TrimSpace(verifiedEmail))
	at := strings.LastIndexByte(email, '@')
	// The entry column holds ASCII only; an address outside it cannot match.
	if entries == 0 || at < 0 || at == len(email)-1 || !auth.StorableEmail(email) {
		return false, nil
	}
	return q.IsSigninAdmittedByAllowlist(ctx, generated.IsSigninAdmittedByAllowlistParams{
		Email:  email,
		Domain: email[at+1:],
	})
}
