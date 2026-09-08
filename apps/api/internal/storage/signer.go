package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Errors a relayed request can be turned away with. They are distinguished for
// the log, not for the caller: both answer 403, because telling an unsigned
// caller which one it was tells it whether it guessed a real key.
var (
	// ErrURLInvalid means the signature did not cover the request as it arrived.
	ErrURLInvalid = errors.New("blob url signature is invalid")
	// ErrURLExpired means the signature was good but the URL has aged out.
	ErrURLExpired = errors.New("blob url has expired")
	// ErrSignerUnconfigured means no public base URL was given, so there is no
	// address a browser could be sent to.
	ErrSignerUnconfigured = errors.New("blob url base is not configured")
)

// blobURLLabel domain-separates the blob-URL key from the signing secret's
// other uses. Derived rather than configured so a deployment has one secret to
// keep rather than two, and separated so a blob URL can never be presented as,
// or help forge, a session token.
const blobURLLabel = "nodate-time/blob-url/v1"

// BlobPath is where the relay is mounted. The signer builds it into every URL
// it mints, so the route and the URLs cannot drift apart.
const BlobPath = "/blobs"

// inlineTypes are the media types a blob may be served with as itself. This is
// an allowlist rather than a denylist because the relay answers on the API's
// own origin: a type a browser executes -- HTML, SVG, anything scriptable --
// would run there, with the session in reach. Everything not named here is
// served as an attachment of inert bytes instead.
//
// The upload paths already restrict what can be stored (avatars and album
// photos take these types and refuse SVG; attachments take anything but are
// only ever downloaded). This repeats the restriction at the point of serving,
// where getting it wrong is what would actually cost something.
var inlineTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

// InlineAllowed reports whether contentType may be served as itself rather
// than forced to download.
func InlineAllowed(contentType string) bool {
	return inlineTypes[mediaType(contentType)]
}

// SameMediaType reports whether two Content-Type headers name the same media
// type. Parameters are ignored: a request declaring `image/png; charset=utf-8`
// is the same picture as one declaring `image/png`, and what gets stored is
// the type that was signed either way.
func SameMediaType(a, b string) bool {
	return mediaType(a) == mediaType(b)
}

// mediaType reduces a Content-Type to the type itself, lowercased and without
// parameters, which is the form every comparison in this package wants.
func mediaType(contentType string) string {
	base, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(base))
}

// Signer mints and checks the short-lived URLs a browser uses to read and
// write blobs.
//
// Object storage has no route from the internet: the bytes are relayed by this
// API. What the browser gets is a URL back to this API carrying its own
// authorization -- an HMAC over the object key, the expiry, and, for a write,
// the exact size and media type the object is allowed to be.
//
// The authorization travels in the URL rather than a header because the two
// callers cannot send one: an <img> tag has nowhere to put an Authorization
// header, and the upload is a bare fetch(url, {method: 'PUT'}) whose whole
// point is that it does not carry the session.
type Signer struct {
	base string
	key  []byte
}

// NewSigner derives the URL-signing key from secret. base is the public origin
// this API is reached at (TC_API_PUBLIC_URL), because the URLs are handed to a
// browser; a trailing slash and any path prefix are preserved, so an API
// served under a path keeps it.
func NewSigner(base string, secret []byte) *Signer {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(blobURLLabel))
	return &Signer{base: strings.TrimRight(base, "/"), key: mac.Sum(nil)}
}

// SetBase points the signer at a different public origin. It exists for tests,
// which cannot know the address their server will be given until it is
// listening.
func (s *Signer) SetBase(base string) {
	s.base = strings.TrimRight(base, "/")
}

// Grant is what a verified blob URL permits.
type Grant struct {
	// ContentType is the media type a write must declare and is stored as. It
	// is empty on a read.
	ContentType string
	// ByteSize is the exact length a write must have. It is zero on a read.
	ByteSize int64
	// Download forces the read to be served as an attachment of inert bytes.
	Download bool
	// Filename is the name a downloaded object is saved under.
	Filename string
	// ExpiresAt is when the URL stops being accepted. Set by Verify and unused
	// when minting: a response may be cached up to here and no further,
	// because past it the URL is gone and a cache holding the body longer
	// holds something the page can no longer re-fetch.
	ExpiresAt time.Time
}

// params is everything a signature covers besides the method and the key.
type params struct {
	exp int64
	Grant
}

// GetURL returns a URL that reads the object as itself, for as long as expires.
func (s *Signer) GetURL(key string, expires time.Duration) (string, error) {
	return s.mint("GET", key, params{exp: time.Now().Add(expires).Unix()})
}

// DownloadURL returns a URL that reads the object as an attachment named
// filename, served as inert bytes whatever it holds.
func (s *Signer) DownloadURL(key, filename string, expires time.Duration) (string, error) {
	return s.mint("GET", key, params{
		exp:   time.Now().Add(expires).Unix(),
		Grant: Grant{Download: true, Filename: filename},
	})
}

// PutURL returns a URL that writes exactly byteSize bytes of contentType.
//
// Both are bound into the signature, so the caller's request must match or it
// is turned away -- this is what keeps a size the issuing handler checked
// against its own limit from being a number only re-examined after a much
// larger object could already have been stored.
//
// A byteSize that is not positive is refused rather than signed without the
// binding: an unbound URL is an unlimited write to the bucket.
func (s *Signer) PutURL(key, contentType string, byteSize int64, expires time.Duration) (string, error) {
	if byteSize <= 0 {
		return "", errors.New("blob put url: byte size must be positive")
	}
	return s.mint("PUT", key, params{
		exp:   time.Now().Add(expires).Unix(),
		Grant: Grant{ContentType: contentType, ByteSize: byteSize},
	})
}

// Verify checks a relayed request against the signature it carries and answers
// with what that signature permits.
//
// Everything the URL says about the transfer is signed, so a caller cannot
// widen it: raising the size to store more than was reserved, or swapping the
// media type to have a script stored as an image, changes the bytes under the
// MAC and the request stops here.
//
// method is the signed method, which is GET for both GET and HEAD -- a HEAD is
// the same read, answered without the body.
func (s *Signer) Verify(method, key string, q url.Values, now time.Time) (Grant, error) {
	exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
	if err != nil {
		return Grant{}, ErrURLInvalid
	}
	var byteSize int64
	if raw := q.Get("sz"); raw != "" {
		if byteSize, err = strconv.ParseInt(raw, 10, 64); err != nil {
			return Grant{}, ErrURLInvalid
		}
	}
	p := params{
		exp: exp,
		Grant: Grant{
			ContentType: q.Get("ct"),
			ByteSize:    byteSize,
			Download:    q.Get("dl") == "1",
			Filename:    q.Get("fn"),
		},
	}

	// Compared in constant time: a comparison that stops at the first wrong
	// byte reports how much of a guess was right, which is enough to find the
	// rest a byte at a time.
	if !hmac.Equal([]byte(s.sign(method, key, p)), []byte(q.Get("sig"))) {
		return Grant{}, ErrURLInvalid
	}
	// Checked after the signature so that an unsigned caller cannot learn
	// anything from the difference between the two answers.
	expiresAt := time.Unix(exp, 0)
	if !now.Before(expiresAt) {
		return Grant{}, ErrURLExpired
	}
	p.ExpiresAt = expiresAt
	return p.Grant, nil
}

func (s *Signer) mint(method, key string, p params) (string, error) {
	if s.base == "" {
		return "", ErrSignerUnconfigured
	}
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(p.exp, 10))
	if p.ContentType != "" {
		q.Set("ct", p.ContentType)
	}
	if p.ByteSize != 0 {
		q.Set("sz", strconv.FormatInt(p.ByteSize, 10))
	}
	if p.Download {
		q.Set("dl", "1")
	}
	if p.Filename != "" {
		q.Set("fn", p.Filename)
	}
	q.Set("sig", s.sign(method, key, p))
	return s.base + BlobPath + "/" + escapeKey(key) + "?" + q.Encode(), nil
}

func (s *Signer) sign(method, key string, p params) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(canonical(
		method,
		key,
		strconv.FormatInt(p.exp, 10),
		p.ContentType,
		strconv.FormatInt(p.ByteSize, 10),
		strconv.FormatBool(p.Download),
		p.Filename,
	))
	return hex.EncodeToString(mac.Sum(nil))
}

// canonical joins fields so that no set of values can produce the same bytes
// as a different set: each is written with its length, so a value holding the
// separator cannot borrow characters from the field after it. A plain
// delimiter would let a filename ending in the delimiter shift the boundary
// and sign a transfer nobody authorized.
func canonical(fields ...string) []byte {
	var b strings.Builder
	for _, f := range fields {
		b.WriteString(strconv.Itoa(len(f)))
		b.WriteByte(':')
		b.WriteString(f)
	}
	return []byte(b.String())
}

// escapeKey percent-encodes each segment of an object key while leaving the
// separators alone, so the key arrives back as the path it was signed as.
func escapeKey(key string) string {
	segments := strings.Split(key, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}
