package timetree

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// maxNameRunes is the width of calendars.name.
const maxNameRunes = 255

// Mapping is the operator's description of how TimeTree calendars land here.
type Mapping struct {
	Importer string        `yaml:"importer"`
	Users    []MappingUser `yaml:"users"`
	// Authors maps a TimeTree user id to the address recorded as the creator
	// of the events that user created.
	Authors   map[string]string `yaml:"authors"`
	Calendars []MappingCalendar `yaml:"calendars"`
}

// MappingUser is an account to create before anything else.
type MappingUser struct {
	Email string `yaml:"email"`
	Name  string `yaml:"name"`
}

// MappingCalendar maps one TimeTree calendar.
type MappingCalendar struct {
	TimeTree     string `yaml:"timetree"`
	Name         string `yaml:"name"`
	DefaultOwner string `yaml:"default_owner"`
	// DefaultAttendees are the participants of the events default_owner
	// receives: the ones no mapped label assigns to a person.
	DefaultAttendees []string          `yaml:"default_attendees"`
	Members          []MappingMember   `yaml:"members"`
	Labels           map[string]string `yaml:"labels"`
}

// MappingMember is a calendar member and the role it is given.
type MappingMember struct {
	Email string `yaml:"email"`
	Role  string `yaml:"role"`
}

// mappingRoles are the roles a mapping may grant; owner is the importer's.
var mappingRoles = map[string]generated.CalendarMembersRole{
	"manager": generated.CalendarMembersRoleManager,
	"editor":  generated.CalendarMembersRoleEditor,
	"viewer":  generated.CalendarMembersRoleViewer,
}

// norm is the form addresses are compared in.
func norm(email string) string { return auth.NormalizeEmail(email) }

// parseMapping decodes the YAML, refusing unknown keys and empty input.
func parseMapping(r io.Reader) (*Mapping, error) {
	if r == nil {
		return nil, errors.New("mapping: no input")
	}
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var m Mapping
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("mapping is empty")
		}
		return nil, fmt.Errorf("mapping: %w", err)
	}
	return &m, nil
}

// validEmail applies the sign-in allow-list rule: ASCII, local@domain.
func validEmail(email string) bool {
	if email == "" || !auth.StorableEmail(email) || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	local, domain, ok := strings.Cut(email, "@")
	return ok && local != "" && domain != "" && !strings.Contains(domain, "@")
}

// validate checks the mapping on its own, without TimeTree or the database,
// and returns every problem it finds. A dry run may name no calendars.
func (m *Mapping) validate(dryRun bool) []string {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	importer := norm(m.Importer)
	switch {
	case m.Importer == "":
		add("importer is required")
	case !validEmail(m.Importer):
		add("importer %q is not a valid ASCII email address", m.Importer)
	}
	if len(m.Calendars) == 0 && !dryRun {
		add("calendars must list at least one calendar")
	}

	seenUsers := map[string]bool{}
	for i, u := range m.Users {
		switch {
		case !validEmail(u.Email):
			add("users[%d]: %q is not a valid ASCII email address", i, u.Email)
		case seenUsers[norm(u.Email)]:
			add("users[%d]: %q is listed twice", i, u.Email)
		}
		seenUsers[norm(u.Email)] = true
	}

	for _, id := range sortedKeys(m.Authors) {
		if !isUserID(id) {
			add("authors[%q]: the key is not a TimeTree user id", id)
		}
		if email := m.Authors[id]; !validEmail(email) {
			add("authors[%q]: %q is not a valid ASCII email address", id, email)
		}
	}

	seenTT, seenName := map[string]bool{}, map[string]bool{}
	for i, c := range m.Calendars {
		where := fmt.Sprintf("calendars[%d]", i)
		if c.TimeTree == "" {
			add("%s: timetree is required", where)
		} else if seenTT[c.TimeTree] {
			add("%s: timetree %q is listed twice", where, c.TimeTree)
		}
		seenTT[c.TimeTree] = true
		if c.Name != "" {
			if utf8.RuneCountInString(c.Name) > maxNameRunes {
				add("%s: name is longer than %d characters", where, maxNameRunes)
			}
			if seenName[c.Name] {
				add("%s: name %q is used twice", where, c.Name)
			}
			seenName[c.Name] = true
		}

		allowed := map[string]bool{importer: true}
		for j, mem := range c.Members {
			switch {
			case !validEmail(mem.Email):
				add("%s.members[%d]: %q is not a valid ASCII email address", where, j, mem.Email)
			case norm(mem.Email) == importer:
				add("%s.members[%d]: the importer is the owner already and must not be listed", where, j)
			}
			if _, ok := mappingRoles[mem.Role]; !ok {
				add("%s.members[%d]: role %q is not manager, editor or viewer", where, j, mem.Role)
			}
			allowed[norm(mem.Email)] = true
		}
		if c.DefaultOwner != "" && !allowed[norm(c.DefaultOwner)] {
			add("%s: default_owner %q is neither a member nor the importer", where, c.DefaultOwner)
		}
		seenAttendee := map[string]bool{}
		for j, email := range c.DefaultAttendees {
			switch {
			case !allowed[norm(email)]:
				add("%s.default_attendees[%d]: %q is neither a member nor the importer", where, j, email)
			case seenAttendee[norm(email)]:
				add("%s.default_attendees[%d]: %q is listed twice", where, j, email)
			}
			seenAttendee[norm(email)] = true
		}
		for _, label := range sortedKeys(c.Labels) {
			if owner := c.Labels[label]; !allowed[norm(owner)] {
				add("%s.labels[%q]: %q is neither a member nor the importer", where, label, owner)
			}
		}
	}
	return errs
}

// isUserID reports whether s is a TimeTree user id: decimal digits.
func isUserID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// emailRef is an address the mapping names and what it is named for.
type emailRef struct{ email, what string }

// refs lists every address the mapping names, importer and users first.
func (m *Mapping) refs() []emailRef {
	refs := []emailRef{{m.Importer, "importer"}}
	for _, u := range m.Users {
		refs = append(refs, emailRef{u.Email, "user"})
	}
	for _, id := range sortedKeys(m.Authors) {
		refs = append(refs, emailRef{m.Authors[id], "author " + id})
	}
	for _, c := range m.Calendars {
		for _, mem := range c.Members {
			refs = append(refs, emailRef{mem.Email, "member of " + c.TimeTree})
		}
		for _, label := range sortedKeys(c.Labels) {
			refs = append(refs, emailRef{c.Labels[label], "owner of label " + label})
		}
		if c.DefaultOwner != "" {
			refs = append(refs, emailRef{c.DefaultOwner, "default_owner of " + c.TimeTree})
		}
		for _, email := range c.DefaultAttendees {
			refs = append(refs, emailRef{email, "default attendee of " + c.TimeTree})
		}
	}
	return refs
}
