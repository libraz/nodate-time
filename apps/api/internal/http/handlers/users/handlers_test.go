package users

import (
	"errors"
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

func TestPasswordHashForLoginKeepsBcryptHash(t *testing.T) {
	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if got := passwordHashForLogin(hash); got != hash {
		t.Fatal("valid bcrypt hash was not preserved")
	}
}

func TestPasswordHashForLoginUsesDummyHashForPlaceholder(t *testing.T) {
	if got := passwordHashForLogin("!"); got != dummyPasswordHash {
		t.Fatal("placeholder hash did not use dummy bcrypt hash")
	}
}
