package storage

import (
	"context"
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

// PresignPut returns a presigned PUT URL for uploading an object.
func (c *Client) PresignPut(ctx context.Context, key string, contentType string, expires time.Duration) (string, error) {
	u, err := c.mc.PresignedPutObject(ctx, c.bucket, key, expires)
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

// DeleteObject removes an object from the bucket. Returns nil if the key is empty.
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	return c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{})
}

// StatObject returns true if the object exists in the bucket.
func (c *Client) StatObject(ctx context.Context, key string) (bool, error) {
	_, err := c.mc.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.StatusCode == 404 || resp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
