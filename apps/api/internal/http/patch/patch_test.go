package patch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
)

type body struct {
	Title patch.Field[string]   `json:"title,omitzero" required:"false"`
	Tags  patch.Field[[]string] `json:"tags,omitzero" required:"false"`
}

func TestFieldDistinguishesAbsentNullAndValue(t *testing.T) {
	var b body
	if err := json.Unmarshal([]byte(`{"title":null,"tags":["a"]}`), &b); err != nil {
		t.Fatal(err)
	}
	if !b.Title.Present() || !b.Title.IsNull() {
		t.Fatalf("title: present=%v null=%v, want null", b.Title.Present(), b.Title.IsNull())
	}
	if tags, ok := b.Tags.Get(); !ok || len(tags) != 1 || tags[0] != "a" {
		t.Fatalf("tags = %v, %v", tags, ok)
	}

	var empty body
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Title.Present() || empty.Tags.Present() {
		t.Fatal("absent members must not be present")
	}

	var zero body
	if err := json.Unmarshal([]byte(`{"title":""}`), &zero); err != nil {
		t.Fatal(err)
	}
	if v, ok := zero.Title.Get(); !ok || v != "" {
		t.Fatalf("empty string must be a value, got %q %v", v, ok)
	}
}

func TestFieldMarshal(t *testing.T) {
	out, err := json.Marshal(body{Title: patch.Of("x"), Tags: patch.Null[[]string]()})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"title":"x","tags":null}` {
		t.Fatalf("got %s", out)
	}
	out, _ = json.Marshal(body{})
	if string(out) != `{}` {
		t.Fatalf("absent fields must be omitted, got %s", out)
	}
}

type patchInput struct {
	Body body
}

func TestFieldSchemaIsOptionalAndNullable(t *testing.T) {
	_, api := humatest.New(t)
	var got body
	huma.Register(api, huma.Operation{OperationID: "patch", Method: http.MethodPatch, Path: "/r"},
		func(_ context.Context, in *patchInput) (*struct{}, error) {
			got = in.Body
			return nil, nil
		})

	spec, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec, &doc); err != nil {
		t.Fatal(err)
	}
	s, ok := doc.Components.Schemas["Body"]
	if !ok {
		t.Fatalf("Body schema missing: %s", spec)
	}
	if len(s.Required) != 0 {
		t.Fatalf("members must be optional, required = %v", s.Required)
	}
	for _, name := range []string{"title", "tags"} {
		var prop struct {
			Type any `json:"type"`
		}
		if err := json.Unmarshal(s.Properties[name], &prop); err != nil {
			t.Fatal(err)
		}
		types, _ := prop.Type.([]any)
		hasNull := false
		for _, ty := range types {
			hasNull = hasNull || ty == "null"
		}
		if !hasNull {
			t.Fatalf("%s must be nullable, schema = %s", name, s.Properties[name])
		}
	}

	if resp := api.Patch("/r", map[string]any{"title": nil}); resp.Code >= 300 {
		t.Fatalf("null title refused: %d %s", resp.Code, resp.Body.String())
	}
	if !got.Title.IsNull() || got.Tags.Present() {
		t.Fatalf("handler saw title null=%v tags present=%v", got.Title.IsNull(), got.Tags.Present())
	}
	if resp := api.Patch("/r", map[string]any{}); resp.Code >= 300 {
		t.Fatalf("empty body refused: %d %s", resp.Code, resp.Body.String())
	}
	if resp := api.Patch("/r", map[string]any{"title": 5}); resp.Code != 422 {
		t.Fatalf("wrong type status = %d, want 422", resp.Code)
	}
}
