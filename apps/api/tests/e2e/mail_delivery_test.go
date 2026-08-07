package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// stalledMailer accepts a message and then holds it, the way a relay that
// completes the TCP handshake and stops answering holds a sender.
type stalledMailer struct {
	entered chan struct{}
	hold    time.Duration
}

func (m *stalledMailer) Send(ctx context.Context, _ mailer.Message) error {
	select {
	case m.entered <- struct{}{}:
	default:
	}
	select {
	case <-time.After(m.hold):
	case <-ctx.Done():
	}
	return nil
}

// A stalled relay must cost a background goroutine, not the request that asked
// for the mail: the endpoint answers on its own timescale, and it answers the
// same way whether or not the address exists.
func TestPasswordResetAnswersWhileTheRelayIsStalled(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	relay := &stalledMailer{entered: make(chan struct{}, 8), hold: 5 * time.Second}
	srv := helpers.NewTestServerWithMailer(t, testDB, mailer.NewDispatcher(relay, mailer.DispatcherOptions{}))
	tt := helpers.NewTenant(t, srv.BaseURL)

	start := time.Now()
	status, known := helpers.DoJSONStatus(t, http.MethodPost, srv.BaseURL+"/auth/password-reset/request", "",
		map[string]any{"email": tt.Email})
	elapsed := time.Since(start)
	require.Equal(t, http.StatusOK, status)
	require.Less(t, elapsed, 2*time.Second,
		"the request waited %v on the relay instead of handing the send off", elapsed)

	select {
	case <-relay.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no delivery was attempted at all")
	}

	status2, unknown := helpers.DoJSONStatus(t, http.MethodPost, srv.BaseURL+"/auth/password-reset/request", "",
		map[string]any{"email": uniqueEmail()})
	require.Equal(t, status, status2, "the response reveals whether the address exists")
	require.JSONEq(t, string(known), string(unknown), "the response reveals whether the address exists")
}
