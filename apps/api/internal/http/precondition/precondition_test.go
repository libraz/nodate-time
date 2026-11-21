package precondition_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

const current = `"20260101T000000.000"`

type input struct {
	IfMatch precondition.IfMatch `header:"If-Match" required:"true"`
}

type output struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

func newAPI(t *testing.T) humatest.TestAPI {
	t.Helper()
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "write", Method: http.MethodPatch, Path: "/r"},
		func(_ context.Context, in *input) (*output, error) {
			if err := in.IfMatch.Check(current); err != nil {
				if errors.Is(err, precondition.ErrStale) {
					return nil, huma.Error412PreconditionFailed("stale")
				}
				return nil, err
			}
			out := &output{}
			out.Body.OK = true
			return out, nil
		})
	return api
}

func TestIfMatchStatuses(t *testing.T) {
	cases := []struct {
		name   string
		header []any
		want   int
	}{
		{"missing", nil, 428},
		{"blank", []any{"If-Match: "}, 428},
		{"star", []any{"If-Match: *"}, 200},
		{"match", []any{"If-Match: " + current}, 200},
		{"match among spaces", []any{"If-Match:   " + current + "  "}, 200},
		{"stale", []any{`If-Match: "20250101T000000.000"`}, 412},
		{"weak", []any{"If-Match: W/" + current}, 412},
		{"multiple including match", []any{"If-Match: " + current + `, "x"`}, 412},
		{"unquoted", []any{"If-Match: abc"}, 400},
		{"unterminated", []any{`If-Match: "abc`}, 400},
		{"star with tag", []any{`If-Match: *, "a"`}, 400},
		{"trailing comma", []any{"If-Match: " + current + ","}, 400},
		{"space inside tag", []any{`If-Match: "a b"`}, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := newAPI(t).Patch("/r", c.header...)
			if resp.Code != c.want {
				t.Fatalf("status = %d, want %d: %s", resp.Code, c.want, resp.Body.String())
			}
		})
	}
}

func TestResolveErrorsAreMatchable(t *testing.T) {
	var h precondition.IfMatch
	if err := h.Check(current); !errors.Is(err, precondition.ErrMalformed) {
		t.Fatalf("empty header Check = %v, want ErrMalformed", err)
	}
	if got := precondition.ErrRequired.GetStatus(); got != 428 {
		t.Fatalf("ErrRequired status = %d", got)
	}
	if got := precondition.ErrMalformed.GetStatus(); got != 400 {
		t.Fatalf("ErrMalformed status = %d", got)
	}
}
