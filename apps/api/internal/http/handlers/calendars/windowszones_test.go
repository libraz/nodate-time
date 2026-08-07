package calendars

import (
	"testing"
	"time"
)

// TestWindowsZoneNamesResolveToRealZones verifies the mapping is usable: every
// name in the table has to name a zone this server can actually load, or the
// file that carries it still lands in UTC and nothing says so.
func TestWindowsZoneNamesResolveToRealZones(t *testing.T) {
	for windows, iana := range windowsZones {
		if _, err := time.LoadLocation(iana); err != nil {
			t.Errorf("%q maps to %q, which does not load: %v", windows, iana, err)
		}
	}
}

// TestResolveTZIDPlacesTheNamesFilesActuallyCarry covers the four kinds of TZID
// that arrive: an IANA name, a Windows one, a name for the machine's own zone,
// and one nothing can make sense of.
func TestResolveTZIDPlacesTheNamesFilesActuallyCarry(t *testing.T) {
	cases := []struct {
		tzid string
		want string
		ok   bool
	}{
		{"Asia/Tokyo", "Asia/Tokyo", true},
		{"Tokyo Standard Time", "Asia/Tokyo", true},
		// Exchange quotes a name containing spaces, and the quotes are not part
		// of it.
		{`"W. Europe Standard Time"`, "Europe/Berlin", true},
		{"UTC", "UTC", true},
		// "Local" is the zone of whichever machine runs the server, which is
		// not something a file can be talking about.
		{"Local", "", false},
		{"Mars/Olympus", "", false},
		{"../../../../etc/passwd", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := resolveTZID(c.tzid)
		if ok != c.ok || got != c.want {
			t.Errorf("resolveTZID(%q) = %q, %v; want %q, %v", c.tzid, got, ok, c.want, c.ok)
		}
	}
}

// TestWindowsZoneNamesCarryTheirOwnDaylightRules verifies what the mapping is
// for. "Standard Time" is part of the name all year, so the zone it resolves to
// is what decides whether a given instant is on standard time or not -- taking
// the standard offset from the name would be wrong for half of every year.
func TestWindowsZoneNamesCarryTheirOwnDaylightRules(t *testing.T) {
	name, ok := resolveTZID("Eastern Standard Time")
	if !ok {
		t.Fatal("Eastern Standard Time should resolve")
	}
	loc := loadLocationOrUTC(name)
	summer := time.Date(2026, 6, 1, 10, 0, 0, 0, loc)
	if got := summer.UTC().Format(time.RFC3339); got != "2026-06-01T14:00:00Z" {
		t.Errorf("10:00 on 1 June is 14:00Z on daylight time, got %s", got)
	}
	winter := time.Date(2026, 1, 15, 10, 0, 0, 0, loc)
	if got := winter.UTC().Format(time.RFC3339); got != "2026-01-15T15:00:00Z" {
		t.Errorf("10:00 in January is 15:00Z on standard time, got %s", got)
	}
}
