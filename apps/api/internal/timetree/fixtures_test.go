package timetree

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/libraz/nodate-time/apps/api/internal/accounts"
	"github.com/libraz/nodate-time/apps/api/internal/calimport"
)

// repoRoot is the repository root; tests run in apps/api/internal/timetree.
const repoRoot = "../../../.."

// readCoreFixture reads a shared importer fixture from sql/core/importers.
func readCoreFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "sql", "core", "importers", name))
	require.NoError(t, err)
	return b
}

// kindPhrases are the detail fragments each kind must carry.
var kindPhrases = map[string]string{
	kindEmpty:               "mapping is empty",
	kindRequired:            "is required",
	kindInvalidEmail:        "is not a valid ASCII email address",
	kindDuplicate:           "is listed twice",
	kindInvalidSourceUserID: "is not a source user id",
	kindNoContainers:        "must list at least one",
	kindTooLong:             "is longer than",
	kindImporterListed:      "the importer must not be listed",
	kindInvalidRole:         "is not one of",
	kindNotMember:           "is neither a member nor the importer",
}

type mappingFixtureCase struct {
	Name      string           `yaml:"name"`
	DryRun    bool             `yaml:"dry_run"`
	Raw       *string          `yaml:"raw"`
	Mapping   *Mapping         `yaml:"mapping"`
	Container *MappingCalendar `yaml:"container"`
	Expect    []struct {
		Path string `yaml:"path"`
		Kind string `yaml:"kind"`
	} `yaml:"expect"`
}

// validateRaw runs both stages on a whole document, as Run does.
func validateRaw(raw string, dryRun bool) []mappingError {
	m, err := parseMapping(strings.NewReader(raw))
	if err != nil {
		var me mappingError
		if errors.As(err, &me) {
			return []mappingError{me}
		}
		return []mappingError{{Kind: kindInvalidYAML, Detail: err.Error()}}
	}
	return m.validate(dryRun)
}

func TestMappingCommonFixtures(t *testing.T) {
	var f struct {
		Version int                  `yaml:"version"`
		Cases   []mappingFixtureCase `yaml:"cases"`
	}
	require.NoError(t, yaml.Unmarshal(readCoreFixture(t, "mapping-common.yaml"), &f))
	require.Equal(t, 1, f.Version)
	require.NotEmpty(t, f.Cases)

	for _, c := range f.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var got []mappingError
			if c.Raw != nil {
				got = validateRaw(*c.Raw, c.DryRun)
			} else {
				m := c.Mapping
				if m == nil {
					m = &Mapping{}
				}
				m.Calendars = nil
				if c.Container != nil {
					m.Calendars = []MappingCalendar{*c.Container}
				}
				got = m.validate(c.DryRun)
			}

			type key struct{ path, kind string }
			want := map[key]bool{}
			for _, e := range c.Expect {
				path := e.Path
				if path == "containers" || strings.HasPrefix(path, "containers[") {
					path = "calendars" + strings.TrimPrefix(path, "containers")
				}
				want[key{path, e.Kind}] = true
			}
			have := map[key]bool{}
			for _, e := range got {
				k := key{e.Path, e.Kind}
				assert.False(t, have[k], "reported twice: %v", e)
				have[k] = true
				if e.Kind == kindUnknownKey {
					assert.Contains(t, e.Detail, "authors")
				} else if p, ok := kindPhrases[e.Kind]; ok {
					assert.Contains(t, e.Detail, p, "detail of %v", k)
				}
			}
			assert.Equal(t, want, have)
		})
	}
}

func TestMappingRejectsRenamedKeys(t *testing.T) {
	for old, src := range map[string]string{
		"timetree": "importer: a@example.com\ncalendars:\n  - timetree: x\n",
		"authors":  "importer: a@example.com\nauthors:\n  1: b@example.com\n",
	} {
		_, err := parseMapping(strings.NewReader(src))
		var me mappingError
		require.ErrorAs(t, err, &me, old)
		assert.Equal(t, kindUnknownKey, me.Kind, old)
		assert.Contains(t, me.Detail, old)
	}
	m := mustParse(t, "importer: a@example.com\nsource_users:\n  1: b@example.com\ncalendars:\n  - source: x\n")
	assert.Equal(t, "x", m.Calendars[0].Source)
	assert.Equal(t, map[string]string{"1": "b@example.com"}, m.SourceUsers)
}

func TestSyncGuardFixtures(t *testing.T) {
	var f struct {
		Version int `json:"version"`
		Cases   []struct {
			Name    string  `json:"name"`
			Present int     `json:"present"`
			Synced  int     `json:"synced"`
			Delete  int     `json:"delete"`
			Moved   int     `json:"moved"`
			Refuse  *string `json:"refuse"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(readCoreFixture(t, "sync-guard.json"), &f))
	require.Equal(t, 1, f.Version)
	require.NotEmpty(t, f.Cases)
	for _, c := range f.Cases {
		t.Run(c.Name, func(t *testing.T) {
			want := ""
			if c.Refuse != nil {
				want = *c.Refuse
			}
			got, _ := guard(c.Present, c.Synced, c.Delete, c.Moved)
			assert.Equal(t, want, got)
		})
	}
}

func TestReportLines(t *testing.T) {
	t.Run("sync plan", func(t *testing.T) {
		var sb strings.Builder
		all := []*analysis{{data: calData{cal: ttCalendar{ID: "1"}, repeated: 1}}}
		res := resolution{Plans: []calendarPlan{{TT: ttCalendar{ID: "1"}, Name: "Marketing"}}}
		syncs := map[flexID]*calendarSync{"1": {
			plan:  calimport.SyncPlan{Create: 3, Update: 1, Unchanged: 2, Delete: make([]uint32, 4), Kept: 1, Synced: 7, Orphaned: 2},
			moved: 1,
		}}
		writeSyncPlans(&sb, all, res, syncs)
		assert.Equal(t, `calendar "Marketing": sync plan events create=3 update=1 unchanged=2 delete=4 moved=1 kept=1 synced=7 orphaned=2 repeated=1`+"\n", sb.String())
	})

	t.Run("dry-run tail", func(t *testing.T) {
		var sb strings.Builder
		writeDryRunTail(&sb, accounts.Restriction{}, resolution{})
		assert.Equal(t, "sign-in restriction: open\nusers to create: none\nvalidation errors: 0\n", sb.String())

		sb.Reset()
		writeDryRunTail(&sb, accounts.Restriction{Domains: 2}, resolution{
			NewUsers: []string{"a@x", "b@y"},
			Errors:   []string{"importer: boom\nsecond line", "users[0].email: bad"},
		})
		assert.Equal(t, "sign-in restriction: domains=2 allowlist=0\n"+
			"users to create: a@x, b@y\n"+
			"validation errors: 2\n"+
			"  - importer: boom second line\n"+
			"  - users[0].email: bad\n", sb.String())

		sb.Reset()
		writeDryRunTail(&sb, accounts.Restriction{Allowlist: 1}, resolution{})
		assert.Contains(t, sb.String(), "sign-in restriction: domains=0 allowlist=1\n")
	})

	t.Run("run validation errors", func(t *testing.T) {
		var sb strings.Builder
		writeErrors(&sb, []string{"calendars[0].source: matches nothing"})
		assert.Equal(t, "validation errors: 1\n  - calendars[0].source: matches nothing\n", sb.String())
	})

	t.Run("refusals", func(t *testing.T) {
		plans := []calendarPlan{{TT: ttCalendar{ID: "1"}, Name: "Marketing"}, {TT: ttCalendar{ID: "2"}, Name: "Sales"}}
		syncs := map[flexID]*calendarSync{
			"1": {present: ids("a"), plan: calimport.SyncPlan{Synced: 20, Delete: make([]uint32, 11)}},
			"2": {present: ids(), plan: calimport.SyncPlan{Synced: 4, Delete: make([]uint32, 3)}},
		}
		assert.Equal(t, []string{
			`calendar "Marketing" would delete 11 of 20 synced events`,
			`TimeTree returned no events for calendar "Sales" but 4 are synced`,
		}, refusals(plans, syncs))
	})

	t.Run("calendar and total", func(t *testing.T) {
		c := counts{imported: 5, duplicates: 1, skipped: 1, failed: 1}
		var sb strings.Builder
		writeCalendarResult(&sb, "Marketing", "created", 2, 1, c)
		writeTotal(&sb, c, 1, 2)
		assert.Equal(t,
			`calendar "Marketing" (created): members added=2 existing=1; events imported=5 updated=0 duplicates=1 deleted=0 kept=0 moved=0 skipped=1 failed=1 rejected=0`+"\n"+
				`total: events imported=5 updated=0 duplicates=1 deleted=0 kept=0 moved=0 skipped=1 failed=1 rejected=0; calendars_created=1 users_created=2`+"\n",
			sb.String())
	})
}
