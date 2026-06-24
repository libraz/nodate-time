package users

import "testing"

func TestEmailDomainAllowed(t *testing.T) {
	cases := []struct {
		name    string
		email   string
		domains []string
		want    bool
	}{
		{"unrestricted allows any", "anyone@gmail.com", nil, true},
		{"unrestricted allows any (empty slice)", "anyone@gmail.com", []string{}, true},
		{"matching domain", "user@example.com", []string{"example.com"}, true},
		{"matching domain case-insensitive", "User@Example.COM", []string{"example.com"}, true},
		{"one of several domains", "user@agency.co.jp", []string{"example.com", "agency.co.jp"}, true},
		{"non-matching domain", "user@gmail.com", []string{"example.com"}, false},
		{"subdomain is not the domain", "user@mail.example.com", []string{"example.com"}, false},
		{"missing @", "not-an-email", []string{"example.com"}, false},
		{"trailing @", "user@", []string{"example.com"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := emailDomainAllowed(tc.email, tc.domains); got != tc.want {
				t.Errorf("emailDomainAllowed(%q, %v) = %v, want %v", tc.email, tc.domains, got, tc.want)
			}
		})
	}
}
