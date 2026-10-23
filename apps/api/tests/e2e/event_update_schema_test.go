package e2e

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The update keeps the stored value of an optional field the body omits, and
// clients rely on it: a drag sends no showAs or flexibility. The published
// schema is what a client is written against, so it has to say so rather than
// describe the create defaults.
func TestEventUpdateSchemaSaysOmittedOptionalFieldsAreKept(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	status, body := getRaw(t, testServerURL+"/openapi.json")
	require.Equal(t, 200, status)

	var doc struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &doc))

	put := doc.Paths["/calendars/{calendarId}/events/{eventId}"]["put"]
	ref := put.RequestBody.Content["application/json"].Schema.Ref
	require.NotEmpty(t, ref)
	schema, ok := doc.Components.Schemas[strings.TrimPrefix(ref, "#/components/schemas/")]
	require.True(t, ok, "schema %s", ref)

	for _, field := range []string{"showAs", "flexibility", "visibility"} {
		desc := schema.Properties[field].Description
		require.Contains(t, desc, "omitted keeps the current value", "%s: %q", field, desc)
		require.NotContains(t, desc, "defaults to", "%s: %q", field, desc)
	}
}
