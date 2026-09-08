package storage

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testSecret = "a-sufficiently-long-signing-secret-for-this-test"

func testSigner() *Signer { return NewSigner("https://app.example.com/api", []byte(testSecret)) }

// parse splits a minted URL into the key the relay would route on and the
// query it would verify, which is the pair Verify is given in production.
func parse(t *testing.T, raw string) (string, url.Values) {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	key := strings.TrimPrefix(u.Path, "/api"+BlobPath+"/")
	return key, u.Query()
}

func TestGetURLRoundTrips(t *testing.T) {
	s := testSigner()
	raw, err := s.GetURL("avatars/abc/def", time.Hour)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(raw, "https://app.example.com/api/blobs/avatars/abc/def?"), raw)

	key, q := parse(t, raw)
	grant, err := s.Verify("GET", key, q, time.Now())
	require.NoError(t, err)
	require.False(t, grant.Download)
	require.Empty(t, grant.ContentType)
	require.Zero(t, grant.ByteSize)
	require.WithinDuration(t, time.Now().Add(time.Hour), grant.ExpiresAt, 2*time.Second)
}

func TestPutURLCarriesTheSizeAndTypeItBinds(t *testing.T) {
	s := testSigner()
	raw, err := s.PutURL("workspace/ws/digest", "image/png", 1234, 15*time.Minute)
	require.NoError(t, err)

	key, q := parse(t, raw)
	grant, err := s.Verify("PUT", key, q, time.Now())
	require.NoError(t, err)
	require.Equal(t, "image/png", grant.ContentType)
	require.Equal(t, int64(1234), grant.ByteSize)
}

func TestDownloadURLKeepsTheFilename(t *testing.T) {
	s := testSigner()
	raw, err := s.DownloadURL("workspace/ws/digest", "契約書.pdf", 5*time.Minute)
	require.NoError(t, err)

	key, q := parse(t, raw)
	grant, err := s.Verify("GET", key, q, time.Now())
	require.NoError(t, err)
	require.True(t, grant.Download)
	require.Equal(t, "契約書.pdf", grant.Filename)
}

// An unbound write URL accepts a body of any length, so the signer refuses to
// mint one rather than leaving the bucket open to it.
func TestPutURLRefusesAnUnboundSize(t *testing.T) {
	s := testSigner()
	for _, size := range []int64{0, -1} {
		_, err := s.PutURL("k", "image/png", size, time.Minute)
		require.Error(t, err, "size %d must not be signable", size)
	}
}

func TestMintingNeedsAPublicBase(t *testing.T) {
	s := NewSigner("", []byte(testSecret))
	_, err := s.GetURL("k", time.Minute)
	require.ErrorIs(t, err, ErrSignerUnconfigured)
}

// Every field the URL states about the transfer is under the MAC, so none of
// them can be edited in flight. Each case changes exactly one and expects the
// request to stop at Verify.
func TestVerifyRejectsEveryTamperedField(t *testing.T) {
	s := testSigner()
	raw, err := s.PutURL("workspace/ws/digest", "image/png", 1000, 15*time.Minute)
	require.NoError(t, err)
	key, original := parse(t, raw)

	// The URL as minted is accepted, so a rejection below is the edit and not
	// a broken fixture.
	_, err = s.Verify("PUT", key, original, time.Now())
	require.NoError(t, err)

	cases := []struct {
		name   string
		method string
		key    string
		edit   func(url.Values)
	}{
		{name: "size raised to store more than was reserved", edit: func(q url.Values) { q.Set("sz", "999999999") }},
		{name: "size lowered", edit: func(q url.Values) { q.Set("sz", "1") }},
		{name: "size removed", edit: func(q url.Values) { q.Del("sz") }},
		{name: "type swapped for one the browser executes", edit: func(q url.Values) { q.Set("ct", "text/html") }},
		{name: "type removed", edit: func(q url.Values) { q.Del("ct") }},
		{name: "expiry extended", edit: func(q url.Values) { q.Set("exp", "99999999999") }},
		{name: "expiry removed", edit: func(q url.Values) { q.Del("exp") }},
		{name: "download flag added", edit: func(q url.Values) { q.Set("dl", "1") }},
		{name: "filename added", edit: func(q url.Values) { q.Set("fn", "invoice.pdf") }},
		{name: "signature removed", edit: func(q url.Values) { q.Del("sig") }},
		{name: "signature altered", edit: func(q url.Values) { q.Set("sig", strings.Repeat("0", 64)) }},
		{name: "key pointed at another object", key: "workspace/ws/someone-elses", edit: func(url.Values) {}},
		{name: "write signature replayed as a read", method: "GET", edit: func(url.Values) {}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := url.Values{}
			for k, v := range original {
				q[k] = append([]string(nil), v...)
			}
			tc.edit(q)

			method := tc.method
			if method == "" {
				method = "PUT"
			}
			target := tc.key
			if target == "" {
				target = key
			}
			_, err := s.Verify(method, target, q, time.Now())
			require.ErrorIs(t, err, ErrURLInvalid)
		})
	}
}

func TestVerifyRejectsASignatureFromAnotherSecret(t *testing.T) {
	raw, err := NewSigner("https://app.example.com/api", []byte("a-different-secret-of-adequate-length")).
		GetURL("avatars/abc/def", time.Hour)
	require.NoError(t, err)

	key, q := parse(t, raw)
	_, err = testSigner().Verify("GET", key, q, time.Now())
	require.ErrorIs(t, err, ErrURLInvalid)
}

func TestVerifyRejectsAnExpiredURLAtTheBoundary(t *testing.T) {
	s := testSigner()
	raw, err := s.GetURL("avatars/abc/def", time.Minute)
	require.NoError(t, err)
	key, q := parse(t, raw)

	exp := time.Unix(mustInt(t, q.Get("exp")), 0)
	_, err = s.Verify("GET", key, q, exp.Add(-time.Second))
	require.NoError(t, err, "a second before the expiry is still valid")

	// The expiry second itself is already over: a URL good "until" a time is
	// not good at it.
	_, err = s.Verify("GET", key, q, exp)
	require.ErrorIs(t, err, ErrURLExpired)
	_, err = s.Verify("GET", key, q, exp.Add(time.Hour))
	require.ErrorIs(t, err, ErrURLExpired)
}

// A URL whose signature does not check out is refused before its expiry is
// looked at, so the two answers cannot be told apart by anyone who did not
// have a valid signature to begin with.
func TestExpiryIsNotReportedForAnUnsignedURL(t *testing.T) {
	s := testSigner()
	raw, err := s.GetURL("avatars/abc/def", -time.Hour)
	require.NoError(t, err)
	key, q := parse(t, raw)
	q.Set("sig", strings.Repeat("0", 64))

	_, err = s.Verify("GET", key, q, time.Now())
	require.ErrorIs(t, err, ErrURLInvalid, "an unsigned expired URL must read as invalid, not as expired")
}

// The signed string is length-prefixed so that no rearrangement of the values
// produces the bytes of another. A plain delimiter would let a filename ending
// in it borrow the next field and authorize a transfer nobody signed.
func TestCanonicalEncodingCannotBeShiftedAcrossFields(t *testing.T) {
	collidable := [][]string{
		{"a", "bc"},
		{"ab", "c"},
		{"a:b", "c"},
		{"a", ":bc"},
		{"", "abc"},
		{"abc", ""},
		{"1:a", "b"},
		{"1", ":ab"},
	}
	seen := map[string][]string{}
	for _, fields := range collidable {
		encoded := string(canonical(fields...))
		if prior, clash := seen[encoded]; clash {
			t.Fatalf("%q and %q encode identically as %q", prior, fields, encoded)
		}
		seen[encoded] = fields
	}
}

// A key is escaped per path segment, so it comes back off the wire as the
// string it was signed as rather than as a different one that happens to route
// to the same place.
func TestKeysSurviveTheURLRoundTrip(t *testing.T) {
	s := testSigner()
	for _, key := range []string{
		"avatars/0191d1f0/da39a3ee",
		"albums/0191d1f0/0191d200/thumb",
		"workspace/0191d1f0/e3b0c442",
	} {
		raw, err := s.GetURL(key, time.Hour)
		require.NoError(t, err)
		got, q := parse(t, raw)
		require.Equal(t, key, got)
		_, err = s.Verify("GET", got, q, time.Now())
		require.NoError(t, err)
	}
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	require.NoError(t, err)
	return n
}
