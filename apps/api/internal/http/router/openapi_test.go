package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestPublicPathPrefix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		url  string
		want string
	}{
		{"served at the host root", "https://app.example.com", ""},
		{"trailing slash is not a prefix", "https://app.example.com/", ""},
		{"published under a path", "https://app.example.com/api", "/api"},
		{"trailing slash is trimmed", "https://app.example.com/api/", "/api"},
		{"more than one segment", "https://app.example.com/svc/api", "/svc/api"},
		{"port is not part of the path", "http://localhost:8080", ""},
		{"unset", "", ""},
		{"unparseable", "://nonsense", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, publicPathPrefix(tc.url))
		})
	}
}

type schemaProbeOutput struct {
	Body struct {
		Name string `json:"name"`
	}
}

// newSchemaProbe builds a one-route API on the shared config, the way Build
// does, and returns the mux to make requests against.
func newSchemaProbe(t *testing.T, apiPublicURL string) (http.Handler, huma.Config) {
	t.Helper()
	cfg := docConfig("Probe", "1.0.0", apiPublicURL)
	r := chi.NewRouter()
	api := humachi.New(r, cfg)
	huma.Get(api, "/probe", func(context.Context, *struct{}) (*schemaProbeOutput, error) {
		out := &schemaProbeOutput{}
		out.Body.Name = "probe"
		return out, nil
	})
	serveDocs(r, cfg)
	return r, cfg
}

// schemaFieldOf returns the $schema link a response body carries. reqURL is
// absolute because the link is built from the host the request names.
func schemaFieldOf(t *testing.T, h http.Handler, reqURL string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, reqURL, nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	link, ok := body["$schema"].(string)
	require.True(t, ok, "the response carries no $schema field to check: %s", rec.Body.String())
	return link
}

// TestSchemaLinksNameThePublicPath covers an API published under a path by a
// reverse proxy that strips it. The link in the body is resolved by a client
// against the public address, so it has to carry the prefix; the route serving
// it is reached with the prefix already gone, so it must not.
func TestSchemaLinksNameThePublicPath(t *testing.T) {
	t.Parallel()

	h, cfg := newSchemaProbe(t, "https://app.example.com/api")
	require.Equal(t, "/api/schemas", cfg.SchemasPath)

	// Checked whole rather than for the prefix alone: this is the URL a client
	// fetches, and without the /api it is answered by whatever else is
	// published at the host root.
	link := schemaFieldOf(t, h, "https://app.example.com/probe")
	require.Equal(t, "https://app.example.com/api/schemas/SchemaProbeOutputBody.json", link)

	// The same schema, at the path a request actually arrives on.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schemas/SchemaProbeOutputBody.json", nil))
	require.Equal(t, http.StatusOK, rec.Code, "the prefix is stripped before the request gets here")
}

// TestSchemaLinksAtTheHostRoot covers the deployment the prefix logic must not
// disturb: no prefix, so the link and the route are the same path.
func TestSchemaLinksAtTheHostRoot(t *testing.T) {
	t.Parallel()

	h, cfg := newSchemaProbe(t, "https://app.example.com")
	require.Equal(t, "/schemas", cfg.SchemasPath)

	link := schemaFieldOf(t, h, "https://app.example.com/probe")
	require.Equal(t, "https://app.example.com/schemas/SchemaProbeOutputBody.json", link,
		"an empty prefix must neither add a segment nor double the separator")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schemas/SchemaProbeOutputBody.json", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestDocsPageFetchesItsDescriptionRelatively guards the same prefix problem in
// the docs page: an absolute path would ask the host root for the document,
// which under a proxy is the SPA rather than this API.
func TestDocsPageFetchesItsDescriptionRelatively(t *testing.T) {
	t.Parallel()

	h, _ := newSchemaProbe(t, "https://app.example.com/api")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `apiDescriptionUrl="openapi.yaml"`)
	require.NotContains(t, rec.Body.String(), `apiDescriptionUrl="/openapi.yaml"`)
}
