package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDownloadResponseParamsForceAttachmentHeaders(t *testing.T) {
	params := downloadResponseParams("")
	require.Equal(t, "attachment", params.Get("response-content-disposition"))
	require.Equal(t, "application/octet-stream", params.Get("response-content-type"))
}

func TestDownloadResponseParamsCarriesFilename(t *testing.T) {
	params := downloadResponseParams("契約書.pdf")
	disposition := params.Get("response-content-disposition")
	require.Contains(t, disposition, `filename="`)
	require.Contains(t, disposition, "filename*=UTF-8''%E5%A5%91%E7%B4%84%E6%9B%B8.pdf")
}

func TestContentDispositionAsciiFallbackIsSafeInsideQuotes(t *testing.T) {
	disposition := contentDisposition(`weird"name\here.txt`)
	require.Contains(t, disposition, `filename="weird_name_here.txt"`)
}
