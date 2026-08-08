package config

import (
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// assertUTCPinned checks the two independent clocks a connection carries: the
// driver's own location for reading and writing time.Time, and the session
// time_zone the server evaluates NOW(3) in.
func assertUTCPinned(t *testing.T, dsn string) {
	t.Helper()
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("re-parse normalized DSN %q: %v", dsn, err)
	}
	if !cfg.ParseTime {
		t.Errorf("parseTime is off in %q; DATETIME would arrive as []byte", dsn)
	}
	if cfg.Loc != time.UTC {
		t.Errorf("driver location is %v, want UTC, in %q", cfg.Loc, dsn)
	}
	if got := cfg.Params["time_zone"]; got != "'+00:00'" {
		t.Errorf("session time_zone is %q, want %q, in %q", got, "'+00:00'", dsn)
	}
}

func TestNormalizeDSNPinsBothClocksToUTC(t *testing.T) {
	got, err := NormalizeDSN("ttuser:ttpw@tcp(127.0.0.1:33306)/nodate")
	if err != nil {
		t.Fatalf("NormalizeDSN: %v", err)
	}
	assertUTCPinned(t, got)
}

func TestNormalizeDSNOverridesAConflictingOperatorSetting(t *testing.T) {
	// An operator-supplied TC_DB_DSN must not be able to reintroduce a local
	// zone: expiry timestamps are computed by the server and read by the
	// driver, and the two disagreeing is silent data corruption.
	got, err := NormalizeDSN(
		"ttuser:ttpw@tcp(127.0.0.1:33306)/nodate?parseTime=false&loc=Asia%2FTokyo&time_zone=%27%2B09%3A00%27",
	)
	if err != nil {
		t.Fatalf("NormalizeDSN: %v", err)
	}
	assertUTCPinned(t, got)
}

func TestNormalizeDSNRejectsGarbage(t *testing.T) {
	if _, err := NormalizeDSN("not a dsn"); err == nil {
		t.Fatal("expected an error for an unparseable DSN")
	}
}

func TestLoadNormalizesTheConfiguredDSN(t *testing.T) {
	// Load runs the guards, and the environment no longer excuses any of them,
	// so this answers each one the way a developer's shell does.
	t.Setenv("TC_JWT_SECRET", "a-sufficiently-long-signing-secret-for-this-test")
	t.Setenv("TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS", "true")
	t.Setenv("TC_ALLOW_CONSOLE_MAILER", "true")
	t.Setenv("TC_DB_DSN", "ttuser:ttpw@tcp(127.0.0.1:33306)/nodate?loc=Asia%2FTokyo")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertUTCPinned(t, cfg.DbDsn)
}
