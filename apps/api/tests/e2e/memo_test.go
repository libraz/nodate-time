package e2e

import (
	"net/http"
	"strings"
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
	var memos struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/memos", tt.AccessToken, nil, &memos)
	require.Len(t, memos.Items, 2)
	require.Equal(t, "シフト表作成", memos.Items[0].Title)
	require.Equal(t, "経費精算", memos.Items[1].Title)
	require.Empty(t, memos.NextCursor, "two memos fit in one page")
}

func TestMemoBodyLengthLimit(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, calURL+"/memos", tt.AccessToken,
		map[string]any{"title": "too long", "body": strings.Repeat("a", 16001), "sortOrder": 0})
	require.Equal(t, http.StatusUnprocessableEntity, status)
}

func TestMemoUpdateDelete(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID

	// Create memo
	var memo struct {
		ID   string `json:"id"`
		Done bool   `json:"done"`
	}
	helpers.DoJSON(t, http.MethodPost, calURL+"/memos", tt.AccessToken,
		map[string]any{"title": "備品発注", "sortOrder": 0}, &memo)
	require.NotEmpty(t, memo.ID)
	require.False(t, memo.Done)

	// Update memo: rename and mark done
	var updated struct {
		Title string `json:"title"`
		Done  bool   `json:"done"`
	}
	helpers.DoJSON(t, http.MethodPut, calURL+"/memos/"+memo.ID, tt.AccessToken,
		map[string]any{"title": "備品発注（完了）", "done": true, "sortOrder": 0}, &updated)
	require.Equal(t, "備品発注（完了）", updated.Title)
	require.True(t, updated.Done)

	// The update is reflected in the list
	var memos struct {
		Items []struct {
			Title string `json:"title"`
			Done  bool   `json:"done"`
		} `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/memos", tt.AccessToken, nil, &memos)
	require.Len(t, memos.Items, 1)
	require.Equal(t, "備品発注（完了）", memos.Items[0].Title)
	require.True(t, memos.Items[0].Done)

	// Delete memo
	status, _ := helpers.DoJSONStatus(t, http.MethodDelete, calURL+"/memos/"+memo.ID, tt.AccessToken, nil)
	require.True(t, status >= 200 && status < 300)

	// List is now empty
	var after struct {
		Items []struct{ Title string } `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/memos", tt.AccessToken, nil, &after)
	require.Len(t, after.Items, 0)
}
