package e2e

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The update keeps the stored value of a field the body omits, and clients
// rely on it: a drag sends no showAs or flexibility. The published schema is
// what a client is written against, so it has to make every field optional
// rather than describe the create defaults.
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
				Required   []string `json:"required"`
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &doc))

	patch := doc.Paths["/calendars/{calendarId}/events/{eventId}"]["patch"]
	ref := patch.RequestBody.Content["application/json"].Schema.Ref
	require.NotEmpty(t, ref)
	schema, ok := doc.Components.Schemas[strings.TrimPrefix(ref, "#/components/schemas/")]
	require.True(t, ok, "schema %s", ref)

	for _, field := range []string{"showAs", "flexibility", "visibility"} {
		prop, ok := schema.Properties[field]
		require.True(t, ok, "%s is patchable", field)
		require.NotContains(t, prop.Description, "defaults to", "%s: %q", field, prop.Description)
	}
	require.Empty(t, schema.Required, "an update body omits whatever it leaves alone")
}
