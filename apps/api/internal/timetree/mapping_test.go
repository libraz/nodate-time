package timetree

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodMapping = `
importer: ops@example.com
users:
  - email: hanako@example.com
    name: 山田花子
  - email: mgr@example.com
source_users:
  10000001: hanako@example.com
calendars:
  - source: Band
    name: Team
    default_owner: mgr@example.com
    members:
      - { email: hanako@example.com, role: viewer }
      - { email: mgr@example.com, role: manager }
    labels:
      花子: hanako@example.com
`

func mustParse(t *testing.T, src string) *Mapping {
	t.Helper()
	m, err := parseMapping(strings.NewReader(src))
	require.NoError(t, err)
	return m
}

func TestMappingParsesTheDocumentedShape(t *testing.T) {
	m := mustParse(t, goodMapping)
	assert.Equal(t, "ops@example.com", m.Importer)
	require.Len(t, m.Users, 2)
	assert.Equal(t, "山田花子", m.Users[0].Name)
	assert.Equal(t, "", m.Users[1].Name, "name is optional")
	require.Len(t, m.Calendars, 1)
	assert.Equal(t, "hanako@example.com", m.Calendars[0].Labels["花子"], "non-ASCII label names are kept as written")
	assert.Equal(t, map[string]string{"10000001": "hanako@example.com"}, m.SourceUsers, "a numeric key reads as the id")
	assert.Empty(t, m.validate(false))
}

func TestMappingEmptyOrUnreadableYAMLIsRefused(t *testing.T) {
	for name, src := range map[string]string{
		"empty":      "",
		"whitespace": "  \n",
		"not yaml":   "importer: [unclosed",
		"wrong type": "- just\n- a list\n",
		"duplicate":  "importer: a@example.com\nimporter: b@example.com\n",
	} {
		_, err := parseMapping(strings.NewReader(src))
		assert.Error(t, err, name)
	}
	_, err := parseMapping(nil)
	assert.Error(t, err)
}

func TestMappingUnknownKeysAreRefused(t *testing.T) {
	for name, src := range map[string]string{
		"top level": "importer: a@example.com\nversion: 1\ncalendars: []\n",
		"user":      "importer: a@example.com\nusers:\n  - {email: b@example.com, nane: x}\n",
		"calendar":  "importer: a@example.com\ncalendars:\n  - {source: x, owner: y}\n",
		"member":    "importer: a@example.com\ncalendars:\n  - {source: x, members: [{email: b@example.com, role: viewer, color: red}]}\n",
	} {
		_, err := parseMapping(strings.NewReader(src))
		assert.Error(t, err, name)
	}
}

// edit parses goodMapping, applies mut and returns the validation errors.
func edit(t *testing.T, dryRun bool, mut func(*Mapping)) []string {
	t.Helper()
	m := mustParse(t, goodMapping)
	mut(m)
	return errorTexts(m.validate(dryRun))
}

func assertOneError(t *testing.T, errs []string, contains string) {
	t.Helper()
	require.Len(t, errs, 1, "%v", errs)
	assert.Contains(t, errs[0], contains)
}

func TestMappingValidationRules(t *testing.T) {
	t.Run("importer is required", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) { m.Importer = "" }), "importer: is required")
	})
	t.Run("calendars are required on a real run", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) { m.Calendars = nil }), "at least one calendar")
	})
	t.Run("calendars may be empty on a dry run", func(t *testing.T) {
		assert.Empty(t, edit(t, true, func(m *Mapping) { m.Calendars = nil }))
	})
	t.Run("non-ASCII email", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) { m.Users[0].Email = "はなこ@example.com" }), "users[0]")
	})
	t.Run("malformed emails", func(t *testing.T) {
		for _, bad := range []string{"plain", "@example.com", "a@", "a@b@c", "a b@example.com", " a@example.com"} {
			errs := edit(t, false, func(m *Mapping) { m.Importer = bad })
			assert.NotEmpty(t, errs, bad)
		}
	})
	t.Run("emails compare case-insensitively", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) { m.Users[1].Email = "HANAKO@example.com" }), "listed twice")
		// A label owner written in another case still names the member.
		assert.Empty(t, edit(t, false, func(m *Mapping) { m.Calendars[0].Labels["花子"] = "Hanako@EXAMPLE.com" }))
	})
	t.Run("duplicate users", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) { m.Users = append(m.Users, m.Users[0]) }), "listed twice")
	})
	t.Run("duplicate source", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) {
			c := m.Calendars[0]
			c.Name = "Other"
			m.Calendars = append(m.Calendars, c)
		}), `calendars[1].source: "Band" is listed twice`)
	})
	t.Run("duplicate name", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) {
			c := m.Calendars[0]
			c.Source = "Another"
			m.Calendars = append(m.Calendars, c)
		}), `calendars[1].name: "Team" is listed twice`)
	})
	t.Run("name length", func(t *testing.T) {
		assert.Empty(t, edit(t, false, func(m *Mapping) { m.Calendars[0].Name = strings.Repeat("あ", 255) }))
		assertOneError(t, edit(t, false, func(m *Mapping) { m.Calendars[0].Name = strings.Repeat("あ", 256) }), "longer than 255")
	})
	t.Run("unknown or owner role", func(t *testing.T) {
		for _, role := range []string{"owner", "admin", ""} {
			assertOneError(t, edit(t, false, func(m *Mapping) { m.Calendars[0].Members[0].Role = role }), "role")
		}
	})
	t.Run("importer as a member", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) {
			m.Calendars[0].Members = append(m.Calendars[0].Members, MappingMember{Email: "OPS@example.com", Role: "viewer"})
		}), "importer")
	})
	t.Run("importer in users, labels and default_owner is fine", func(t *testing.T) {
		assert.Empty(t, edit(t, false, func(m *Mapping) {
			m.Users = append(m.Users, MappingUser{Email: "ops@example.com"})
			m.Calendars[0].DefaultOwner = "ops@example.com"
			m.Calendars[0].Labels["importer"] = "ops@example.com"
		}))
	})
	t.Run("label owner must be a member or the importer", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) { m.Calendars[0].Labels["x"] = "stranger@example.com" }), "labels[\"x\"]")
	})
	t.Run("default_owner must be a member or the importer", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) { m.Calendars[0].DefaultOwner = "stranger@example.com" }), "default_owner")
	})
	t.Run("default_attendees must be members or the importer", func(t *testing.T) {
		assert.Empty(t, edit(t, false, func(m *Mapping) {
			m.Calendars[0].DefaultAttendees = []string{"ops@example.com", "Hanako@example.com"}
		}))
		assertOneError(t, edit(t, false, func(m *Mapping) {
			m.Calendars[0].DefaultAttendees = []string{"stranger@example.com"}
		}), "default_attendees[0]")
	})
	t.Run("default_attendees duplicates", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) {
			m.Calendars[0].DefaultAttendees = []string{"hanako@example.com", "HANAKO@example.com"}
		}), "default_attendees[1]")
	})
	t.Run("source user keys are TimeTree user ids", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) { m.SourceUsers["hanako"] = "hanako@example.com" }), `source_users["hanako"]`)
	})
	t.Run("source user addresses must be valid", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) { m.SourceUsers["1"] = "not-an-address" }), `source_users["1"]`)
	})
	t.Run("source is required", func(t *testing.T) {
		assertOneError(t, edit(t, false, func(m *Mapping) { m.Calendars[0].Source = "" }), "calendars[0].source: is required")
	})
}

func TestRunRefusesBadInputWithoutTouchingAnything(t *testing.T) {
	ctx := context.Background()
	run := func(o Options) (int, string, error) {
		var out strings.Builder
		o.Out = &out
		code, err := Run(ctx, o)
		return code, out.String(), err
	}
	ok := strings.NewReader(goodMapping)

	code, _, err := run(Options{Password: "p", Mapping: ok})
	assert.Equal(t, 2, code)
	assert.ErrorContains(t, err, "TIMETREE_EMAIL")
	code, _, err = run(Options{Email: "e", Mapping: strings.NewReader(goodMapping), DryRun: true})
	assert.Equal(t, 2, code, "a dry run needs credentials too")
	assert.Error(t, err)

	code, emptyOut, err := run(Options{Email: "e", Password: "p", Mapping: strings.NewReader("")})
	assert.Equal(t, 2, code)
	assert.ErrorContains(t, err, "1 error")
	assert.Contains(t, emptyOut, "  - mapping is empty")

	code, out, err := run(Options{Email: "e", Password: "p", Mapping: strings.NewReader("importer: nope\ncalendars: [{source: x}]\n")})
	assert.Equal(t, 2, code)
	assert.Error(t, err)
	assert.Contains(t, out, "not a valid ASCII email")
}
