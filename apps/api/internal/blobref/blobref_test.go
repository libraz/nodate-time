package blobref

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func TestIsForeignKeyRestrictRecognisesTheRefusalOnly(t *testing.T) {
	require.True(t, IsForeignKeyRestrict(fmt.Errorf("wrapped: %w", &mysql.MySQLError{Number: 1451})))
	require.True(t, IsForeignKeyRestrict(&mysql.MySQLError{Number: 1217}))
	require.False(t, IsForeignKeyRestrict(&mysql.MySQLError{Number: 1062}), "a duplicate key is not a referrer")
	require.False(t, IsForeignKeyRestrict(errors.New("delete refused")))
	require.False(t, IsForeignKeyRestrict(nil))
}
