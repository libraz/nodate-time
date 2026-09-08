// Package blobs relays object bytes between the browser and object storage.
//
// The object store has no route from the internet: it listens only on the
// private network, and nothing publishes a port for it. A browser that needs
// to read a picture or write an upload therefore talks to this API, which
// copies the bytes through.
//
// The routes are unauthenticated in the sense that no middleware runs on them,
// because neither caller can carry a session -- an <img> tag has nowhere to
// put an Authorization header, and the upload is a bare PUT of the file. What
// stands in for the session is the signature the URL carries, minted by the
// handler that already decided the caller was allowed this object. See
// storage.Signer.
package blobs

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

type Deps struct {
	// Storage is nil in a deployment without object storage, which answers
	// every blob route 503 rather than 404: the object is not missing, the
	// thing that would hold it is.
	Storage *storage.Client
	Signer  *storage.Signer
}

// Handler builds the relay routes, to be mounted at storage.BlobPath.
func Handler(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Get("/*", read(deps))
	// A HEAD is the same read answered without a body, so it is accepted
	// against the same signature rather than needing one of its own.
	r.Head("/*", read(deps))
	r.Put("/*", write(deps))
	return r
}

func read(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := chi.URLParam(r, "*")
		grant, err := deps.Signer.Verify(http.MethodGet, key, r.URL.Query(), time.Now())
		if err != nil {
			refuse(w, r, key, err)
			return
		}
		if deps.Storage == nil {
			http.Error(w, "object storage is not configured", http.StatusServiceUnavailable)
			return
		}

		obj, found, err := deps.Storage.Open(r.Context(), key)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to open blob", "key", key, "error", err)
			http.Error(w, "object storage is unavailable", http.StatusBadGateway)
			return
		}
		if !found {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer obj.Close()

		// The stored type is only honoured for types a browser renders as a
		// picture and nothing else. Anything else is served as an attachment
		// of inert bytes, because this response comes from the API's own
		// origin: content a browser executes would execute there, next to the
		// session. nosniff stops it being reinterpreted past that.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if grant.Download || !storage.InlineAllowed(obj.Info.ContentType) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", storage.ContentDisposition(grant.Filename))
		} else {
			w.Header().Set("Content-Type", obj.Info.ContentType)
		}
		// Cached no longer than the URL is accepted: past the expiry the URL is
		// gone, so a cache holding the body longer holds something the page can
		// no longer re-fetch to revalidate.
		if maxAge := int(time.Until(grant.ExpiresAt).Seconds()); maxAge > 0 {
			w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(maxAge))
		}

		// ServeContent rather than io.Copy: it answers Range and conditional
		// requests from the seekable object, which is what lets a large
		// attachment resume instead of starting over.
		http.ServeContent(w, r, "", obj.ModTime, obj)
	}
}

func write(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := chi.URLParam(r, "*")
		grant, err := deps.Signer.Verify(http.MethodPut, key, r.URL.Query(), time.Now())
		if err != nil {
			refuse(w, r, key, err)
			return
		}
		if deps.Storage == nil {
			http.Error(w, "object storage is not configured", http.StatusServiceUnavailable)
			return
		}
		// A write URL without a size bound is an unlimited write to the bucket.
		// The signer refuses to mint one, so reaching here means the signature
		// covers something this handler will not carry out.
		if grant.ByteSize <= 0 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		// The length is compared before a byte is read: the point of binding it
		// into the signature is that the limit the issuing handler checked is
		// enforced before the object exists, not discovered afterwards from
		// what happens to have been stored.
		switch {
		case r.ContentLength > grant.ByteSize:
			http.Error(w, "body is larger than the reserved size", http.StatusRequestEntityTooLarge)
			return
		case r.ContentLength != grant.ByteSize:
			// Also catches a chunked body, which arrives as -1: without a
			// declared length there is nothing to hold to the reservation.
			http.Error(w, "body length does not match the reserved size", http.StatusBadRequest)
			return
		}
		if !storage.SameMediaType(r.Header.Get("Content-Type"), grant.ContentType) {
			http.Error(w, "content type does not match the reserved type", http.StatusUnsupportedMediaType)
			return
		}

		// Stored as the signed type rather than as whatever the request said,
		// so the media type in the bucket is the one that was authorized even
		// if the header carried extra parameters.
		body := http.MaxBytesReader(w, r.Body, grant.ByteSize)
		if err := deps.Storage.Put(r.Context(), key, body, grant.ByteSize, grant.ContentType); err != nil {
			slog.ErrorContext(r.Context(), "failed to store blob", "key", key, "error", err)
			http.Error(w, "object storage is unavailable", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// refuse answers an unsigned, tampered or aged-out request. Both causes answer
// 403 with the same body: telling them apart tells a caller holding a guess
// whether the key it guessed is real.
func refuse(w http.ResponseWriter, r *http.Request, key string, err error) {
	if errors.Is(err, storage.ErrURLExpired) {
		slog.DebugContext(r.Context(), "blob url expired", "key", key)
	} else {
		slog.WarnContext(r.Context(), "blob url rejected", "key", key, "error", err)
	}
	http.Error(w, "forbidden", http.StatusForbidden)
}
