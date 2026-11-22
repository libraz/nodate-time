package apppassword

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerate(t *testing.T) {
	plain, prefix, hash, err := Generate()
	require.NoError(t, err)
	require.Len(t, plain, len(Prefix)+32)
	require.True(t, strings.HasPrefix(plain, Prefix))
	for _, c := range plain[len(Prefix):] {
		require.Contains(t, crockford, string(c))
	}
	require.Equal(t, plain[:PrefixLen], prefix)
	require.Equal(t, Hash(plain), hash)
	require.Len(t, hash, 64)

	other, _, _, err := Generate()
	require.NoError(t, err)
	require.NotEqual(t, plain, other)
}

func TestNormalizeRoundTrip(t *testing.T) {
	plain, _, hash, err := Generate()
	require.NoError(t, err)

	body := plain[len(Prefix):]
	variants := []string{
		plain,
		Format(plain),
		" " + Format(plain) + "\n",
		strings.ToLower(plain),
		"NDT_" + body,
		"ndt_" + strings.ToLower(body),
		strings.ReplaceAll(Format(plain), "-", " "),
	}
	for _, v := range variants {
		require.Equal(t, plain, Normalize(v), v)
		require.Equal(t, hash, Hash(v), v)
	}
}

func TestNormalize(t *testing.T) {
	require.Equal(t, "ndt_ABCD", Normalize(" NDT_ab-cd "))
	require.Equal(t, "ABCD", Normalize("ab cd"))
	require.Equal(t, "", Normalize("  - "))
}

func TestFormat(t *testing.T) {
	require.Equal(t, "ndt_ABCD-EFGH-JK", Format("ndt_ABCDEFGHJK"))
	require.Equal(t, "ndt_", Format("ndt_"))
}

func TestDerive(t *testing.T) {
	key := []byte("derive-key")
	nonce := []byte("0123456789abcdef")
	id := []byte("fedcba9876543210")

	plain, prefix, hash := Derive(key, nonce, id)
	require.Len(t, plain, len(Prefix)+32)
	require.True(t, strings.HasPrefix(plain, Prefix))
	for _, c := range plain[len(Prefix):] {
		require.Contains(t, crockford, string(c))
	}
	require.Equal(t, plain, Normalize(plain))
	require.Equal(t, plain[:PrefixLen], prefix)
	require.Equal(t, Hash(plain), hash)

	again, againPrefix, againHash := Derive(key, nonce, id)
	require.Equal(t, plain, again, "the same input derives the same password")
	require.Equal(t, prefix, againPrefix)
	require.Equal(t, hash, againHash)

	otherNonce, _, _ := Derive(key, []byte("0123456789abcdeX"), id)
	require.NotEqual(t, plain, otherNonce)
	otherID, _, _ := Derive(key, nonce, []byte("fedcba987654321X"))
	require.NotEqual(t, plain, otherID)
	otherKey, _, _ := Derive([]byte("other-key"), nonce, id)
	require.NotEqual(t, plain, otherKey)
}

func TestEncodeMatchesKnownBytes(t *testing.T) {
	var zero [rawLen]byte
	require.Equal(t, Prefix+strings.Repeat("0", 32), encode(zero))
	var ones [rawLen]byte
	for i := range ones {
		ones[i] = 0xff
	}
	require.Equal(t, Prefix+strings.Repeat("Z", 32), encode(ones))
}
