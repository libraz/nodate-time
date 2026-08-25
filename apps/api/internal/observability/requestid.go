package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
)

type ctxKeyRequestID struct{}

// RequestIDHeader is the header the identifier is read from and answered in.
const RequestIDHeader = "X-Request-Id"

// maxRequestIDLen bounds an identifier accepted from upstream. It is written
// into every log line the request produces, so it is a value a caller can put
// in the logs -- length is the cheapest half of not letting that matter.
const maxRequestIDLen = 64

// NewRequestID returns a fresh identifier. Sixteen hex characters: enough that
// two live requests colliding is not a thing that happens, short enough to
// read off a terminal and grep for.
func NewRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on any supported platform, and an
		// identifier is not worth refusing a request over. An empty id makes
		// the line unlinkable, not wrong.
		return ""
	}
	return hex.EncodeToString(b[:])
}

// SanitizeRequestID reports whether s is safe to adopt as this request's
// identifier, and returns it unchanged when it is.
//
// The character set is the other half of not letting a caller-supplied value
// matter: logs are JSON, so a quote or a newline in an attribute would be
// escaped rather than injected, but the same string is also read by eye and by
// grep, and there is no reason to carry anything but an identifier's alphabet.
func SanitizeRequestID(s string) (string, bool) {
	if s == "" || len(s) > maxRequestIDLen {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.':
		default:
			return "", false
		}
	}
	return s, true
}

// WithRequestID stores the identifier for the current request.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID{}, id)
}

// RequestIDFromContext retrieves the identifier stored by the middleware.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyRequestID{}).(string)
	return v, ok
}

// requestIDKey is the attribute name the identifier is logged under. It has to
// be the same on every line or the thing it exists for -- collecting one
// request's lines out of everyone else's -- does not work.
const requestIDKey = "request_id"

// ContextHandler is a slog.Handler that copies the request identifier out of
// the context onto every record logged under it.
//
// This is what makes the identifier worth carrying. The handlers in this
// application already log through the *Context variants -- slog.ErrorContext
// and friends -- but slog itself does nothing with the context it is handed,
// so those lines are unattributable: an error is known to have happened, and
// not which of the requests in flight it belonged to. Wrapping the handler
// attributes all of them without changing a single call site.
type ContextHandler struct {
	// derived is the handler as the caller built it, with any attributes and
	// groups already applied. It serves every record.
	derived slog.Handler
	// base and ops are only used once a group has been opened; see Handle.
	base    slog.Handler
	ops     []handlerOp
	inGroup bool
}

// handlerOp records one derivation so it can be replayed. Exactly one field is
// set.
type handlerOp struct {
	attrs []slog.Attr
	group string
}

// NewContextHandler wraps inner so that records logged with a context carrying
// a request identifier gain a request_id attribute.
func NewContextHandler(inner slog.Handler) *ContextHandler {
	return &ContextHandler{derived: inner, base: inner}
}

func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.derived.Enabled(ctx, level)
}

func (h *ContextHandler) Handle(ctx context.Context, rec slog.Record) error {
	id, ok := RequestIDFromContext(ctx)
	if !ok || id == "" {
		return h.derived.Handle(ctx, rec)
	}

	if !h.inGroup {
		// Nothing has opened a group, so an attribute added to the record
		// lands at the top level, which is where it has to be. This is the
		// path essentially every line takes, and it allocates nothing beyond
		// the clone slog already requires.
		rec = rec.Clone()
		rec.AddAttrs(slog.String(requestIDKey, id))
		return h.derived.Handle(ctx, rec)
	}

	// Inside a group, an attribute added to the record would be nested under
	// the group's name -- so a subsystem that called slog.WithGroup would log
	// its identifier under a different key than everything else, which is the
	// one thing this attribute must not do. The identifier is instead applied
	// to the handler as it was before the group was opened, and the
	// derivations are replayed on top.
	handler := h.base.WithAttrs([]slog.Attr{slog.String(requestIDKey, id)})
	for _, op := range h.ops {
		if op.group != "" {
			handler = handler.WithGroup(op.group)
			continue
		}
		handler = handler.WithAttrs(op.attrs)
	}
	return handler.Handle(ctx, rec)
}

// WithAttrs and WithGroup have to rewrap, and to remember what they did: a
// derived logger that lost the wrapper would silently stop attributing its
// lines, and one that lost the record of how it was derived could not put the
// identifier back in the right place.
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.with(handlerOp{attrs: attrs}, h.derived.WithAttrs(attrs), h.inGroup)
}

func (h *ContextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.with(handlerOp{group: name}, h.derived.WithGroup(name), true)
}

func (h *ContextHandler) with(op handlerOp, derived slog.Handler, inGroup bool) slog.Handler {
	// Copied rather than appended in place: two loggers derived from the same
	// parent would otherwise share the backing array and overwrite each
	// other's last derivation.
	ops := make([]handlerOp, len(h.ops), len(h.ops)+1)
	copy(ops, h.ops)
	return &ContextHandler{
		derived: derived,
		base:    h.base,
		ops:     append(ops, op),
		inGroup: inGroup,
	}
}
