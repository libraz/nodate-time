package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client wraps the MinIO S3 client.
//
// It also mints the URLs a browser reads and writes blobs through. Those URLs
// address this API rather than the object store: the store is reachable only
// from the private network, so every transfer is relayed. See Signer.
type Client struct {
	mc     *minio.Client
	bucket string
	signer *Signer
}

// NewClient creates a new MinIO storage client. signer mints the browser-facing
// URLs; a nil signer leaves the deployment able to store and read blobs
// server-side but unable to hand any of them to a client.
func NewClient(endpoint, accessKey, secretKey, bucket string, useSSL bool, signer *Signer) (*Client, error) {
	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, err
	}
	return &Client{mc: mc, bucket: bucket, signer: signer}, nil
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

// Ping reports whether the object store answers and still holds the bucket
// this deployment writes to.
//
// Deliberately not EnsureBucket: a health check that repairs what it finds
// missing cannot report it, and a bucket that disappeared underneath a running
// deployment is a fact an operator needs told rather than papered over.
func (c *Client) Ping(ctx context.Context) error {
	exists, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("bucket %q does not exist", c.bucket)
	}
	return nil
}

// SignedPutURL returns a URL the browser uploads an object of exactly byteSize
// bytes of contentType through. See Signer.PutURL for what the signature binds.
func (c *Client) SignedPutURL(key, contentType string, byteSize int64, expires time.Duration) (string, error) {
	if c.signer == nil {
		return "", ErrSignerUnconfigured
	}
	return c.signer.PutURL(key, contentType, byteSize, expires)
}

// SignedGetURL returns a URL the browser reads the object through.
func (c *Client) SignedGetURL(key string, expires time.Duration) (string, error) {
	if c.signer == nil {
		return "", ErrSignerUnconfigured
	}
	return c.signer.GetURL(key, expires)
}

// SignedDownloadURL returns a URL that makes browsers save the object as inert
// bytes instead of rendering potentially active content. filename, if
// non-empty, is carried into Content-Disposition so the saved file keeps its
// original name (including non-ASCII characters) rather than the bucket's
// opaque, extensionless object key.
func (c *Client) SignedDownloadURL(key, filename string, expires time.Duration) (string, error) {
	if c.signer == nil {
		return "", ErrSignerUnconfigured
	}
	return c.signer.DownloadURL(key, filename, expires)
}

// ContentDisposition builds an attachment Content-Disposition value per
// RFC 6266 / RFC 5987: filename carries an ASCII-safe fallback for older
// clients, filename* carries the exact, percent-encoded UTF-8 name for
// clients that support it (all modern browsers).
func ContentDisposition(filename string) string {
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

// Object is an open object body.
//
// It is seekable because that is what lets the relay answer a Range request --
// a resumed download, or a player seeking within a large attachment -- without
// reading from the start and discarding the part nobody asked for.
type Object struct {
	obj     *minio.Object
	Info    ObjectInfo
	ModTime time.Time
}

func (o *Object) Read(p []byte) (int, error) { return o.obj.Read(p) }

func (o *Object) Seek(offset int64, whence int) (int64, error) { return o.obj.Seek(offset, whence) }

func (o *Object) Close() error { return o.obj.Close() }

// Open returns the object's bytes for reading, and whether it exists.
//
// The metadata is read here rather than left to the first Read: minio-go opens
// lazily, so a missing object would otherwise surface partway through a
// response that had already claimed to be a success.
func (c *Client) Open(ctx context.Context, key string) (*Object, bool, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, false, err
	}
	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		resp := minio.ToErrorResponse(err)
		if resp.StatusCode == 404 || resp.Code == "NoSuchKey" {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &Object{
		obj:     obj,
		Info:    ObjectInfo{Size: stat.Size, ContentType: stat.ContentType},
		ModTime: stat.LastModified,
	}, true, nil
}

// Put stores exactly byteSize bytes read from r under key.
//
// The size is passed rather than discovered so the object is streamed straight
// through: given a length, minio-go sends it as one request without staging a
// copy, which is what keeps a 100MB attachment from being 100MB of API memory.
func (c *Client) Put(ctx context.Context, key string, r io.Reader, byteSize int64, contentType string) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, r, byteSize, minio.PutObjectOptions{ContentType: contentType})
	return err
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
