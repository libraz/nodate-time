package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type jsonMap = map[string]any

func loadDocument(t *testing.T) jsonMap {
	t.Helper()
	h, _ := BuildResult(Deps{Mailer: relayStub{}, PasswordLoginEnabled: true, AuthRateLimit: -1, ShareRateLimit: -1})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var doc jsonMap
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	return doc
}

// documentedOperations indexes the document by operation id.
func documentedOperations(doc jsonMap) map[string]jsonMap {
	ops := map[string]jsonMap{}
	for path, item := range doc["paths"].(jsonMap) {
		for method, raw := range item.(jsonMap) {
			op := raw.(jsonMap)
			op["_method"] = strings.ToUpper(method)
			op["_path"] = path
			ops[op["operationId"].(string)] = op
		}
	}
	return ops
}

// resolve follows a $ref into components.schemas.
func resolve(doc jsonMap, schema jsonMap) jsonMap {
	ref, ok := schema["$ref"].(string)
	if !ok {
		return schema
	}
	name := strings.TrimPrefix(ref, "#/components/schemas/")
	return doc["components"].(jsonMap)["schemas"].(jsonMap)[name].(jsonMap)
}

func successResponse(op jsonMap, status int) (jsonMap, bool) {
	r, ok := op["responses"].(jsonMap)[fmt.Sprint(status)].(jsonMap)
	return r, ok
}

func successStatuses(op jsonMap) []string {
	var out []string
	for code := range op["responses"].(jsonMap) {
		if strings.HasPrefix(code, "2") {
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out
}

func bodySchema(doc jsonMap, resp jsonMap) (jsonMap, bool) {
	content, ok := resp["content"].(jsonMap)
	if !ok {
		return nil, false
	}
	for _, c := range content {
		if s, ok := c.(jsonMap)["schema"].(jsonMap); ok {
			return resolve(doc, s), true
		}
	}
	return nil, false
}

func hasType(schema jsonMap, want string) bool {
	switch v := schema["type"].(type) {
	case string:
		return v == want
	case []any:
		for _, x := range v {
			if x == want {
				return true
			}
		}
	}
	return false
}

func ifMatchHeader(op jsonMap) (jsonMap, bool) {
	params, _ := op["parameters"].([]any)
	for _, p := range params {
		pm := p.(jsonMap)
		if pm["in"] == "header" && strings.EqualFold(pm["name"].(string), "If-Match") {
			return pm, true
		}
	}
	return nil, false
}

func TestConventionTableMatchesDocument(t *testing.T) {
	doc := loadDocument(t)
	ops := documentedOperations(doc)

	seen := map[string]bool{}
	for _, c := range operationConventions {
		require.False(t, seen[c.ID], "duplicate row %s", c.ID)
		seen[c.ID] = true
	}

	t.Run("every row is documented", func(t *testing.T) {
		for _, c := range operationConventions {
			t.Run(c.ID, func(t *testing.T) {
				op, ok := ops[c.ID]
				require.True(t, ok, "operation %s is not documented", c.ID)
				require.Equal(t, c.Method, op["_method"], "method")
				require.Equal(t, c.Path, op["_path"], "path")
			})
		}
	})

	t.Run("every documented operation has a row", func(t *testing.T) {
		for id := range ops {
			t.Run(id, func(t *testing.T) {
				require.True(t, seen[id], "operation %s has no row", id)
			})
		}
	})
}

func TestOperationConventions(t *testing.T) {
	doc := loadDocument(t)
	ops := documentedOperations(doc)

	for _, c := range operationConventions {
		op, ok := ops[c.ID]
		if !ok {
			continue
		}
		t.Run(c.ID, func(t *testing.T) {
			t.Run("status", func(t *testing.T) {
				require.Equal(t, []string{fmt.Sprint(c.Status)}, successStatuses(op))
			})
			t.Run("guard", func(t *testing.T) { checkGuard(t, doc, op, c) })
			t.Run("list", func(t *testing.T) { checkList(t, doc, op, c) })
		})
	}
}

func checkGuard(t *testing.T, doc, op jsonMap, c convention) {
	t.Helper()
	header, has := ifMatchHeader(op)
	switch c.Guard {
	case guardRead:
		require.False(t, has, "a read takes no If-Match")
	case guardUnconditional:
		require.NotEmpty(t, c.Reason, "an unconditional write says why")
		require.False(t, has, "an unconditional write takes no If-Match")
	case guardConditional:
		require.True(t, has, "If-Match header")
		require.Equal(t, true, header["required"], "If-Match is required")
		responses := op["responses"].(jsonMap)
		require.Contains(t, responses, "412")
		require.Contains(t, responses, "428")

		resp, ok := successResponse(op, c.Status)
		require.True(t, ok)
		if c.Status != http.StatusNoContent {
			require.Contains(t, resp["headers"], "ETag", "the new etag is returned as a header")
			body, ok := bodySchema(doc, resp)
			require.True(t, ok)
			require.Contains(t, body["properties"], "etag", "the new etag is returned in the body")
		}
	}
}

func checkList(t *testing.T, doc, op jsonMap, c convention) {
	t.Helper()
	if c.List == listNone {
		return
	}
	resp, ok := successResponse(op, c.Status)
	require.True(t, ok)
	body, ok := bodySchema(doc, resp)
	require.True(t, ok)

	if c.List == listArray {
		require.True(t, hasType(body, "array"), "a bare array")
		return
	}
	props, _ := body["properties"].(jsonMap)
	require.Contains(t, props, "items")
	require.True(t, hasType(props["items"].(jsonMap), "array"), "items is an array")
	require.Contains(t, body["required"], "items")
	if c.List == listCursor {
		require.Contains(t, props, "nextCursor")
		require.NotContains(t, props, "hasMore")
	} else {
		require.Contains(t, props, "hasMore")
		require.NotContains(t, props, "nextCursor")
	}
}
