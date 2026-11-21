package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

type shapeBody struct {
	Required string  `json:"required"`
	Optional string  `json:"optional" required:"false"`
	Omitted  string  `json:"omitted,omitempty"`
	Nullable *string `json:"nullable" required:"false" nullable:"true"`
}

type shapeInput struct {
	Body shapeBody
}

type shapeOutput struct {
	Body shapeBody
}

type guardedInput struct {
	IfMatch string `header:"If-Match"`
}

func (g *guardedInput) Resolve(huma.Context) []error {
	if g.IfMatch == "" {
		return []error{huma.Error428PreconditionRequired("If-Match is required")}
	}
	return nil
}

// newShapeProbe builds an API on the shared config and returns its mux and
// document.
func newShapeProbe(t *testing.T) (http.Handler, map[string]any) {
	t.Helper()
	cfg := docConfig("Probe", "1.0.0", "")
	r := chi.NewRouter()
	api := humachi.New(r, cfg)
	huma.Post(api, "/shape", func(_ context.Context, in *shapeInput) (*shapeOutput, error) {
		return &shapeOutput{Body: in.Body}, nil
	})
	huma.Put(api, "/guarded", func(context.Context, *guardedInput) (*struct{}, error) {
		return nil, nil
	})
	serveDocs(r, cfg)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	return r, doc
}

func shapeSchema(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for name, s := range schemas {
		if strings.HasPrefix(name, "ShapeBody") {
			return s.(map[string]any)
		}
	}
	t.Fatalf("no ShapeBody schema in %v", schemas)
	return nil
}

func TestOptionalFieldsAreNotRequired(t *testing.T) {
	t.Parallel()
	_, doc := newShapeProbe(t)
	s := shapeSchema(t, doc)

	var required []string
	for _, v := range s["required"].([]any) {
		required = append(required, v.(string))
	}
	require.Contains(t, required, "required")
	require.NotContains(t, required, "optional", "required:\"false\" drops the field from required")
	require.NotContains(t, required, "omitted", "omitempty drops the field from required")
}

func TestNullableFieldAdmitsNull(t *testing.T) {
	t.Parallel()
	_, doc := newShapeProbe(t)
	props := shapeSchema(t, doc)["properties"].(map[string]any)

	nullable := props["nullable"].(map[string]any)["type"]
	require.Equal(t, []any{"string", "null"}, nullable)
	require.Equal(t, "string", props["required"].(map[string]any)["type"])
}

func TestValidationFailureIs422(t *testing.T) {
	t.Parallel()
	h, _ := newShapeProbe(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shape", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestResolverStatusErrorKeepsItsStatus(t *testing.T) {
	t.Parallel()
	h, _ := newShapeProbe(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/guarded", nil))
	require.Equal(t, http.StatusPreconditionRequired, rec.Code)
}
