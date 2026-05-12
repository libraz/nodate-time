package helpers

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

// UploadToPresignedURL performs an HTTP PUT to a presigned URL, mimicking what
// a browser would do. It fatals on any non-2xx response.
func UploadToPresignedURL(t *testing.T, presignedURL, contentType string, body []byte) {
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		t.Fatalf("presigned PUT failed: status=%d body=%s", resp.StatusCode, string(buf))
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
