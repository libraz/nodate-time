package blobs

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
	"github.com/stretchr/testify/require"
)

const (
	testSecret  = "a-sufficiently-long-signing-secret-for-this-test"
	testKey     = "workspace/0191d1f0/e3b0c442"
	testType    = "image/png"
	testSize    = 64
	testBaseURL = "http://api.test"
)

// mount builds the relay the way the router does. storagePresent chooses
// between a deployment with object storage and one without; the client it
// gets is pointed at an address nothing listens on, because every case this
// file covers is decided before any byte reaches the bucket.
func mount(t *testing.T, storagePresent bool) (http.Handler, *storage.Signer) {
	t.Helper()
	signer := storage.NewSigner(testBaseURL, []byte(testSecret))

	var sc *storage.Client
	if storagePresent {
		var err error
		sc, err = storage.NewClient("127.0.0.1:1", "key", "secret", "bucket", false, signer)
		require.NoError(t, err)
	}

	r := chi.NewRouter()
	r.Mount(storage.BlobPath, Handler(Deps{Storage: sc, Signer: signer}))
	return r, signer
}

// requestPath turns a minted absolute URL into the path+query the router sees.
func requestPath(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u.RequestURI()
}

// --- the write matrix ---------------------------------------------------
//
// Four things about a relayed write vary independently, and the handler has to
// decide on all of them: whether the URL is signed for this request, whether
// the deployment has object storage at all, whether the body is the length the
// URL reserved, and whether it is the media type the URL reserved. Rather than
// pick a few combinations by eye, every one is run and checked against the
// contract below.

type sigKind int

const (
	sigValid sigKind = iota
	sigTampered
	sigExpired
)

type lengthKind int

const (
	lengthExact lengthKind = iota
	lengthOver
	lengthUnder
	lengthChunked
)

type typeKind int

const (
	typeExact typeKind = iota
	typeDifferent
	typeWithParameters
)

func (k sigKind) String() string {
	return [...]string{"signature valid", "signature tampered", "signature expired"}[k]
}

func (k lengthKind) String() string {
	return [...]string{"length exact", "length over", "length under", "length chunked"}[k]
}

func (k typeKind) String() string {
	return [...]string{"type exact", "type different", "type with parameters"}[k]
}

// expectedStatus states the contract as ordered rules rather than as a copy of
// the handler: the cheapest and most conclusive checks come first, so a
// request that is wrong in several ways is reported by the first thing that is
// wrong with it.
//
// The last case is the one that gets through. It cannot assert a success,
// because the bucket in this test does not exist -- what it asserts is that
// nothing turned the request away, which is the part this file is about.
func expectedStatus(sig sigKind, storagePresent bool, length lengthKind, ct typeKind) int {
	switch {
	case sig != sigValid:
		return http.StatusForbidden
	case !storagePresent:
		return http.StatusServiceUnavailable
	case length == lengthOver:
		return http.StatusRequestEntityTooLarge
	case length != lengthExact:
		return http.StatusBadRequest
	case ct == typeDifferent:
		return http.StatusUnsupportedMediaType
	default:
		return http.StatusBadGateway // reached storage, which is unreachable here
	}
}

func TestWriteMatrix(t *testing.T) {
	for _, sig := range []sigKind{sigValid, sigTampered, sigExpired} {
		for _, storagePresent := range []bool{true, false} {
			for _, length := range []lengthKind{lengthExact, lengthOver, lengthUnder, lengthChunked} {
				for _, ct := range []typeKind{typeExact, typeDifferent, typeWithParameters} {
					name := fmt.Sprintf("%s/storage=%t/%s/%s", sig, storagePresent, length, ct)
					t.Run(name, func(t *testing.T) {
						handler, signer := mount(t, storagePresent)

						expires := 15 * time.Minute
						if sig == sigExpired {
							expires = -time.Minute
						}
						raw, err := signer.PutURL(testKey, testType, testSize, expires)
						require.NoError(t, err)
						target := requestPath(t, raw)
						if sig == sigTampered {
							target = strings.Replace(target, "sig=", "sig=00", 1)
						}

						req := httptest.NewRequest(http.MethodPut, target, bytes.NewReader(make([]byte, testSize)))
						switch length {
						case lengthExact:
							req.ContentLength = testSize
						case lengthOver:
							req.ContentLength = testSize + 1
						case lengthUnder:
							req.ContentLength = testSize - 1
						case lengthChunked:
							req.ContentLength = -1
						}
						switch ct {
						case typeExact:
							req.Header.Set("Content-Type", testType)
						case typeDifferent:
							req.Header.Set("Content-Type", "text/html")
						case typeWithParameters:
							req.Header.Set("Content-Type", testType+"; charset=utf-8")
						}

						rec := httptest.NewRecorder()
						handler.ServeHTTP(rec, req)
						require.Equal(t, expectedStatus(sig, storagePresent, length, ct), rec.Code)
					})
				}
			}
		}
	}
}

// --- reads --------------------------------------------------------------

func TestReadRefusesAnUnsignedRequest(t *testing.T) {
	handler, _ := mount(t, true)

	for _, target := range []string{
		storage.BlobPath + "/" + testKey,
		storage.BlobPath + "/" + testKey + "?exp=99999999999",
		storage.BlobPath + "/" + testKey + "?exp=99999999999&sig=" + strings.Repeat("0", 64),
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusForbidden, rec.Code, target)
	}
}

// An expired read is refused with the same answer as an unsigned one, so
// nothing about which keys exist leaks to a caller holding a guess.
func TestReadRefusesAnExpiredURL(t *testing.T) {
	handler, signer := mount(t, true)
	raw, err := signer.GetURL(testKey, -time.Minute)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, requestPath(t, raw), nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// A HEAD is the same read without a body, so it is accepted against the read's
// own signature rather than needing one minted for HEAD.
func TestHeadIsAcceptedAgainstTheReadSignature(t *testing.T) {
	handler, signer := mount(t, true)
	raw, err := signer.GetURL(testKey, time.Hour)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, requestPath(t, raw), nil))
	require.Equal(t, http.StatusBadGateway, rec.Code, "should reach storage rather than be refused")
}

// A deployment without object storage answers that the store is missing rather
// than that the object is: the object was never looked for.
func TestReadWithoutStorageIsUnavailableRatherThanNotFound(t *testing.T) {
	handler, signer := mount(t, false)
	raw, err := signer.GetURL(testKey, time.Hour)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, requestPath(t, raw), nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// A write URL cannot be replayed as a read, nor a read URL as a write: the
// method is under the signature.
func TestSignaturesDoNotCrossMethods(t *testing.T) {
	handler, signer := mount(t, true)

	readURL, err := signer.GetURL(testKey, time.Hour)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, requestPath(t, readURL), bytes.NewReader(nil))
	req.Header.Set("Content-Type", testType)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, "a read URL must not authorize a write")

	writeURL, err := signer.PutURL(testKey, testType, testSize, time.Hour)
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, requestPath(t, writeURL), nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "a write URL must not authorize a read")
}
