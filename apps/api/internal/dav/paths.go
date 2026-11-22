package dav

import (
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// level is how deep in the tree a path sits.
type level int

const (
	levelRoot level = iota
	levelPrincipal
	levelHome
	levelCalendar
	levelObject
)

// homeSegment names the calendar home under a principal.
const homeSegment = "calendars"

// hrefName is what a resource may be called: safe in a URL without escaping,
// and at most 255 bytes with its extension.
var hrefName = regexp.MustCompile(`^[A-Za-z0-9._~@+=-]{1,251}\.ics$`)

// Default resource names and UIDs carry these, and nothing a client creates
// may.
const (
	defaultNamePrefix = "ndt-"
	nameSuffix        = ".ics"
)

type target struct {
	level    level
	calendar uuid.UUID
	name     string
}

// parse reads a public path. A status is returned for a path that names
// nothing this user may reach (404) or a resource name outside the allowed
// set (400).
func (b *backend) parse(p string) (target, int) {
	prefix := b.s.prefix
	if p != prefix && !strings.HasPrefix(p, prefix+"/") {
		return target{}, http.StatusNotFound
	}
	rest := strings.Trim(path.Clean("/"+strings.TrimPrefix(p, prefix)), "/")
	if rest == "" {
		return target{level: levelRoot}, 0
	}
	segs := strings.Split(rest, "/")
	if len(segs) > int(levelObject) {
		return target{}, http.StatusNotFound
	}
	user, err := uuid.Parse(segs[0])
	if err != nil || user.String() != b.userUUID {
		return target{}, http.StatusNotFound
	}
	t := target{level: level(len(segs))}
	if len(segs) >= int(levelHome) && segs[1] != homeSegment {
		return target{}, http.StatusNotFound
	}
	if len(segs) >= int(levelCalendar) {
		if t.calendar, err = uuid.Parse(segs[2]); err != nil {
			return target{}, http.StatusNotFound
		}
	}
	if len(segs) == int(levelObject) {
		if !hrefName.MatchString(segs[3]) {
			return target{}, http.StatusBadRequest
		}
		t.name = segs[3]
	}
	return t, 0
}

// canonical is the one spelling of t: collections end in a slash, UUIDs are
// lowercase.
func (b *backend) canonical(t target) string {
	switch t.level {
	case levelRoot:
		return b.s.prefix + "/"
	case levelPrincipal:
		return b.principalPath()
	case levelHome:
		return b.homePath()
	case levelCalendar:
		return b.calendarPath(t.calendar)
	default:
		return b.calendarPath(t.calendar) + t.name
	}
}

func (b *backend) principalPath() string { return b.s.prefix + "/" + b.userUUID + "/" }

func (b *backend) homePath() string { return b.principalPath() + homeSegment + "/" }

func (b *backend) calendarPath(cal uuid.UUID) string { return b.homePath() + cal.String() + "/" }

// defaultName is the resource name of a series no client named.
func defaultName(publicID []byte) string {
	u, err := uuid.FromBytes(publicID)
	if err != nil {
		return ""
	}
	return defaultNamePrefix + u.String() + nameSuffix
}

// defaultNameID returns the series public ID a default name spells, in the
// exact spelling defaultName writes.
func defaultNameID(name string) ([]byte, bool) {
	if !strings.HasPrefix(name, defaultNamePrefix) || !strings.HasSuffix(name, nameSuffix) {
		return nil, false
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(name, defaultNamePrefix), nameSuffix)
	u, err := uuid.Parse(mid)
	if err != nil || u.String() != mid {
		return nil, false
	}
	return u[:], true
}

// publicUUID reads a stored public ID; the column always holds 16 bytes.
func publicUUID(b []byte) uuid.UUID {
	u, _ := uuid.FromBytes(b)
	return u
}
