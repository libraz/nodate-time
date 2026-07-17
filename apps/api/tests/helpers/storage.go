package helpers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// The presign responses no longer echo the storage key back to the client
// (see finding #34), so tests that need to reach into storage directly
// reconstruct the key from the same layout the handlers build it with.

// AvatarStorageKey mirrors users.avatarStorageKey.
func AvatarStorageKey(userID, avatarID string) string {
	return fmt.Sprintf("avatars/%s/%s", userID, avatarID)
}

// AlbumPhotoStorageKey mirrors albums.photoStorageKey.
func AlbumPhotoStorageKey(calendarID, photoID string) string {
	return fmt.Sprintf("albums/%s/%s", calendarID, photoID)
}

// AttachmentStorageKey mirrors the key events.PresignUpload builds.
func AttachmentStorageKey(calendarID, eventID, attachmentID string) string {
	return fmt.Sprintf("attachments/%s/%s/%s", calendarID, eventID, attachmentID)
}

// UploadToPresignedURL performs an HTTP PUT to a presigned URL, mimicking what
// a browser would do. It fatals on any non-2xx response.
func UploadToPresignedURL(t *testing.T, presignedURL, contentType string, body []byte) {
	t.Helper()
	status, respBody := UploadToPresignedURLStatus(t, presignedURL, contentType, body)
	if status < 200 || status >= 300 {
		t.Fatalf("presigned PUT failed: status=%d body=%s", status, string(respBody))
	}
}

// UploadToPresignedURLStatus is like UploadToPresignedURL but returns the
// status instead of failing the test, for negative-path tests — e.g. a body
// whose length does not match what was bound into the presigned URL's
// signature.
func UploadToPresignedURLStatus(t *testing.T, presignedURL, contentType string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, presignedURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = int64(len(body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put presigned: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody
}

// PutRawObject writes bytes directly into a bucket using the same test MinIO
// credentials the API's storage client uses, bypassing any presigned URL —
// and so bypassing the Content-Type/Content-Length binding a presigned PUT
// enforces. Used to simulate an object landing in storage with metadata that
// disagrees with what was declared at presign time, to exercise the
// Confirm-time mismatch cleanup as defense-in-depth independent of how such a
// mismatch might occur.
func PutRawObject(t *testing.T, bucket, key, contentType string, body []byte) {
	t.Helper()
	endpoint := os.Getenv("TC_S3_ENDPOINT")
	if endpoint == "" {
		endpoint = testMinioEndpoint
	}
	access := os.Getenv("TC_S3_ACCESS_KEY")
	if access == "" {
		access = testMinioAccess
	}
	secret := os.Getenv("TC_S3_SECRET_KEY")
	if secret == "" {
		secret = testMinioSecret
	}
	mc, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: false})
	if err != nil {
		t.Fatalf("raw minio client: %v", err)
	}
	if _, err := mc.PutObject(context.Background(), bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: contentType}); err != nil {
		t.Fatalf("put raw object: %v", err)
	}
}

// FetchURL does an HTTP GET on a presigned URL and returns the bytes. Fatals
// on any non-2xx response.
func FetchURL(t *testing.T, url string) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get presigned: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("presigned GET failed: status=%d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

// TinyPNG returns a 1x1 transparent PNG byte slice. Useful for upload tests
// where the bytes are not visually inspected.
func TinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x62, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
}
