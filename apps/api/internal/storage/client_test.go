package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDownloadResponseParamsForceAttachmentHeaders(t *testing.T) {
	params := downloadResponseParams()
	require.Equal(t, "attachment", params.Get("response-content-disposition"))
	require.Equal(t, "application/octet-stream", params.Get("response-content-type"))
}
