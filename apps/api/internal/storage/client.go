package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client wraps the MinIO S3 client.
type Client struct {
	mc     *minio.Client
	bucket string
}

// NewClient creates a new MinIO storage client.
func NewClient(endpoint, accessKey, secretKey, bucket string, useSSL bool) (*Client, error) {
	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, err
	}
	return &Client{mc: mc, bucket: bucket}, nil
}

// EnsureBucket creates the bucket if it does not exist.
func (c *Client) EnsureBucket(ctx context.Context) error {
	exists, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return c.mc.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{})
	}
	return nil
}

// PresignPut returns a presigned PUT URL for uploading an object of exactly
// byteSize bytes. Content-Type and Content-Length are both bound into the
// signature, so the client's actual request must carry a matching
// Content-Type and a body of exactly that length or the signature is
// rejected — this is what keeps a byteSize the caller validated against its
// own limit (e.g. 100MB attachments) from being a value trusted only at
// Confirm time, when a much larger object could already have been written.
//
// A byteSize that is not positive is refused rather than signed without the
// length binding: an unbound URL accepts a body of any size, so a caller that
// let a zero through would be handing out an unlimited write to the bucket.
// contentType == "" skips the type binding.
func (c *Client) PresignPut(ctx context.Context, key string, contentType string, byteSize int64, expires time.Duration) (string, error) {
	if byteSize <= 0 {
		return "", fmt.Errorf("presign put %s: byte size must be positive, got %d", key, byteSize)
	}
	headers := http.Header{}
	if contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	headers.Set("Content-Length", strconv.FormatInt(byteSize, 10))
	u, err := c.mc.PresignHeader(ctx, http.MethodPut, c.bucket, key, expires, url.Values{}, headers)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// PresignGet returns a presigned GET URL for downloading an object.
func (c *Client) PresignGet(ctx context.Context, key string, expires time.Duration) (string, error) {
	u, err := c.mc.PresignedGetObject(ctx, c.bucket, key, expires, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// PresignDownload returns a presigned GET URL that forces browsers to download
// the object as inert bytes instead of rendering potentially active content.
// filename, if non-empty, is carried in Content-Disposition so the saved file
// keeps its original name (including non-ASCII characters) instead of the
// bucket's opaque, extensionless object key.
func (c *Client) PresignDownload(ctx context.Context, key string, filename string, expires time.Duration) (string, error) {
	params := downloadResponseParams(filename)
	u, err := c.mc.PresignedGetObject(ctx, c.bucket, key, expires, params)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func downloadResponseParams(filename string) url.Values {
	params := url.Values{}
	params.Set("response-content-disposition", contentDisposition(filename))
	params.Set("response-content-type", "application/octet-stream")
	return params
}

// contentDisposition builds an attachment Content-Disposition value per
// RFC 6266 / RFC 5987: filename carries an ASCII-safe fallback for older
// clients, filename* carries the exact, percent-encoded UTF-8 name for
// clients that support it (all modern browsers).
func contentDisposition(filename string) string {
	if filename == "" {
		return "attachment"
	}
	return `attachment; filename="` + asciiFallbackFilename(filename) + `"; filename*=UTF-8''` + rfc5987Encode(filename)
}

// asciiFallbackFilename replaces anything outside printable ASCII (and the
// characters that would break the quoted-string) with "_", so it is always
// safe inside filename="...".
func asciiFallbackFilename(filename string) string {
	var b strings.Builder
	for _, r := range filename {
		if r < 0x20 || r > 0x7E || r == '"' || r == '\\' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "download"
	}
	return b.String()
}

// rfc5987AttrChars is the RFC 5987 "attr-char" set: everything else in an
// ext-value must be percent-encoded.
const rfc5987AttrChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!#$&+-.^_`|~"

func rfc5987Encode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(rfc5987AttrChars, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// DeleteObject removes an object from the bucket. Returns nil if the key is empty.
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	return c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{})
}

// ObjectInfo is the subset of object metadata needed by API confirmation
// handlers.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// SHA256 streams the stored object and returns the digest of its bytes.
//
// The digest a client declares before uploading decides which object its
// bytes are stored as, so it cannot also be taken as proof of what they are.
// This is the only thing that can say what actually landed.
func (c *Client) SHA256(ctx context.Context, key string) ([]byte, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	h := sha256.New()
	if _, err := io.Copy(h, obj); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// StatObject returns object metadata and whether the object exists.
func (c *Client) StatObject(ctx context.Context, key string) (ObjectInfo, bool, error) {
	info, err := c.mc.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.StatusCode == 404 || resp.Code == "NoSuchKey" {
			return ObjectInfo{}, false, nil
		}
		return ObjectInfo{}, false, err
	}
	return ObjectInfo{Size: info.Size, ContentType: info.ContentType}, true, nil
}
