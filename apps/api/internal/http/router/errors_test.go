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

	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

type errorProbeInput struct {
	IfMatch precondition.IfMatch `header:"If-Match"`
	Body    struct {
		Name string `json:"name" minLength:"1"`
	}
}

func newErrorProbe(t *testing.T) (http.Handler, map[string]any) {
	t.Helper()
	cfg := docConfig("Probe", "1.0.0", "")
	r := chi.NewRouter()
	api := humachi.New(r, cfg)
	huma.Register(api, huma.Operation{
		OperationID: "probe-write",
		Method:      http.MethodPut,
		Path:        "/probe",
		Errors:      errorStatuses(apierrors.EventStale, apierrors.Validation),
	}, func(_ context.Context, in *errorProbeInput) (*struct{}, error) {
		if err := in.IfMatch.Check(`"v1"`); err != nil {
			return nil, apierrors.ToHuma(apierrors.EventStale)
		}
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

func putProbe(h http.Handler, ifMatch, body string) (int, map[string]any) {
	req := httptest.NewRequest(http.MethodPut, "/probe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestDefaultErrorSchemaCarriesCode(t *testing.T) {
	t.Parallel()

	_, doc := newErrorProbe(t)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	hErr, ok := schemas["HumaError"].(map[string]any)
	require.True(t, ok, "the document has no HumaError schema: %v", schemas)
	props := hErr["properties"].(map[string]any)
	require.Contains(t, props, "code")
	require.Contains(t, props, "message")

	responses := doc["paths"].(map[string]any)["/probe"].(map[string]any)["put"].(map[string]any)["responses"].(map[string]any)
	require.Contains(t, responses, "412")
	require.Contains(t, responses, "422")
	require.Contains(t, responses, "500")
}

func TestRequestFailuresCarryTheirCodes(t *testing.T) {
	t.Parallel()

	h, _ := newErrorProbe(t)
	for _, tc := range []struct {
		name, ifMatch, body, code string
		status                    int
	}{
		{"validation failure", `"v1"`, `{"name":""}`, "REQUEST.VALIDATION", 422},
		{"missing If-Match", "", `{"name":"x"}`, "REQUEST.PRECONDITION_REQUIRED", 428},
		{"malformed If-Match", `v1`, `{"name":"x"}`, "REQUEST.BAD_IF_MATCH", 400},
		{"stale If-Match", `"v0"`, `{"name":"x"}`, "EVENT.STALE", 412},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, body := putProbe(h, tc.ifMatch, tc.body)
			require.Equal(t, tc.status, status, "%v", body)
			require.Equal(t, tc.code, body["code"])
		})
	}
}

func TestValidationFailureKeepsFieldDetails(t *testing.T) {
	t.Parallel()

	h, _ := newErrorProbe(t)
	_, body := putProbe(h, `"v1"`, `{"name":""}`)
	details, _ := body["errors"].([]any)
	require.NotEmpty(t, details, "%v", body)
}
