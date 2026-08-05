package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// getRaw fetches a URL with no credentials and returns the status and body.
func getRaw(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// TestOpenAPIDescribesTheWholeAPI verifies that the description of the API is
// readable without credentials and covers every route group, not just whichever
// one happened to register the path last.
func TestOpenAPIDescribesTheWholeAPI(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	status, body := getRaw(t, testServerURL+"/openapi.json")
	require.Equal(t, 200, status, "reading the API description must not require a token")

	var doc struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &doc))
	require.Equal(t, "3.1.0", doc.OpenAPI)

	// One path from each group, so a document that lost a whole group fails
	// rather than merely shrinking.
	for _, path := range []string{
		"/auth/refresh",                  // public
		"/calendars",                     // authenticated
		"/calendars/{calendarId}/events", // authenticated
		"/share/{token}/events",          // public share
		"/admin/oauth-providers",         // admin
	} {
		require.Contains(t, doc.Paths, path)
	}
	require.Greater(t, len(doc.Paths), 40, "the document should describe the API, not one group of it")

	// The YAML rendering is the one the docs page loads.
	yamlStatus, yamlBody := getRaw(t, testServerURL+"/openapi.yaml")
	require.Equal(t, 200, yamlStatus)
	require.Contains(t, yamlBody, "openapi: 3.1.0")

	docsStatus, docsBody := getRaw(t, testServerURL+"/docs")
	require.Equal(t, 200, docsStatus)
	require.Contains(t, docsBody, "/openapi.yaml")
}

// TestResponseSchemaLinksResolve verifies that the $schema URL a response
// carries can actually be fetched, which it could not while the route was
// owned by the admin group.
func TestResponseSchemaLinksResolve(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	status, body := getRaw(t, testServerURL+"/schemas/CalendarResponse.json")
	require.Equal(t, 200, status)
	require.Contains(t, body, `"type"`)

	missing, _ := getRaw(t, testServerURL+"/schemas/NoSuchSchema.json")
	require.Equal(t, 404, missing)
}
