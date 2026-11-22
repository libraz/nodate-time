// Package dav serves the calendars a user belongs to over CalDAV. A client
// authenticates with HTTP Basic and an app password; every calendar the user
// is a member of is one calendar collection under their home set, and every
// series they can see is one resource in it.
//
// The handler sits in front of the go-webdav CalDAV handler and settles what
// that handler cannot: authentication, the public path prefix a reverse proxy
// adds, and the methods this server refuses outright. Paths reaching the
// backend are always canonical and carry the public prefix.
//
// What a resource contains is the export's view of the same series for the
// same reader, rendered by internal/ical; its ETag is the content hash of that
// rendering, so it changes with what this reader sees and nothing else.
package dav

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/apppassword"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// MountPath is where the handler is mounted on the API's own mux.
const MountPath = "/dav"

// maxReportBody bounds a REPORT body, which is buffered whole to log its kind.
const maxReportBody = 1 << 20

// allowedMethods is what OPTIONS advertises.
const allowedMethods = "OPTIONS, GET, HEAD, PUT, DELETE, PROPFIND, REPORT"

func init() {
	// chi answers 405 to any method it has not been told about.
	for _, m := range []string{"PROPFIND", "PROPPATCH", "REPORT", "MKCOL", "MKCALENDAR", "COPY", "MOVE"} {
		chi.RegisterMethod(m)
	}
}

// Deps are what the handler reads and writes through.
type Deps struct {
	Queries     *generated.Queries
	WorkspaceID uint32
	// AppName is the PRODID of every resource and the Basic realm.
	AppName string
	// APIPublicURL is the address the API answers at from outside; its path
	// is the prefix every href the server hands out carries.
	APIPublicURL string
	// Events are the REST event handlers' dependencies, which writes go
	// through.
	Events events.Deps
}

type server struct {
	deps   Deps
	prefix string
	realm  string
}

// Handler returns the CalDAV handler, to be mounted at MountPath.
func Handler(deps Deps) http.Handler {
	prefix := MountPath
	if u, err := url.Parse(deps.APIPublicURL); err == nil {
		prefix = strings.TrimSuffix(u.Path, "/") + MountPath
	}
	realm := strings.ReplaceAll(deps.AppName, `"`, "'")
	return &server{
		deps:   deps,
		prefix: prefix,
		realm:  `Basic realm="` + realm + `", charset="UTF-8"`,
	}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Answered without credentials and without the backend: a client probes
	// with OPTIONS before it has been asked for any.
	if r.Method == http.MethodOptions {
		w.Header().Set("DAV", "1, 3, calendar-access")
		w.Header().Set("Allow", allowedMethods)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	user, ok, err := s.authenticate(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "dav: authenticate", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if !ok {
		w.Header().Set("WWW-Authenticate", s.realm)
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	b := &backend{s: s, userID: user.ID, userUUID: user.publicID}
	t, status := b.parse(s.prefix + strings.TrimPrefix(r.URL.Path, MountPath))
	if status != 0 {
		http.Error(w, http.StatusText(status), status)
		return
	}

	switch r.Method {
	case "MKCALENDAR", "MKCOL", "COPY", "MOVE":
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	ctx := middleware.WithActor(r.Context(), user.ID)
	if r.Method == http.MethodDelete {
		ctx = context.WithValue(ctx, ctxKeyIfMatch, webdav.ConditionalMatch(r.Header.Get("If-Match")))
	}
	req := r.Clone(ctx)
	req.URL.Path = b.canonical(t)
	req.URL.RawPath = ""

	if r.Method == "REPORT" {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxReportBody+1))
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if len(body) > maxReportBody {
			http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		slog.InfoContext(ctx, "dav report", "dav.report", reportName(body))
	}
	if r.Method == http.MethodPut {
		var ok bool
		if req, ok = bufferPut(w, req); !ok {
			return
		}
	}

	h := caldav.Handler{Backend: b, Prefix: s.prefix}
	h.ServeHTTP(w, req)
}

type davUser struct {
	ID       uint32
	publicID string
}

// authenticate checks the Basic password against the live app passwords. The
// username is not consulted: the password alone names its owner, and an
// address change must not break every device. Every refusal looks the same.
func (s *server) authenticate(r *http.Request) (davUser, bool, error) {
	_, password, ok := r.BasicAuth()
	if !ok || password == "" {
		return davUser{}, false, nil
	}
	ctx := r.Context()
	q := s.deps.Queries
	row, err := q.GetDavAppPasswordByHash(ctx, apppassword.Hash(password))
	if errors.Is(err, sql.ErrNoRows) {
		return davUser{}, false, nil
	}
	if err != nil {
		return davUser{}, false, err
	}
	if row.WorkspaceID != s.deps.WorkspaceID {
		return davUser{}, false, nil
	}
	u, err := q.GetUserByID(ctx, row.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return davUser{}, false, nil
	}
	if err != nil {
		return davUser{}, false, err
	}
	pub, err := uuid.FromBytes(u.PublicID)
	if err != nil {
		return davUser{}, false, err
	}
	if err := q.TouchDavAppPassword(ctx, row.ID); err != nil {
		slog.WarnContext(ctx, "dav: record app password use", "error", err)
	}
	return davUser{ID: u.ID, publicID: pub.String()}, true, nil
}

// reportName is the local name of a REPORT body's root element.
func reportName(body []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		if el, ok := tok.(xml.StartElement); ok {
			return el.Name.Local
		}
	}
}

type ctxKey int

const ctxKeyIfMatch ctxKey = iota

// ifMatchFrom returns the If-Match a DELETE carried. go-webdav hands a PUT's
// conditions to the backend but not a DELETE's, so the handler keeps them.
func ifMatchFrom(ctx context.Context) webdav.ConditionalMatch {
	v, _ := ctx.Value(ctxKeyIfMatch).(webdav.ConditionalMatch)
	return v
}
