// Package precondition parses and compares the If-Match header of a
// conditional write.
//
// An operation takes IfMatch as a required header field. When the header is
// absent or empty the request is refused with 428; a header that is not valid
// entity-tag syntax is refused with 400. Check then compares the header with
// the current tag of the resource and reports ErrStale, which each handler maps
// to the stale-write error of its own resource (412).
package precondition

import (
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// Error is a refusal of the If-Match header itself. It is a huma.StatusError so
// a Resolver can return it, and the error-mapping layer matches it with
// errors.Is against ErrRequired and ErrMalformed to pick the wire code.
type Error struct {
	status  int
	message string
}

func (e *Error) Error() string  { return e.message }
func (e *Error) GetStatus() int { return e.status }

var (
	// ErrRequired is a conditional write sent without an If-Match header (428).
	ErrRequired = &Error{status: http.StatusPreconditionRequired, message: "If-Match header is required"}
	// ErrMalformed is an If-Match value that is not a list of entity tags or "*" (400).
	ErrMalformed = &Error{status: http.StatusBadRequest, message: "If-Match header is malformed"}
)

// ErrStale is a well-formed If-Match that does not name the current version of
// the resource (412). Handlers translate it into their resource's stale error.
var ErrStale = errors.New("precondition: resource has changed")

// IfMatch is the If-Match request header of a conditional write.
type IfMatch string

// Resolve refuses a missing or syntactically invalid header.
func (h *IfMatch) Resolve(ctx huma.Context) []error {
	raw := strings.TrimSpace(ctx.Header("If-Match"))
	if raw == "" {
		return []error{ErrRequired}
	}
	if _, err := parse(raw); err != nil {
		return []error{err}
	}
	return nil
}

// Check compares the header with current, the resource's quoted strong tag
// (for example `"20060102T150405.000"`). "*" matches any existing resource, so
// the caller must have found it before checking. Comparison is strong: a weak
// tag, or a list of more than one tag, never matches. A value Resolve would
// have refused is reported as ErrMalformed.
func (h IfMatch) Check(current string) error {
	p, err := parse(strings.TrimSpace(string(h)))
	if err != nil {
		return err
	}
	switch {
	case p.any:
		return nil
	case len(p.tags) != 1 || p.tags[0].weak || p.tags[0].value != current:
		return ErrStale
	}
	return nil
}

type tag struct {
	weak  bool
	value string
}

type parsed struct {
	any  bool
	tags []tag
}

func parse(raw string) (parsed, error) {
	if raw == "*" {
		return parsed{any: true}, nil
	}
	var p parsed
	rest := raw
	for {
		rest = strings.TrimLeft(rest, " \t")
		t := tag{}
		if strings.HasPrefix(rest, "W/") {
			t.weak = true
			rest = rest[2:]
		}
		if rest == "" || rest[0] != '"' {
			return parsed{}, ErrMalformed
		}
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			return parsed{}, ErrMalformed
		}
		body := rest[1 : 1+end]
		for i := 0; i < len(body); i++ {
			if c := body[i]; c == 0x20 || c < 0x21 || c == 0x7f {
				return parsed{}, ErrMalformed
			}
		}
		t.value = rest[:end+2]
		p.tags = append(p.tags, t)
		rest = strings.TrimLeft(rest[end+2:], " \t")
		if rest == "" {
			return p, nil
		}
		if rest[0] != ',' {
			return parsed{}, ErrMalformed
		}
		rest = rest[1:]
	}
}
