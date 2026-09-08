package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentDispositionWithoutFilenameIsBareAttachment(t *testing.T) {
	require.Equal(t, "attachment", ContentDisposition(""))
}

func TestContentDispositionCarriesNonASCIIFilename(t *testing.T) {
	disposition := ContentDisposition("契約書.pdf")
	require.Contains(t, disposition, `filename="`)
	require.Contains(t, disposition, "filename*=UTF-8''%E5%A5%91%E7%B4%84%E6%9B%B8.pdf")
}

func TestContentDispositionAsciiFallbackIsSafeInsideQuotes(t *testing.T) {
	disposition := ContentDisposition(`weird"name\here.txt`)
	require.Contains(t, disposition, `filename="weird_name_here.txt"`)
}

// The relay serves the object's own media type only for the types a browser
// renders as a picture and nothing else. It answers on the API's origin, so a
// type the browser executes would execute next to the session.
func TestInlineAllowedRejectsExecutableTypes(t *testing.T) {
	for _, ct := range []string{"image/jpeg", "image/png", "image/webp", "image/gif", "IMAGE/PNG", "image/png; charset=utf-8"} {
		require.True(t, InlineAllowed(ct), "%s should be servable inline", ct)
	}
	for _, ct := range []string{"image/svg+xml", "text/html", "application/xhtml+xml", "application/pdf", "text/plain", "", "application/octet-stream"} {
		require.False(t, InlineAllowed(ct), "%s must not be servable inline", ct)
	}
}

func TestSameMediaTypeIgnoresParametersAndCase(t *testing.T) {
	require.True(t, SameMediaType("image/png", "IMAGE/PNG; charset=utf-8"))
	require.True(t, SameMediaType(" image/jpeg ", "image/jpeg"))
	require.False(t, SameMediaType("image/png", "image/jpeg"))
	require.False(t, SameMediaType("", "image/png"))
}
