package apierrors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/blobref"
)

func TestFromBlobref(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want *Spec
	}{
		{blobref.ErrDigestMismatch, UploadDigestMismatch},
		{fmt.Errorf("confirm: %w", blobref.ErrBadDigest), UploadDigestMismatch},
		{blobref.ErrTooLarge, UploadTooLarge},
		{errors.New("other"), nil},
	} {
		got, ok := FromBlobref(tc.err)
		if got != tc.want || ok != (tc.want != nil) {
			t.Errorf("FromBlobref(%v) = %v, %v", tc.err, got, ok)
		}
	}
}

func TestStaleSpecsAnswer412(t *testing.T) {
	for _, s := range []*Spec{EventStale, SchedulePollStale, SchedulePollResponseStale, CalendarStale,
		MemoStale, ChecklistStale, CommentStale, AlbumStale} {
		if s.Status != 412 {
			t.Errorf("%s answers %d, want 412", s.Code, s.Status)
		}
	}
}
