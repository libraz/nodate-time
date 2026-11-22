package apppasswords

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/apppassword"
	"github.com/stretchr/testify/require"
)

var issued = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func testClaims(t *testing.T, name string) ProfileClaims {
	t.Helper()
	c, err := NewProfileClaims(uuid.MustParse("019a0000-0000-7000-8000-0000000000ee"), name)
	require.NoError(t, err)
	return c
}

func mint(t *testing.T, k *ProfileKeys, c ProfileClaims) string {
	t.Helper()
	ticket, err := k.Mint(c, issued)
	require.NoError(t, err)
	return ticket
}

// signed builds a ticket around an arbitrary payload, signed as k signs.
func signed(k *ProfileKeys, payloadJSON string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	return payload + "." + base64.RawURLEncoding.EncodeToString(k.sign(payload))
}

func TestProfileTicketRoundTrip(t *testing.T) {
	k := NewProfileKeys([]byte("secret"))
	c := testClaims(t, "田中の iPhone")
	got, err := k.Open(mint(t, k, c), issued.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, c.TicketID, got.TicketID)
	require.Equal(t, c.SessionID, got.SessionID)
	require.Equal(t, c.Nonce, got.Nonce)
	require.Equal(t, c.Name, got.Name)
	require.True(t, got.ExpiresAt.Equal(issued.Add(ProfileLinkTTL)))

	plain, prefix, hash := k.password(got)
	wantPlain, wantPrefix, wantHash := k.password(c)
	require.Equal(t, wantPlain, plain)
	require.Equal(t, wantPrefix, prefix)
	require.Equal(t, wantHash, hash)
	require.Equal(t, apppassword.Hash(plain), hash)
}

func TestProfileTicketMaximumName(t *testing.T) {
	k := NewProfileKeys([]byte("secret"))
	ticket := mint(t, k, testClaims(t, strings.Repeat("𠮷", 100)))
	require.Less(t, len(ticket), maxTicketLen)
	_, err := k.Open(ticket, issued)
	require.NoError(t, err)
}

func TestProfileKeysFromAnEmptySecret(t *testing.T) {
	k := NewProfileKeys(nil)
	_, err := k.Open(mint(t, k, testClaims(t, "Mac")), issued)
	require.NoError(t, err)
}

func TestProfileTicketRefusals(t *testing.T) {
	k := NewProfileKeys([]byte("secret"))
	c := testClaims(t, "iPhone")
	good := mint(t, k, c)
	payload, sig, _ := strings.Cut(good, ".")
	other := mint(t, k, testClaims(t, "Mac"))
	_, otherSig, _ := strings.Cut(other, ".")
	flip := func(s string, i int) string {
		b := []byte(s)
		if b[i] == 'A' {
			b[i] = 'B'
		} else {
			b[i] = 'A'
		}
		return string(b)
	}
	e := issued.Add(ProfileLinkTTL).Unix()
	valid := `"i":"` + c.TicketID.String() + `","s":"` + c.SessionID.String() +
		`","r":"` + base64.RawURLEncoding.EncodeToString(c.Nonce) + `","n":"iPhone","e":`

	for name, ticket := range map[string]string{
		"empty":                      "",
		"no dot":                     payload + sig,
		"three parts":                good + ".x",
		"bad base64 signature":       payload + ".!!!",
		"bad base64 payload":         "!!!." + base64.RawURLEncoding.EncodeToString(k.sign("!!!")),
		"signature one char off":     payload + "." + flip(sig, 3),
		"payload one char off":       flip(payload, 5) + "." + sig,
		"another ticket's signature": payload + "." + otherSig,
		"another key":                mint(t, NewProfileKeys([]byte("other")), c),
		"over the length bound":      strings.Repeat("a", maxTicketLen+1),
		"not json":                   signed(k, "not json"),
		"unknown key":                signed(k, `{"v":1,`+valid+itoa(e)+`,"x":1}`),
		"trailing data":              signed(k, `{"v":1,`+valid+itoa(e)+`}{}`),
		"version 2":                  signed(k, `{"v":2,`+valid+itoa(e)+`}`),
		"bad ticket id":              signed(k, `{"v":1,"i":"x","s":"`+c.SessionID.String()+`","r":"AAAAAAAAAAAAAAAAAAAAAA","n":"a","e":`+itoa(e)+`}`),
		"short nonce":                signed(k, `{"v":1,"i":"`+c.TicketID.String()+`","s":"`+c.SessionID.String()+`","r":"AAAA","n":"a","e":`+itoa(e)+`}`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := k.Open(ticket, issued)
			require.ErrorIs(t, err, errTicketInvalid)
		})
	}

	t.Run("expiry", func(t *testing.T) {
		_, err := k.Open(good, issued.Add(ProfileLinkTTL-time.Second))
		require.NoError(t, err)
		_, err = k.Open(good, issued.Add(ProfileLinkTTL))
		require.ErrorIs(t, err, errTicketInvalid, "exactly at the expiry")
		_, err = k.Open(good, issued.Add(time.Hour))
		require.ErrorIs(t, err, errTicketInvalid)
	})

	t.Run("signature covers the payload as received", func(t *testing.T) {
		mac := hmac.New(sha256.New, k.ticket)
		mac.Write([]byte(payload))
		require.Equal(t, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), sig)
	})
}

func TestProfileTicketMintRefusesABadNonce(t *testing.T) {
	k := NewProfileKeys([]byte("secret"))
	c := testClaims(t, "iPhone")
	c.Nonce = c.Nonce[:4]
	_, err := k.Mint(c, issued)
	require.Error(t, err)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
