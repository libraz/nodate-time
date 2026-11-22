package dav

import (
	"bufio"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// registries are the event kind lists a deployment accepts, relative to this
// package.
var registries = []string{
	"../../../../sql/core/event-kinds.yaml",
	"../../../../sql/time/event-kinds.yaml",
}

func registryKinds(t *testing.T) []string {
	t.Helper()
	var kinds []string
	for _, p := range registries {
		f, err := os.Open(p)
		if err != nil {
			t.Fatalf("open %s: %v", p, err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if k, ok := strings.CutPrefix(line, "- kind:"); ok {
				kinds = append(kinds, strings.TrimSpace(k))
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
	}
	return kinds
}

func TestEveryLogKindIsClassified(t *testing.T) {
	kinds := registryKinds(t)
	if len(kinds) == 0 {
		t.Fatal("no kinds read from the registries")
	}
	listed := map[string]bool{}
	for _, k := range kinds {
		listed[k] = true
		if _, ok := logActions[k]; !ok {
			t.Errorf("kind %q has no sync action", k)
		}
	}
	for k := range logActions {
		if !listed[k] {
			t.Errorf("sync action for %q, which no registry lists", k)
		}
	}
}

func TestSyncTokenRoundTrip(t *testing.T) {
	tok := syncToken{calendar: uuid.NewString(), logID: 42, day: 20000, version: "1"}
	got, ok := parseToken(tok.String())
	if !ok || got != tok {
		t.Fatalf("parse(%q) = %+v, %v", tok.String(), got, ok)
	}
	for _, bad := range []string{
		"", "urn:nodate-time:sync:", "urn:other:sync:a:1:2:1",
		"urn:nodate-time:sync:a:x:2:1", "urn:nodate-time:sync:a:1:y:1",
		"urn:nodate-time:sync:a:1:2:1:extra", "urn:nodate-time:sync:a:-1:2:1",
	} {
		if _, ok := parseToken(bad); ok {
			t.Errorf("parse(%q) accepted", bad)
		}
	}
}

func TestParseCanonicalisesAndRefuses(t *testing.T) {
	user := uuid.New()
	cal := uuid.New()
	b := &backend{s: &server{prefix: "/api/dav"}, userUUID: user.String()}
	home := "/api/dav/" + user.String() + "/calendars/"
	cases := []struct {
		in     string
		want   string
		status int
	}{
		{in: "/api/dav", want: "/api/dav/"},
		{in: "/api/dav/" + strings.ToUpper(user.String()), want: "/api/dav/" + user.String() + "/"},
		{in: home + cal.String(), want: home + cal.String() + "/"},
		{in: home + "x/../" + cal.String() + "//", want: home + cal.String() + "/"},
		{in: home + cal.String() + "/a.ics", want: home + cal.String() + "/a.ics"},
		{in: "/api/dav/" + uuid.NewString() + "/", status: http.StatusNotFound},
		{in: "/api/dav/" + user.String() + "/other/", status: http.StatusNotFound},
		{in: home + "not-a-uuid/", status: http.StatusNotFound},
		{in: home + cal.String() + "/a.ics/b", status: http.StatusNotFound},
		{in: home + cal.String() + "/a b.ics", status: http.StatusBadRequest},
		{in: home + cal.String() + "/a%41.ics", status: http.StatusBadRequest},
		{in: home + cal.String() + "/" + strings.Repeat("a", 252) + ".ics", status: http.StatusBadRequest},
		{in: home + cal.String() + "/a.txt", status: http.StatusBadRequest},
		{in: "/elsewhere/", status: http.StatusNotFound},
	}
	for _, c := range cases {
		tgt, status := b.parse(c.in)
		if status != c.status {
			t.Errorf("parse(%q) status = %d, want %d", c.in, status, c.status)
			continue
		}
		if status == 0 && b.canonical(tgt) != c.want {
			t.Errorf("canonical(%q) = %q, want %q", c.in, b.canonical(tgt), c.want)
		}
	}
}

func TestDefaultNameRoundTrip(t *testing.T) {
	u := uuid.New()
	name := defaultName(u[:])
	got, ok := defaultNameID(name)
	if !ok || publicUUID(got) != u {
		t.Fatalf("defaultNameID(%q) = %x, %v", name, got, ok)
	}
	for _, other := range []string{
		strings.ToUpper(name), "ndt-" + u.String() + ".ICS", u.String() + ".ics", "ndt-x.ics",
	} {
		if _, ok := defaultNameID(other); ok {
			t.Errorf("defaultNameID(%q) accepted", other)
		}
	}
}
