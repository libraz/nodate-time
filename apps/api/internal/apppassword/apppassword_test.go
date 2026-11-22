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
