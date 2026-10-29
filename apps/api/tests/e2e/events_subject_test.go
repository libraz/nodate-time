package e2e

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

var eventKindRegistries = []string{
	"../../../../sql/core/event-kinds.yaml",
	"../../../../sql/time/event-kinds.yaml",
}

// registryKind is one entry of an event-kinds.yaml, read the way the
// conformance tooling reads it: the `- kind:` line starts an entry and the
// `subject:` and `required_payload_keys:` lines belong to it.
type registryKind struct {
	subject      string
	requiredKeys []string
}

func loadEventKinds(t *testing.T) map[string]registryKind {
	t.Helper()
	out := map[string]registryKind{}
	for _, path := range eventKindRegistries {
		f, err := os.Open(path)
		require.NoError(t, err)
		current := ""
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			switch {
			case strings.HasPrefix(line, "- kind:"):
				current = strings.TrimSpace(strings.TrimPrefix(line, "- kind:"))
				out[current] = registryKind{}
			case current != "" && strings.HasPrefix(line, "subject:"):
				k := out[current]
				k.subject = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "subject:")), `"'`)
				out[current] = k
			case current != "" && strings.HasPrefix(line, "required_payload_keys:"):
				list := strings.TrimSpace(strings.TrimPrefix(line, "required_payload_keys:"))
				list = strings.Trim(list, "[]")
				k := out[current]
				for _, key := range strings.Split(list, ",") {
					if key = strings.TrimSpace(key); key != "" {
						k.requiredKeys = append(k.requiredKeys, key)
					}
				}
				out[current] = k
			}
		}
		require.NoError(t, sc.Err())
		require.NoError(t, f.Close())
	}
	require.NotEmpty(t, out)
	return out
}

// subjectPayloadKeys is the payload key each subject table's public id is also
// written under.
var subjectPayloadKeys = map[string]string{
	"calendar_events":  "eventId",
	"calendar_memos":   "memoId",
	"album_photos":     "photoId",
	"calendar_invites": "inviteId",
}

// The events table is shared with writers that do not know this product, and
// the subject column is a plain one the writer fills: nothing lifts it out of
// the payload, so whatever another writer puts under `$.id` neither breaks
// its insert nor becomes a subject.
func TestEventsAcceptAnyPayloadWithoutDerivingASubject(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServer(t, testDB)
	tenant := helpers.NewTenant(t, srv.BaseURL)
	workspaceID := helpers.TestWorkspace(generated.New(testDB)).ID
	calendarID := internalCalendarID(t, tenant.CalendarID)

	for name, payload := range map[string]string{
		"uuid":           `{"id": "` + uuid.NewString() + `"}`,
		"longer than 36": `{"id": "` + strings.Repeat("a", 200) + `"}`,
		"not latin1":     `{"id": "` + strings.Repeat("日", 36) + `"}`,
		"nested object":  `{"id": {"task": 7}}`,
		"no id":          `{"other": 1}`,
	} {
		t.Run(name, func(t *testing.T) {
			publicID := uuid.New()
			_, err := testDB.Exec(
				`INSERT INTO events (public_id, workspace_id, calendar_id, type, payload_json, occurred_at)
				 VALUES (?, ?, ?, 'event.updated', ?, NOW(3))`,
				publicID[:], workspaceID, calendarID, payload)
			require.NoError(t, err, "a payload the core DDL accepts must insert")

			var got []byte
			require.NoError(t, testDB.QueryRow(
				`SELECT subject_public_id FROM events WHERE public_id = ?`, publicID[:]).Scan(&got))
			require.Nil(t, got)
		})
	}
}

// Every row this product writes carries the subject its kind has in the
// registries: the public id of the event, memo or invite for a kind with a
// subject, NULL for one without. The id is also in the payload under the key
// the subject table gives it, and the payload holds every key the registry
// requires for the kind.
func TestEventRowsCarryTheirRegistrySubjectAndRequiredKeys(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	registry := loadEventKinds(t)
	owner := helpers.NewTenant(t, testServerURL)
	guest := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + owner.CalendarID

	var evt struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/events", owner.AccessToken,
		eventBody("Subject check", "2026-07-02T10:00:00+09:00", "2026-07-02T11:00:00+09:00", owner.UserID), &evt)
	eventURL := calURL + "/events/" + evt.ID
	helpers.DoJSON(t, http.MethodPut, eventURL, owner.AccessToken,
		eventBody("Subject check, moved", "2026-07-02T12:00:00+09:00", "2026-07-02T13:00:00+09:00", owner.UserID), nil)
	var comment struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, eventURL+"/activities", owner.AccessToken,
		map[string]any{"content": "a comment"}, &comment)
	helpers.DoJSON(t, http.MethodPut, eventURL+"/activities/"+comment.ID, owner.AccessToken,
		map[string]any{"content": "an edited comment"}, nil)
	var item struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, eventURL+"/checklist", owner.AccessToken,
		map[string]any{"title": "pack"}, &item)
	helpers.DoJSON(t, http.MethodPut, eventURL+"/checklist/"+item.ID, owner.AccessToken,
		map[string]any{"done": true}, nil)
	helpers.DoJSON(t, http.MethodDelete, eventURL+"/checklist/"+item.ID, owner.AccessToken, nil, nil)

	memoID := createMemo(t, calURL, owner.AccessToken, "a memo")
	helpers.DoJSON(t, http.MethodPut, calURL+"/memos/"+memoID, owner.AccessToken,
		map[string]any{"done": true}, nil)
	helpers.DoJSON(t, http.MethodDelete, calURL+"/memos/"+memoID, owner.AccessToken, nil, nil)

	inv := createInvite(t, calURL, owner.AccessToken, map[string]any{"role": "editor"})
	acceptInvite(t, inv.Token, guest.AccessToken)
	helpers.DoJSON(t, http.MethodPut, calURL+"/members/"+guest.UserID+"/role", owner.AccessToken,
		map[string]any{"role": "manager"}, nil)
	helpers.DoJSON(t, http.MethodDelete, calURL+"/invites/"+inv.ID, owner.AccessToken, nil, nil)
	helpers.DoJSON(t, http.MethodDelete, calURL+"/members/"+guest.UserID, owner.AccessToken, nil, nil)
	helpers.DoJSON(t, http.MethodDelete, eventURL, owner.AccessToken, nil, nil)

	calendarPublicID, err := uuid.Parse(owner.CalendarID)
	require.NoError(t, err)
	rows, err := testDB.Query(`
		SELECT e.type, e.subject_public_id, e.payload_json
		  FROM events e JOIN calendars c ON c.id = e.calendar_id
		 WHERE c.public_id = ?`, calendarPublicID[:])
	require.NoError(t, err)
	defer rows.Close()

	seen := map[string]bool{}
	withSubject, withoutSubject := 0, 0
	for rows.Next() {
		var kind string
		var subject []byte
		var raw json.RawMessage
		require.NoError(t, rows.Scan(&kind, &subject, &raw))
		seen[kind] = true

		entry, ok := registry[kind]
		require.True(t, ok, "%s is written but listed in no registry", kind)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(raw, &payload))
		for _, key := range entry.requiredKeys {
			require.Contains(t, payload, key, "%s: payload lacks the required key %s", kind, key)
		}

		if entry.subject == "" {
			require.Nil(t, subject, "%s has no registry subject, so the column stays NULL", kind)
			withoutSubject++
			continue
		}
		require.Len(t, subject, 16, "%s must carry its %s subject", kind, entry.subject)
		key, ok := subjectPayloadKeys[entry.subject]
		require.True(t, ok, "%s: no payload key for subject table %s", kind, entry.subject)
		require.Equal(t, uuid.UUID(subject).String(), payload[key],
			"%s: payload %s must be the subject's public id", kind, key)
		withSubject++
	}
	require.NoError(t, rows.Err())

	require.Positive(t, withSubject, "no subject-bearing row was written, so nothing was checked")
	require.Positive(t, withoutSubject, "no subject-free row was written, so nothing was checked")
	for _, kind := range []string{
		"calendar.event.created", "calendar.event.updated", "calendar.event.deleted",
		"calendar.event.comment.created", "calendar.event.comment.updated",
		"calendar.event.checklist.created", "calendar.event.checklist.updated", "calendar.event.checklist.deleted",
		"calendar.memo.created", "calendar.memo.updated", "calendar.memo.deleted",
		"calendar.invite.created", "calendar.invite.revoked",
		"calendar.member.added", "calendar.member.role_changed", "calendar.member.removed",
	} {
		require.True(t, seen[kind], "the scenario never wrote %s", kind)
	}
}
