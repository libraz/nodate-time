package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type patchedMemo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Done      bool   `json:"done"`
	SortOrder int32  `json:"sortOrder"`
	ETag      string `json:"etag"`
}

func errorCode(t *testing.T, raw []byte) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(raw, &e), string(raw))
	return e.Code
}

func createPatchableMemo(t *testing.T, tt *helpers.TestTenant, calURL string) patchedMemo {
	t.Helper()
	status, raw, hdr := helpers.DoJSONFull(t, http.MethodPost, calURL+"/memos", tt.AccessToken,
		map[string]any{"title": "Order supplies", "body": "paper, toner", "sortOrder": 3}, nil)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var m patchedMemo
	require.NoError(t, json.Unmarshal(raw, &m))
	require.NotEmpty(t, m.ETag, "a created memo carries its etag")
	require.Equal(t, m.ETag, hdr.Get("ETag"))
	return m
}

func TestMemoPatchKeepsWhatItDoesNotName(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	memo := createPatchableMemo(t, tt, calURL)

	status, raw, hdr := helpers.DoJSONFull(t, http.MethodPatch, calURL+"/memos/"+memo.ID, tt.AccessToken,
		map[string]any{"done": true}, map[string]string{"If-Match": memo.ETag})
	require.Equal(t, http.StatusOK, status, string(raw))
	var done patchedMemo
	require.NoError(t, json.Unmarshal(raw, &done))
	require.True(t, done.Done)
	require.Equal(t, memo.Title, done.Title)
	require.Equal(t, "paper, toner", done.Body)
	require.Equal(t, int32(3), done.SortOrder)
	require.NotEqual(t, memo.ETag, done.ETag, "a change moves the etag")
	require.Equal(t, done.ETag, hdr.Get("ETag"))

	// null clears the body, the one member with an empty state.
	status, raw, _ = helpers.DoJSONFull(t, http.MethodPatch, calURL+"/memos/"+memo.ID, tt.AccessToken,
		map[string]any{"body": nil}, map[string]string{"If-Match": done.ETag})
	require.Equal(t, http.StatusOK, status, string(raw))
	var cleared patchedMemo
	require.NoError(t, json.Unmarshal(raw, &cleared))
	require.Empty(t, cleared.Body)
	require.Equal(t, memo.Title, cleared.Title)

	// null on a member that has no empty state is a validation failure, and
	// leaves the memo as it was.
	status, raw, _ = helpers.DoJSONFull(t, http.MethodPatch, calURL+"/memos/"+memo.ID, tt.AccessToken,
		map[string]any{"title": nil}, map[string]string{"If-Match": cleared.ETag})
	require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
	require.Equal(t, "REQUEST.VALIDATION", errorCode(t, raw))

	// The verb is PATCH now.
	status, _ = helpers.DoJSONStatus(t, http.MethodPut, calURL+"/memos/"+memo.ID, tt.AccessToken,
		map[string]any{"done": false})
	require.Equal(t, http.StatusMethodNotAllowed, status)
}

func TestMemoWriteRefusesAMissingOrStaleVersion(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	memo := createPatchableMemo(t, tt, calURL)
	url := calURL + "/memos/" + memo.ID

	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			body := map[string]any{"title": "x"}
			if method == http.MethodDelete {
				body = nil
			}

			status, raw := helpers.DoJSONStatus(t, method, url, tt.AccessToken, body)
			require.Equal(t, http.StatusPreconditionRequired, status, string(raw))
			require.Equal(t, "REQUEST.PRECONDITION_REQUIRED", errorCode(t, raw))

			status, raw, _ = helpers.DoJSONFull(t, method, url, tt.AccessToken, body,
				map[string]string{"If-Match": "not-a-tag"})
			require.Equal(t, http.StatusBadRequest, status, string(raw))
			require.Equal(t, "REQUEST.BAD_IF_MATCH", errorCode(t, raw))

			status, raw, _ = helpers.DoJSONFull(t, method, url, tt.AccessToken, body,
				map[string]string{"If-Match": `"20000101T000000.000"`})
			require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
			require.Equal(t, "MEMO.STALE", errorCode(t, raw))

			status, raw, _ = helpers.DoJSONFull(t, method, url, tt.AccessToken, body,
				map[string]string{"If-Match": "W/" + memo.ETag})
			require.Equal(t, http.StatusPreconditionFailed, status, "a weak tag never matches: %s", string(raw))
		})
	}

	// None of the refusals changed the memo.
	var list struct {
		Items []patchedMemo `json:"items"`
	}
	helpers.DoJSON(t, http.MethodGet, calURL+"/memos", tt.AccessToken, nil, &list)
	require.Len(t, list.Items, 1)
	require.Equal(t, memo.ETag, list.Items[0].ETag, "a list item carries the etag a write needs")
	require.Equal(t, memo.Title, list.Items[0].Title)
}

func TestMemoSecondWriterWithTheOldVersionLoses(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	memo := createPatchableMemo(t, tt, calURL)
	url := calURL + "/memos/" + memo.ID

	status, raw, _ := helpers.DoJSONFull(t, http.MethodPatch, url, tt.AccessToken,
		map[string]any{"title": "first"}, map[string]string{"If-Match": memo.ETag})
	require.Equal(t, http.StatusOK, status, string(raw))

	status, raw, _ = helpers.DoJSONFull(t, http.MethodPatch, url, tt.AccessToken,
		map[string]any{"title": "second"}, map[string]string{"If-Match": memo.ETag})
	require.Equal(t, http.StatusPreconditionFailed, status, string(raw))
	require.Equal(t, "MEMO.STALE", errorCode(t, raw))

	// "*" matches whatever the memo is now.
	status, raw, _ = helpers.DoJSONFull(t, http.MethodPatch, url, tt.AccessToken,
		map[string]any{"title": "third"}, map[string]string{"If-Match": "*"})
	require.Equal(t, http.StatusOK, status, string(raw))

	var current patchedMemo
	require.NoError(t, json.Unmarshal(raw, &current))
	status, raw, _ = helpers.DoJSONFull(t, http.MethodDelete, url, tt.AccessToken, nil,
		map[string]string{"If-Match": current.ETag})
	require.Equal(t, http.StatusNoContent, status, string(raw))
}

func TestMemoListLimitOutOfRangeIsAValidationFailure(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	calURL := testServerURL + "/calendars/" + tt.CalendarID
	for _, limit := range []string{"0", "201"} {
		status, raw := helpers.DoJSONStatus(t, http.MethodGet, calURL+"/memos?limit="+limit, tt.AccessToken, nil)
		require.Equal(t, http.StatusUnprocessableEntity, status, string(raw))
		require.Equal(t, "REQUEST.VALIDATION", errorCode(t, raw))
	}
}
