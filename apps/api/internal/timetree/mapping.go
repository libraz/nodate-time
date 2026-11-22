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

// Kinds of mapping problem. They match the kind column of sql/core/IMPORTERS.md
// one to one.
const (
	kindEmpty               = "empty"
	kindUnknownKey          = "unknown_key"
	kindInvalidYAML         = "invalid_yaml"
	kindRequired            = "required"
	kindInvalidEmail        = "invalid_email"
	kindDuplicate           = "duplicate"
	kindInvalidSourceUserID = "invalid_source_user_id"
	kindNoContainers        = "no_containers"
	kindTooLong             = "too_long"
	kindImporterListed      = "importer_listed"
	kindInvalidRole         = "invalid_role"
	kindNotMember           = "not_member"
)

// mappingError is one problem found in the mapping. Path is the YAML
// position, empty when the problem concerns the whole document.
type mappingError struct{ Path, Kind, Detail string }

// Error renders "<path>: <detail>", or the detail alone when Path is empty.
func (e mappingError) Error() string {
	if e.Path == "" {
		return e.Detail
	}
	return e.Path + ": " + e.Detail
}

// Mapping is the operator's description of how TimeTree calendars land here.
type Mapping struct {
	Importer string        `yaml:"importer"`
	Users    []MappingUser `yaml:"users"`
	// SourceUsers maps a TimeTree user id to the address recorded as the
	// creator of the events that user created.
	SourceUsers map[string]string `yaml:"source_users"`
	Calendars   []MappingCalendar `yaml:"calendars"`
}

// MappingUser is an account to create before anything else.
type MappingUser struct {
	Email string `yaml:"email"`
	Name  string `yaml:"name"`
}

// MappingCalendar maps one TimeTree calendar.
type MappingCalendar struct {
	// Source is the TimeTree calendar name or alias_code.
	Source       string `yaml:"source"`
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

// roleList is mappingRoles in the order the error text lists it.
const roleList = "manager, editor, viewer"

// norm is the form addresses are compared in.
func norm(email string) string { return auth.NormalizeEmail(email) }

// parseMapping decodes the YAML, refusing unknown keys and empty input. Every
// failure is a mappingError.
func parseMapping(r io.Reader) (*Mapping, error) {
	if r == nil {
		return nil, mappingError{Kind: kindEmpty, Detail: "mapping is empty"}
	}
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var m Mapping
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, mappingError{Kind: kindEmpty, Detail: "mapping is empty"}
		}
		kind := kindInvalidYAML
		if strings.Contains(err.Error(), "not found in type") {
			kind = kindUnknownKey
		}
		return nil, mappingError{Kind: kind, Detail: err.Error()}
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
func (m *Mapping) validate(dryRun bool) []mappingError {
	var errs []mappingError
	add := func(path, kind, format string, a ...any) {
		errs = append(errs, mappingError{Path: path, Kind: kind, Detail: fmt.Sprintf(format, a...)})
	}
	email := func(path, v string, seen map[string]bool) {
		switch {
		case v == "":
			add(path, kindRequired, "is required")
		case !validEmail(v):
			add(path, kindInvalidEmail, "%q is not a valid ASCII email address", v)
		case seen != nil && seen[norm(v)]:
			add(path, kindDuplicate, "%q is listed twice", v)
		}
		if seen != nil && v != "" {
			seen[norm(v)] = true
		}
	}

	email("importer", m.Importer, nil)
	importer := norm(m.Importer)

	seenUsers := map[string]bool{}
	for i, u := range m.Users {
		email(fmt.Sprintf("users[%d].email", i), u.Email, seenUsers)
	}

	for _, id := range sortedKeys(m.SourceUsers) {
		path := fmt.Sprintf("source_users[%q]", id)
		if !isUserID(id) {
			add(path, kindInvalidSourceUserID, "is not a source user id")
		}
		if v := m.SourceUsers[id]; !validEmail(v) {
			add(path, kindInvalidEmail, "%q is not a valid ASCII email address", v)
		}
	}

	if len(m.Calendars) == 0 && !dryRun {
		add("calendars", kindNoContainers, "must list at least one calendar")
	}

	seenSource, seenName := map[string]bool{}, map[string]bool{}
	for i, c := range m.Calendars {
		where := fmt.Sprintf("calendars[%d]", i)
		if c.Source == "" {
			add(where+".source", kindRequired, "is required")
		} else if seenSource[c.Source] {
			add(where+".source", kindDuplicate, "%q is listed twice", c.Source)
		}
		seenSource[c.Source] = true
		if c.Name != "" {
			if utf8.RuneCountInString(c.Name) > maxNameRunes {
				add(where+".name", kindTooLong, "is longer than %d characters", maxNameRunes)
			}
			if seenName[c.Name] {
				add(where+".name", kindDuplicate, "%q is listed twice", c.Name)
			}
			seenName[c.Name] = true
		}

		allowed := map[string]bool{importer: true}
		seenMember := map[string]bool{}
		for j, mem := range c.Members {
			path := fmt.Sprintf("%s.members[%d]", where, j)
			switch {
			case mem.Email == "":
				add(path+".email", kindRequired, "is required")
			case !validEmail(mem.Email):
				add(path+".email", kindInvalidEmail, "%q is not a valid ASCII email address", mem.Email)
			case norm(mem.Email) == importer:
				add(path+".email", kindImporterListed, "the importer must not be listed")
			case seenMember[norm(mem.Email)]:
				add(path+".email", kindDuplicate, "%q is listed twice", mem.Email)
			}
			seenMember[norm(mem.Email)] = true
			switch _, ok := mappingRoles[mem.Role]; {
			case mem.Role == "":
				add(path+".role", kindRequired, "is required")
			case !ok:
				add(path+".role", kindInvalidRole, "%q is not one of %s", mem.Role, roleList)
			}
			allowed[norm(mem.Email)] = true
		}
		if c.DefaultOwner != "" && !allowed[norm(c.DefaultOwner)] {
			add(where+".default_owner", kindNotMember, "%q is neither a member nor the importer", c.DefaultOwner)
		}
		seenAttendee := map[string]bool{}
		for j, email := range c.DefaultAttendees {
			path := fmt.Sprintf("%s.default_attendees[%d]", where, j)
			switch {
			case !allowed[norm(email)]:
				add(path, kindNotMember, "%q is neither a member nor the importer", email)
			case seenAttendee[norm(email)]:
				add(path, kindDuplicate, "%q is listed twice", email)
			}
			seenAttendee[norm(email)] = true
		}
		for _, label := range sortedKeys(c.Labels) {
			if owner := c.Labels[label]; !allowed[norm(owner)] {
				add(fmt.Sprintf("%s.labels[%q]", where, label), kindNotMember, "%q is neither a member nor the importer", owner)
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

// emailRef is an address the mapping names and the path of the place that
// names it.
type emailRef struct{ email, path string }

// refs lists every address the mapping names, importer and users first. An
// address named in several places is listed at each; callers keep the first.
func (m *Mapping) refs() []emailRef {
	refs := []emailRef{{m.Importer, "importer"}}
	for i, u := range m.Users {
		refs = append(refs, emailRef{u.Email, fmt.Sprintf("users[%d].email", i)})
	}
	for _, id := range sortedKeys(m.SourceUsers) {
		refs = append(refs, emailRef{m.SourceUsers[id], fmt.Sprintf("source_users[%q]", id)})
	}
	for i, c := range m.Calendars {
		where := fmt.Sprintf("calendars[%d]", i)
		for j, mem := range c.Members {
			refs = append(refs, emailRef{mem.Email, fmt.Sprintf("%s.members[%d].email", where, j)})
		}
		for _, label := range sortedKeys(c.Labels) {
			refs = append(refs, emailRef{c.Labels[label], fmt.Sprintf("%s.labels[%q]", where, label)})
		}
		if c.DefaultOwner != "" {
			refs = append(refs, emailRef{c.DefaultOwner, where + ".default_owner"})
		}
		for j, email := range c.DefaultAttendees {
			refs = append(refs, emailRef{email, fmt.Sprintf("%s.default_attendees[%d]", where, j)})
		}
	}
	return refs
}
