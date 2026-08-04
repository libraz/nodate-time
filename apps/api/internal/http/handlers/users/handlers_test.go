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
