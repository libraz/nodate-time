package auth

import "strings"

// AdminAllowlist is a set of email addresses with platform-wide admin rights.
// Membership is configured via TC_ADMIN_EMAILS (comma-separated).
type AdminAllowlist struct {
	emails map[string]struct{}
}

// NewAdminAllowlist parses a comma-separated email list. Whitespace and case
// are ignored.
func NewAdminAllowlist(csv string) AdminAllowlist {
	out := AdminAllowlist{emails: map[string]struct{}{}}
	for _, raw := range strings.Split(csv, ",") {
		e := strings.ToLower(strings.TrimSpace(raw))
		if e != "" {
			out.emails[e] = struct{}{}
		}
	}
	return out
}

// Contains reports whether the given email is configured as an admin.
func (a AdminAllowlist) Contains(email string) bool {
	if a.emails == nil {
		return false
	}
	_, ok := a.emails[strings.ToLower(strings.TrimSpace(email))]
	return ok
}

// Empty reports whether no admins are configured.
func (a AdminAllowlist) Empty() bool {
	return len(a.emails) == 0
}
