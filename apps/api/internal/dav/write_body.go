package dav

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/emersion/go-webdav/caldav"
)

const ctxKeyPutBody ctxKey = ctxKeyIfMatch + 1

// bufferPut reads a PUT body whole, bounded by the largest resource a client
// may store, and keeps the bytes for the backend: go-webdav hands it only the
// decoded calendar, and the write path parses the text itself. It reports
// false once it has answered the request.
func bufferPut(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if r.ContentLength > maxResourceSize {
		writePrecondition(w, caldav.PreconditionMaxResourceSize)
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxResourceSize+1))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return nil, false
	}
	if len(body) > maxResourceSize {
		writePrecondition(w, caldav.PreconditionMaxResourceSize)
		return nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 || !utf8.Valid(body) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return nil, false
	}
	req := r.WithContext(context.WithValue(r.Context(), ctxKeyPutBody, body))
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return req, true
}

// putBodyFrom returns the PUT body bufferPut kept.
func putBodyFrom(ctx context.Context) []byte {
	b, _ := ctx.Value(ctxKeyPutBody).([]byte)
	return b
}

// writePrecondition answers 409 with a CalDAV precondition element, as
// caldav.NewPreconditionError does once the request has reached go-webdav.
func writePrecondition(w http.ResponseWriter, p caldav.PreconditionType) {
	type element struct {
		XMLName xml.Name
	}
	type davError struct {
		XMLName xml.Name `xml:"DAV: error"`
		Element element
	}
	out, _ := xml.Marshal(davError{Element: element{XMLName: xml.Name{Space: "urn:ietf:params:xml:ns:caldav", Local: string(p)}}})
	w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusConflict)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(out)
}
