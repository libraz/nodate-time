package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func TestMemoLifecycle(t *testing.T) {
	bootstrap(t)
	t.Parallel()


	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create memo
	var memo struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Done  bool   `json:"done"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/memos", tt.AccessToken,
		map[string]any{"title": "シフト表作成", "sortOrder": 0}, &memo)
	require.NotEmpty(t, memo.ID)
	require.Equal(t, "シフト表作成", memo.Title)
	require.False(t, memo.Done)

	// Create second memo
	helpers.DoJSON(t, http.MethodPost, calURL+"/memos", tt.AccessToken,
		map[string]any{"title": "経費精算", "sortOrder": 1}, nil)

	// List memos
	var memos []struct {
		Title string `json:"title"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/memos", tt.AccessToken, nil, &memos)
	require.Len(t, memos, 2)
	require.Equal(t, "シフト表作成", memos[0].Title)
	require.Equal(t, "経費精算", memos[1].Title)
}
