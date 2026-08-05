package router

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
)

// The API is described by one document, but served through three chi groups
// whose authentication middleware differs. Those two facts have to be held
// apart deliberately.
//
// chi's Group shares the parent's route tree, and registering the same
// method+pattern twice overwrites the first without a word. Three huma
// instances built from three configs each register /openapi.json, /docs and
// /schemas/{schema}, so whichever group is created last owns them -- and if
// that is the admin group, the document describing the whole API sits behind
// an admin check and lists only the admin operations.
//
// So the groups share one config: one OpenAPI document and one schema
// registry, filled in by all three. The document routes are switched off in
// the groups and registered on the bare mux instead, where nothing gates them.

// docConfig is the shared configuration every route group is built from.
// SchemasPath is left at its default because it is also what the schema-link
// transformer writes into each response's $schema field; the route it names is
// re-registered on the bare mux so those links resolve without a token.
func docConfig(title, version string) huma.Config {
	cfg := huma.DefaultConfig(title, version)
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	return cfg
}

// rxSchemaRef rewrites internal component references into the URLs the schema
// route serves, matching what huma does for its own schema endpoint.
var rxSchemaRef = regexp.MustCompile(`"#/components/schemas/([^"]+)"`)

// serveDocs publishes the assembled document on the bare mux. It must run
// after every group has registered, both because the document is only complete
// then and because these registrations have to be the ones that win.
func serveDocs(r chi.Router, cfg huma.Config) {
	oapi := cfg.OpenAPI

	r.Get("/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		body, err := json.Marshal(oapi)
		if err != nil {
			http.Error(w, "failed to render the API description", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oai.openapi+json")
		w.Write(body)
	})

	r.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		body, err := oapi.YAML()
		if err != nil {
			http.Error(w, "failed to render the API description", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oai.openapi")
		w.Write(body)
	})

	// The schema route is registered by every group as well; this one is last
	// and therefore the one the tree keeps, which is what makes the $schema
	// links in responses reachable without authenticating.
	r.Get(cfg.SchemasPath+"/{schema}", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimSuffix(chi.URLParam(req, "schema"), ".json")
		schema, ok := cfg.Components.Schemas.Map()[name]
		if !ok {
			http.NotFound(w, req)
			return
		}
		body, err := json.Marshal(schema)
		if err != nil {
			http.Error(w, "failed to render the schema", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(rxSchemaRef.ReplaceAll(body, []byte(`"`+cfg.SchemasPath+`/$1.json"`)))
	})

	title := "API Reference"
	if cfg.Info != nil && cfg.Info.Title != "" {
		title = cfg.Info.Title + " Reference"
	}
	page := []byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="referrer" content="no-referrer">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>` + title + `</title>
    <link rel="stylesheet" href="https://unpkg.com/@stoplight/elements@9.0.15/styles.min.css">
  </head>
  <body style="height: 100vh;">
    <elements-api apiDescriptionUrl="/openapi.yaml" router="hash" layout="sidebar" tryItCredentialsPolicy="same-origin"></elements-api>
    <script src="https://unpkg.com/@stoplight/elements@9.0.15/web-components.min.js" integrity="sha384-ug0XVxoAdgcaAsyjrRZWXORhs7ja9Ims0KVvxLyGqAM/wCoJvVFA84kcMU1ZJMzc" crossorigin="anonymous"></script>
  </body>
</html>`)

	r.Get("/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
}
