package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeRequestIDAcceptsOnlyAnIdentifiersAlphabet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"hex", "a1b2c3d4e5f60718", true},
		{"uuid", "018f2c4e-6a7b-7c8d-9e0f-123456789abc", true},
		{"underscored", "edge_worker_42", true},
		{"dotted", "svc.edge.7", true},
		{"empty", "", false},
		{"space", "two words", false},
		// JSON escapes these rather than letting them break the line, but the
		// same value is read by eye and by grep and has no business carrying
		// them.
		{"newline", "abc\ndef", false},
		{"quote", `abc"def`, false},
		{"control", "abc\x00def", false},
		{"non-ascii", "識別子", false},
		{"too long", strings.Repeat("a", 65), false},
		{"at the limit", strings.Repeat("a", 64), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := SanitizeRequestID(tc.in)
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				assert.Equal(t, tc.in, got)
			}
		})
	}
}

func TestNewRequestIDIsUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		id := NewRequestID()
		require.Len(t, id, 16)
		_, dup := seen[id]
		require.False(t, dup, "two requests would share the identifier %q", id)
		seen[id] = struct{}{}
	}
}

// The point of the wrapper: the handlers in this application already log
// through the *Context variants, and slog on its own does nothing with the
// context, so those lines cannot be tied to the request that produced them.
func TestContextHandlerAttributesLinesToTheRequest(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))
	ctx := WithRequestID(context.Background(), "a1b2c3d4e5f60718")

	logger.ErrorContext(ctx, "with a request")
	logger.Error("without one")

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 2)
	assert.Equal(t, "a1b2c3d4e5f60718", lines[0]["request_id"])
	assert.NotContains(t, lines[1], "request_id",
		"a line logged outside a request gained an identifier from nowhere")
}

// A logger derived with attributes or a group is still a logger for the same
// request. Losing the wrapper here would silently stop attributing whole
// subsystems -- the ones careful enough to use slog.With.
func TestDerivedLoggersKeepAttributing(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))
	ctx := WithRequestID(context.Background(), "deadbeefdeadbeef")

	base.With("component", "mailer").ErrorContext(ctx, "derived with attrs")
	base.WithGroup("delivery").ErrorContext(ctx, "derived with a group")

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 2)
	for _, line := range lines {
		assert.Equal(t, "deadbeefdeadbeef", line["request_id"])
	}
}

func decodeLines(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	for dec.More() {
		var m map[string]any
		require.NoError(t, dec.Decode(&m))
		out = append(out, m)
	}
	return out
}
