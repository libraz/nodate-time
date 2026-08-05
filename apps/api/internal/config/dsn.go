package config

import (
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

// NormalizeDSN pins the time semantics of a MySQL DSN.
//
// Two clocks meet on every connection and they must agree on UTC or timestamps
// silently disagree by the host's offset:
//
//   - The driver decides how Go time.Time values are written and how DATETIME
//     columns are read back. That is `loc` (plus `parseTime`, without which
//     DATETIME arrives as []byte).
//   - The server decides what NOW(3) and CURRENT_TIMESTAMP(3) evaluate to, which
//     is the session time_zone. Expiry timestamps for sessions, invites and
//     password resets are computed there, so a SYSTEM zone of JST would expire
//     them nine hours early.
//
// Any DSN this application opens goes through here, so an operator-supplied
// TC_DB_DSN cannot reintroduce the split by omitting the parameters.
func NormalizeDSN(dsn string) (string, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	// The quotes are part of the SQL literal: MySQL takes an offset as a quoted
	// string, and the driver sends this verbatim in the connection's SET.
	cfg.Params["time_zone"] = "'+00:00'"
	return cfg.FormatDSN(), nil
}
