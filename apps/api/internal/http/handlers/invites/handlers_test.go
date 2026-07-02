package invites

import (
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestIsDuplicateKey(t *testing.T) {
	if !isDuplicateKey(&mysql.MySQLError{Number: 1062}) {
		t.Fatal("mysql duplicate key error was not recognized")
	}
	if isDuplicateKey(errors.New("duplicate")) {
		t.Fatal("plain errors must not be treated as mysql duplicate key errors")
	}
}
