// Package patch carries merge-patch fields, where a JSON member can be absent
// (leave unchanged), null (clear) or a value (replace).
package patch

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

type state uint8

const (
	absent state = iota
	null
	value
)

// Field is one member of a merge-patch body. The zero value is absent. Tag the
// struct field `json:"name,omitzero" required:"false"` so the generated schema
// marks it optional.
type Field[T any] struct {
	state state
	v     T
}

// Of returns a Field holding v.
func Of[T any](v T) Field[T] { return Field[T]{state: value, v: v} }

// Null returns a Field that clears the member.
func Null[T any]() Field[T] { return Field[T]{state: null} }

// Present reports whether the member was in the body, as null or a value.
func (f Field[T]) Present() bool { return f.state != absent }

// IsNull reports whether the member was an explicit null.
func (f Field[T]) IsNull() bool { return f.state == null }

// Get returns the value and whether the member carried one.
func (f Field[T]) Get() (T, bool) { return f.v, f.state == value }

// IsZero lets encoding/json omit an absent member under omitzero.
func (f Field[T]) IsZero() bool { return f.state == absent }

// UnmarshalJSON records null and values; an absent member never reaches it.
func (f *Field[T]) UnmarshalJSON(b []byte) error {
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		*f = Field[T]{state: null}
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*f = Field[T]{state: value, v: v}
	return nil
}

// MarshalJSON writes the value, or null for null and absent fields.
func (f Field[T]) MarshalJSON() ([]byte, error) {
	if f.state != value {
		return []byte("null"), nil
	}
	return json.Marshal(f.v)
}

// Schema makes the member nullable in the OpenAPI document.
func (Field[T]) Schema(r huma.Registry) *huma.Schema {
	s := r.Schema(reflect.TypeFor[T](), true, "")
	if s.Ref != "" {
		return &huma.Schema{OneOf: []*huma.Schema{{Ref: s.Ref}, {Type: "null"}}}
	}
	c := *s
	c.Nullable = true
	return &c
}
