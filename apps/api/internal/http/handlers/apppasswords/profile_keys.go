package apppasswords

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/apppassword"
)

// ProfileLinkTTL is how long a profile link can be opened after it is issued.
const ProfileLinkTTL = 10 * time.Minute

// Labels domain-separate the two keys derived from the signing secret, so a
// ticket can never be presented as, or help forge, a session token.
const (
	profileLinkLabel   = "nodate-time/app-password-profile-link/v1"
	profileDeriveLabel = "nodate-time/app-password-derive/v1"
)

const (
	ticketVersion = 1
	// maxTicketLen bounds a ticket before anything is decoded; a 100-character
	// device name in any script stays well under it.
	maxTicketLen = 2048
	nonceLen     = 16
)

// errTicketInvalid is every reason a ticket is refused. They are not told
// apart, so a guessed ticket learns nothing about how close it came.
var errTicketInvalid = errors.New("profile link ticket is invalid")

// ProfileKeys mints and opens profile link tickets and derives the password a
// ticket stands for.
type ProfileKeys struct {
	ticket []byte
	derive []byte
}

// NewProfileKeys derives both keys from the signing secret. An empty secret
// is accepted; the secret is checked at startup.
func NewProfileKeys(secret []byte) *ProfileKeys {
	return &ProfileKeys{ticket: labelKey(secret, profileLinkLabel), derive: labelKey(secret, profileDeriveLabel)}
}

func labelKey(secret []byte, label string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

// ProfileClaims is what a ticket carries. The user is not among them: it is
// read from the session, which has to be live when the link is opened.
type ProfileClaims struct {
	// TicketID becomes the app password's public id.
	TicketID uuid.UUID
	// SessionID is the public id of the session that issued the link.
	SessionID uuid.UUID
	// Nonce exists only in the link, so the database and the secret together
	// still cannot yield the password.
	Nonce []byte
	// Name labels the app password.
	Name string
	// ExpiresAt is set by Open.
	ExpiresAt time.Time
}

// NewProfileClaims draws a fresh ticket id and nonce for a link issued by
// session.
func NewProfileClaims(session uuid.UUID, name string) (ProfileClaims, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return ProfileClaims{}, fmt.Errorf("ticket id: %w", err)
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return ProfileClaims{}, fmt.Errorf("ticket nonce: %w", err)
	}
	return ProfileClaims{TicketID: id, SessionID: session, Nonce: nonce, Name: name}, nil
}

type ticketPayload struct {
	V int    `json:"v"`
	I string `json:"i"`
	S string `json:"s"`
	R string `json:"r"`
	N string `json:"n"`
	E int64  `json:"e"`
}

// linkExpiry is when a link issued at issuedAt stops opening, to the second a
// ticket records.
func linkExpiry(issuedAt time.Time) time.Time {
	return time.Unix(issuedAt.Add(ProfileLinkTTL).Unix(), 0)
}

// Mint signs c into a ticket that expires ProfileLinkTTL after issuedAt.
func (k *ProfileKeys) Mint(c ProfileClaims, issuedAt time.Time) (string, error) {
	if len(c.Nonce) != nonceLen {
		return "", fmt.Errorf("ticket nonce must be %d bytes", nonceLen)
	}
	raw, err := json.Marshal(ticketPayload{
		V: ticketVersion,
		I: c.TicketID.String(),
		S: c.SessionID.String(),
		R: base64.RawURLEncoding.EncodeToString(c.Nonce),
		N: c.Name,
		E: linkExpiry(issuedAt).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encode ticket: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + base64.RawURLEncoding.EncodeToString(k.sign(payload)), nil
}

func (k *ProfileKeys) sign(payload string) []byte {
	mac := hmac.New(sha256.New, k.ticket)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// Open checks ticket and returns its claims. The signature is checked before
// anything it covers is decoded, and the expiry last.
func (k *ProfileKeys) Open(ticket string, now time.Time) (ProfileClaims, error) {
	if len(ticket) > maxTicketLen {
		return ProfileClaims{}, errTicketInvalid
	}
	parts := strings.Split(ticket, ".")
	if len(parts) != 2 {
		return ProfileClaims{}, errTicketInvalid
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, k.sign(parts[0])) {
		return ProfileClaims{}, errTicketInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ProfileClaims{}, errTicketInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p ticketPayload
	if err := dec.Decode(&p); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		return ProfileClaims{}, errTicketInvalid
	}
	if p.V != ticketVersion {
		return ProfileClaims{}, errTicketInvalid
	}
	expires := time.Unix(p.E, 0)
	if !now.Before(expires) {
		return ProfileClaims{}, errTicketInvalid
	}
	id, err := uuid.Parse(p.I)
	if err != nil {
		return ProfileClaims{}, errTicketInvalid
	}
	session, err := uuid.Parse(p.S)
	if err != nil {
		return ProfileClaims{}, errTicketInvalid
	}
	nonce, err := base64.RawURLEncoding.DecodeString(p.R)
	if err != nil || len(nonce) != nonceLen {
		return ProfileClaims{}, errTicketInvalid
	}
	return ProfileClaims{TicketID: id, SessionID: session, Nonce: nonce, Name: p.N, ExpiresAt: expires}, nil
}

// password derives the app password c stands for.
func (k *ProfileKeys) password(c ProfileClaims) (plain, prefix, hash string) {
	return apppassword.Derive(k.derive, c.Nonce, c.TicketID[:])
}
