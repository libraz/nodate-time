package users

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
)

func TestIsDuplicateKey(t *testing.T) {
	if !isDuplicateKey(&mysql.MySQLError{Number: 1062}) {
		t.Fatal("mysql duplicate key error was not recognized")
	}
	if isDuplicateKey(errors.New("duplicate")) {
		t.Fatal("plain errors must not be treated as mysql duplicate key errors")
	}
}

// The timing equalizer must be a hash the checker will actually work
// through. If it were a value CheckPassword rejects on sight, the
// no-such-account path would return measurably faster than the wrong-password
// one, which is the enumeration side channel it exists to close.
func TestDummyPasswordHashIsRealWork(t *testing.T) {
	if dummyPasswordHash == "" {
		t.Fatal("dummy hash is empty; the login timing paths would diverge")
	}
	if !strings.HasPrefix(dummyPasswordHash, "$argon2id$") {
		t.Fatalf("dummy hash is not argon2id: %q", dummyPasswordHash)
	}
	if auth.CheckPassword("anything at all", dummyPasswordHash) {
		t.Fatal("the timing equalizer accepted a password")
	}
}

// The columns are NOT NULL and neither sign-up path asks, so whatever comes
// out of here is what the account is created with. A deployment that named a
// zone must get it; one that named nothing must still get something storable.
func TestAccountDefaultsFallBackOnlyWhereUnset(t *testing.T) {
	tests := []struct {
		name             string
		in               AccountDefaults
		locale, timezone string
	}{
		{"nothing named", AccountDefaults{}, fallbackLocale, fallbackTimezone},
		{
			"deployment outside the software's own default",
			AccountDefaults{Locale: "en", Timezone: "America/New_York"},
			"en", "America/New_York",
		},
		{"only the zone named", AccountDefaults{Timezone: "Europe/Berlin"}, fallbackLocale, "Europe/Berlin"},
		{"only the locale named", AccountDefaults{Locale: "en"}, "en", fallbackTimezone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.resolved()
			if got.Locale != tt.locale || got.Timezone != tt.timezone {
				t.Fatalf("resolved() = %+v, want locale %q timezone %q", got, tt.locale, tt.timezone)
			}
		})
	}
}
