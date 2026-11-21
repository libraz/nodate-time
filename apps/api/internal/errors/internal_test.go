package apierrors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestRespondLogsUnexpectedErrorOnce(t *testing.T) {
	buf := captureLog(t)

	got := Respond(context.Background(), fmt.Errorf("load row: %w", errors.New("connection refused")))

	he, ok := got.(*HumaError)
	if !ok || he.Status != http.StatusInternalServerError || he.Code != InternalUnexpected.Code {
		t.Fatalf("got %#v, want the internal-error response", got)
	}
	if n := strings.Count(buf.String(), "connection refused"); n != 1 {
		t.Fatalf("cause logged %d times, want 1: %q", n, buf.String())
	}
}

func TestRespondKeepsDeclaredSpecWithoutLogging(t *testing.T) {
	buf := captureLog(t)

	got := Respond(context.Background(), fmt.Errorf("resolve: %w", CalendarNotFound))

	he, ok := got.(*HumaError)
	if !ok || he.Status != CalendarNotFound.Status || he.Code != CalendarNotFound.Code {
		t.Fatalf("got %#v, want the declared spec", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("a declared refusal was logged: %q", buf.String())
	}
}

// TestHandlersRecordTheCauseOf500 fails when a handler builds the internal
// error response directly instead of going through Internal or Respond, which
// would answer 500 without recording why.
func TestHandlersRecordTheCauseOf500(t *testing.T) {
	root := filepath.Join("..", "http")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "ToHuma(apierrors.InternalUnexpected)") || strings.Contains(line, "ToHuma(InternalUnexpected)") {
				t.Errorf("%s:%d: use apierrors.Internal(ctx, err) so the cause is logged", path, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
