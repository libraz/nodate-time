package e2e

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

var verifyTokenPattern = regexp.MustCompile(`/verify-email\?token=([0-9a-f]+)`)

// verificationToken pulls the confirmation token out of the message a
// registration sent to the given address.
func verificationToken(t *testing.T, email string) string {
	t.Helper()
	msg, ok := testMailer.LastFor(email)
	require.True(t, ok, "no email was sent to %s", email)
	m := verifyTokenPattern.FindStringSubmatch(msg.Text)
	require.Len(t, m, 2, "no confirmation link in the message body: %s", msg.Text)
	return m[1]
}

// confirmEmail completes the address confirmation a registration started.
func confirmEmail(t *testing.T, baseURL, email string) {
	t.Helper()
	helpers.DoJSON(t, http.MethodPost, baseURL+"/auth/verify-email/confirm", "",
		map[string]any{"token": verificationToken(t, email)}, nil)
}

func TestRegistrationSendsAConfirmationAndLeavesTheAddressUnverified(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := uniqueEmail()
	var reg struct {
		Token string `json:"token"`
		User  struct {
			EmailVerified bool `json:"emailVerified"`
		} `json:"user"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "New User", "email": email, "password": "password123"}, &reg)
	require.False(t, reg.User.EmailVerified, "registering does not prove the registrant reads the mailbox")

	confirmEmail(t, testServerURL, email)

	var me struct {
		EmailVerified bool `json:"emailVerified"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", reg.Token, nil, &me)
	require.True(t, me.EmailVerified)
}

func TestConfirmationTokenIsSingleUse(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := uniqueEmail()
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "Once User", "email": email, "password": "password123"}, nil)

	token := verificationToken(t, email)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/verify-email/confirm", "",
		map[string]any{"token": token}, nil)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/verify-email/confirm", "",
		map[string]any{"token": token})
	require.Equal(t, http.StatusBadRequest, status, "a spent confirmation link must not work twice")
}

func TestConfirmationRejectsAnUnknownToken(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/verify-email/confirm", "",
		map[string]any{"token": "0123456789abcdef0123456789abcdef"})
	require.Equal(t, http.StatusBadRequest, status)
}

func TestResendIssuesAFreshTokenAndRetiresTheOldOne(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := uniqueEmail()
	var reg struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "Resend User", "email": email, "password": "password123"}, &reg)

	first := verificationToken(t, email)
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/user/verify-email/resend", reg.Token, nil, nil)
	second := verificationToken(t, email)
	require.NotEqual(t, first, second)

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/verify-email/confirm", "",
		map[string]any{"token": first})
	require.Equal(t, http.StatusBadRequest, status, "asking for a new link must retire the previous one")

	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/verify-email/confirm", "",
		map[string]any{"token": second}, nil)
}
