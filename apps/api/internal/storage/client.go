package storage

import (
	"context"
	"net/http"
	"net/url"
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

// PresignPut returns a presigned PUT URL for uploading an object. When
// contentType is non-empty it is bound into the signature so the client must
// send a matching Content-Type header, preventing callers from uploading an
// arbitrary type (e.g. HTML) to an image-only slot.
func (c *Client) PresignPut(ctx context.Context, key string, contentType string, expires time.Duration) (string, error) {
	if contentType == "" {
		u, err := c.mc.PresignedPutObject(ctx, c.bucket, key, expires)
		if err != nil {
			return "", err
		}
		return u.String(), nil
	}
	headers := http.Header{}
	headers.Set("Content-Type", contentType)
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
func (c *Client) PresignDownload(ctx context.Context, key string, expires time.Duration) (string, error) {
	params := downloadResponseParams()
	u, err := c.mc.PresignedGetObject(ctx, c.bucket, key, expires, params)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func downloadResponseParams() url.Values {
	params := url.Values{}
	params.Set("response-content-disposition", "attachment")
	params.Set("response-content-type", "application/octet-stream")
	return params
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
