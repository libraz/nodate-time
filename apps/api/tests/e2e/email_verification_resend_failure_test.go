package e2e

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// switchableMailer records what it accepts and refuses everything once told
// to, the way a dispatcher with a full queue does.
type switchableMailer struct {
	mu       sync.Mutex
	refusing bool
	sent     []mailer.Message
}

func (m *switchableMailer) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refusing {
		return errors.New("mail not queued")
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *switchableMailer) refuse() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refusing = true
}

func (m *switchableMailer) lastToken(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	require.NotEmpty(t, m.sent, "nothing was sent")
	match := verifyTokenPattern.FindStringSubmatch(m.sent[len(m.sent)-1].Text)
	require.Len(t, match, 2)
	return match[1]
}

// Asking for a new confirmation link retires the old one. When the new one
// cannot be sent, the old one must survive and the caller must be told, not
// left holding no working link and an answer saying it worked.
func TestResendThatSendsNothingKeepsThePreviousLink(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	m := &switchableMailer{}
	srv := helpers.NewTestServerWithMailer(t, testDB, m)

	email := uniqueEmail()
	var reg struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, srv.BaseURL+"/auth/register", "",
		map[string]any{"name": "Refused Resend", "email": email, "password": "password123"}, &reg)
	first := m.lastToken(t)

	m.refuse()
	status, body := helpers.DoJSONStatus(t, http.MethodPost, srv.BaseURL+"/user/verify-email/resend", reg.Token, nil)
	require.GreaterOrEqual(t, status, 400, "a resend that sent nothing must not report success: %s", string(body))
	require.NotContains(t, string(body), `"ok":true`)

	status, body = helpers.DoJSONStatus(t, http.MethodPost, srv.BaseURL+"/auth/verify-email/confirm", reg.Token,
		map[string]any{"token": first})
	require.Equal(t, http.StatusOK, status, "the previous link must still work: %s", string(body))
}
