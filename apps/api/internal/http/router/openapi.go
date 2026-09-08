package router

import (
	"encoding/json"
	"net/http"
	"net/url"
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
//
// SchemasPath is set to the path a client would fetch a schema at, not the one
// this server receives. huma writes it verbatim into each response's $schema
// field and Link header, and resolves those against the request's host alone --
// so under a reverse proxy publishing the API at /api, the default names
// https://host/schemas/... while the schema is only reachable at
// https://host/api/schemas/.... The route itself is registered separately, at
// the path requests actually arrive on. See serveDocs.
func docConfig(title, version, apiPublicURL string) huma.Config {
	cfg := huma.DefaultConfig(title, version)
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = publicPathPrefix(apiPublicURL) + schemasRoutePath
	return cfg
}

// schemasRoutePath is where the schema route is registered: the path a request
// arrives on, which is this server's own root whatever it is published under.
// Its public counterpart is cfg.SchemasPath, which carries the prefix.
const schemasRoutePath = "/schemas"

// publicPathPrefix is the path the API is published under, as a prefix that can
// be put in front of a route: "" when it is served at the root of its host, and
// otherwise a leading slash with no trailing one ("/api").
//
// An unparseable or absent URL yields "", which is the no-prefix deployment --
// the same answer as a URL naming a bare host, and the one that leaves every
// link pointing where it did before.
func publicPathPrefix(apiPublicURL string) string {
	if apiPublicURL == "" {
		return ""
	}
	u, err := url.Parse(apiPublicURL)
	if err != nil {
		return ""
	}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		return ""
	}
	return "/" + p
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
	//
	// Registered at the path requests arrive on, while the links written into
	// responses carry the public prefix -- the two differ whenever a proxy
	// strips one. What the body refers to is likewise the public path, because
	// it is a client that follows it.
	r.Get(schemasRoutePath+"/{schema}", func(w http.ResponseWriter, req *http.Request) {
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
    <!-- Relative, so the document is fetched next to this page rather than at
         the host's root: under a proxy publishing the API at /api, an absolute
         path would ask the SPA for it and be answered with the SPA. -->
    <elements-api apiDescriptionUrl="openapi.yaml" router="hash" layout="sidebar" tryItCredentialsPolicy="same-origin"></elements-api>
    <script src="https://unpkg.com/@stoplight/elements@9.0.15/web-components.min.js" integrity="sha384-ug0XVxoAdgcaAsyjrRZWXORhs7ja9Ims0KVvxLyGqAM/wCoJvVFA84kcMU1ZJMzc" crossorigin="anonymous"></script>
  </body>
</html>`)

	r.Get("/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
}
