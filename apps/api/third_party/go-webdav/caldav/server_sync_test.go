package caldav

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/internal"
)

const syncTestCalendarPath = "/user/calendars/a/"

func newSyncTestObject(path, uid string) CalendarObject {
	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, uid)
	event.Props.SetDateTime(ical.PropDateTimeStamp, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	event.Props.SetDateTime(ical.PropDateTimeStart, time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC))
	event.Props.SetText(ical.PropSummary, "Event "+uid)
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//go-webdav//caldav test//EN")
	cal.Children = []*ical.Component{event.Component}
	return CalendarObject{
		Path: path,
		ETag: uid + "-etag",
		Data: cal,
	}
}

// syncTestBackend implements SyncBackend on top of testBackend. Token "1"
// describes a past state from which b.ics was updated and c.ics deleted; token
// "2" is the current state.
type syncTestBackend struct {
	testBackend
	lastQuery *SyncQuery
}

func newSyncTestBackend() *syncTestBackend {
	return &syncTestBackend{testBackend: testBackend{
		calendars: []Calendar{{Path: syncTestCalendarPath, SyncToken: "2"}},
		objectMap: map[string][]CalendarObject{
			syncTestCalendarPath: {
				newSyncTestObject(syncTestCalendarPath+"a.ics", "a"),
				newSyncTestObject(syncTestCalendarPath+"b.ics", "b"),
			},
		},
	}}
}

func (b *syncTestBackend) SyncCollection(ctx context.Context, path string, query *SyncQuery) (*SyncResponse, error) {
	b.lastQuery = query
	if path != syncTestCalendarPath {
		return nil, internal.HTTPErrorf(http.StatusNotFound, "no calendar at %q", path)
	}

	objs := b.objectMap[path]
	var res SyncResponse
	switch query.SyncToken {
	case "":
		res.Updated = objs
	case "1":
		res.Updated = objs[1:]
		res.Deleted = []string{path + "c.ics"}
	case "2":
	default:
		return nil, fmt.Errorf("unknown token %q: %w", query.SyncToken, ErrInvalidSyncToken)
	}
	if query.Limit > 0 && len(res.Updated)+len(res.Deleted) > query.Limit {
		return nil, ErrSyncLimitExceeded
	}
	res.SyncToken = "2"
	return &res, nil
}

const propFindSyncTokenRequest = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:sync-token/>
    <d:supported-report-set/>
  </d:prop>
</d:propfind>`

const syncCollectionRequest = `<?xml version="1.0" encoding="UTF-8"?>
<d:sync-collection xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:sync-token>%s</d:sync-token>
  <d:sync-level>%s</d:sync-level>%s
  <d:prop>
    <d:getetag/>
  </d:prop>
</d:sync-collection>`

func serveSyncTest(t *testing.T, backend Backend, method, depth, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, syncTestCalendarPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/xml")
	if depth != "" {
		req.Header.Set("Depth", depth)
	}
	w := httptest.NewRecorder()
	handler := Handler{Backend: backend}
	handler.ServeHTTP(w, req)
	return w.Result()
}

func syncCollectionBody(token, level, limit string) string {
	return fmt.Sprintf(syncCollectionRequest, token, level, limit)
}

func decodeSyncTestMultiStatus(t *testing.T, res *http.Response) *internal.MultiStatus {
	t.Helper()
	defer res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus {
		data, _ := io.ReadAll(res.Body)
		t.Fatalf("status = %v, want %v, body:\n%s", res.StatusCode, http.StatusMultiStatus, data)
	}
	var ms internal.MultiStatus
	if err := xml.NewDecoder(res.Body).Decode(&ms); err != nil {
		t.Fatalf("failed to decode multistatus: %v", err)
	}
	return &ms
}

func TestPropFindCalendarSyncToken(t *testing.T) {
	res := serveSyncTest(t, newSyncTestBackend(), "PROPFIND", "0", propFindSyncTokenRequest)
	ms := decodeSyncTestMultiStatus(t, res)
	if len(ms.Responses) != 1 {
		t.Fatalf("got %d responses, want 1", len(ms.Responses))
	}
	resp := &ms.Responses[0]

	var syncToken internal.SyncToken
	if err := resp.DecodeProp(&syncToken); err != nil {
		t.Fatalf("DecodeProp(sync-token): %v", err)
	}
	if syncToken.Token != "2" {
		t.Errorf("sync-token = %q, want %q", syncToken.Token, "2")
	}

	var reportSet internal.SupportedReportSet
	if err := resp.DecodeProp(&reportSet); err != nil {
		t.Fatalf("DecodeProp(supported-report-set): %v", err)
	}
	var reports []xml.Name
	for _, sr := range reportSet.SupportedReport {
		for _, raw := range sr.Report.Raw {
			if name, ok := raw.XMLName(); ok {
				reports = append(reports, name)
			}
		}
	}
	want := []xml.Name{calendarQueryName, calendarMultigetName, internal.SyncCollectionName}
	if !reflect.DeepEqual(reports, want) {
		t.Errorf("supported reports = %v, want %v", reports, want)
	}
}

func TestPropFindCalendarWithoutSyncBackend(t *testing.T) {
	backend := testBackend{calendars: []Calendar{{Path: syncTestCalendarPath}}}
	res := serveSyncTest(t, backend, "PROPFIND", "0", propFindSyncTokenRequest)
	ms := decodeSyncTestMultiStatus(t, res)
	if len(ms.Responses) != 1 {
		t.Fatalf("got %d responses, want 1", len(ms.Responses))
	}
	resp := &ms.Responses[0]

	var syncToken internal.SyncToken
	if err := resp.DecodeProp(&syncToken); !internal.IsNotFound(err) {
		t.Errorf("DecodeProp(sync-token) = %v, want not found", err)
	}
	var reportSet internal.SupportedReportSet
	if err := resp.DecodeProp(&reportSet); !internal.IsNotFound(err) {
		t.Errorf("DecodeProp(supported-report-set) = %v, want not found", err)
	}
}

func TestSyncCollectionInitial(t *testing.T) {
	res := serveSyncTest(t, newSyncTestBackend(), "REPORT", "0", syncCollectionBody("", "1", ""))
	ms := decodeSyncTestMultiStatus(t, res)
	if ms.SyncToken != "2" {
		t.Errorf("sync-token = %q, want %q", ms.SyncToken, "2")
	}

	got := make(map[string]string)
	for _, resp := range ms.Responses {
		p, err := resp.Path()
		if err != nil {
			t.Fatalf("response for %v: %v", resp.Hrefs, err)
		}
		var getETag internal.GetETag
		if err := resp.DecodeProp(&getETag); err != nil {
			t.Fatalf("DecodeProp(getetag) for %q: %v", p, err)
		}
		got[p] = string(getETag.ETag)
	}
	want := map[string]string{
		syncTestCalendarPath + "a.ics": "a-etag",
		syncTestCalendarPath + "b.ics": "b-etag",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("responses = %v, want %v", got, want)
	}
}

// TestSyncCollectionIgnoresDepth covers clients that send a Depth other than
// the "0" RFC 6578 asks for: Apple's send "1".
func TestSyncCollectionIgnoresDepth(t *testing.T) {
	for _, depth := range []string{"", "0", "1", "infinity"} {
		t.Run("Depth "+depth, func(t *testing.T) {
			res := serveSyncTest(t, newSyncTestBackend(), "REPORT", depth, syncCollectionBody("", "1", ""))
			ms := decodeSyncTestMultiStatus(t, res)
			if len(ms.Responses) != 2 {
				t.Errorf("responses = %d, want 2", len(ms.Responses))
			}
		})
	}
}

func TestSyncCollectionIncremental(t *testing.T) {
	res := serveSyncTest(t, newSyncTestBackend(), "REPORT", "", syncCollectionBody("1", "1", ""))
	ms := decodeSyncTestMultiStatus(t, res)
	if ms.SyncToken != "2" {
		t.Errorf("sync-token = %q, want %q", ms.SyncToken, "2")
	}
	if len(ms.Responses) != 2 {
		t.Fatalf("got %d responses, want 2", len(ms.Responses))
	}

	updated := ms.Responses[0]
	if p, err := updated.Path(); err != nil || p != syncTestCalendarPath+"b.ics" {
		t.Errorf("updated response = %q, %v, want %q", p, err, syncTestCalendarPath+"b.ics")
	}

	deleted := ms.Responses[1]
	if len(deleted.Hrefs) != 1 || deleted.Hrefs[0].Path != syncTestCalendarPath+"c.ics" {
		t.Errorf("deleted hrefs = %v, want %q", deleted.Hrefs, syncTestCalendarPath+"c.ics")
	}
	if deleted.Status == nil || deleted.Status.Code != http.StatusNotFound {
		t.Errorf("deleted status = %v, want %v", deleted.Status, http.StatusNotFound)
	}
	if len(deleted.PropStats) != 0 {
		t.Errorf("deleted response has %d propstats, want 0", len(deleted.PropStats))
	}
}

func TestSyncCollectionInvalidToken(t *testing.T) {
	res := serveSyncTest(t, newSyncTestBackend(), "REPORT", "0", syncCollectionBody("stale", "1", ""))
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want %v", res.StatusCode, http.StatusForbidden)
	}
	assertSyncTestErrorBody(t, res.Body, internal.ValidSyncTokenName)
}

func TestSyncCollectionLimit(t *testing.T) {
	backend := newSyncTestBackend()
	limit := "\n  <d:limit><d:nresults>1</d:nresults></d:limit>"
	res := serveSyncTest(t, backend, "REPORT", "0", syncCollectionBody("", "1", limit))
	defer res.Body.Close()
	if backend.lastQuery == nil || backend.lastQuery.Limit != 1 {
		t.Errorf("backend query = %+v, want Limit 1", backend.lastQuery)
	}
	if res.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("status = %v, want %v", res.StatusCode, http.StatusInsufficientStorage)
	}
	assertSyncTestErrorBody(t, res.Body, internal.NumberOfMatchesWithinLimitsName)
}

func assertSyncTestErrorBody(t *testing.T, body io.Reader, want xml.Name) {
	t.Helper()
	var davErr internal.Error
	if err := xml.NewDecoder(body).Decode(&davErr); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	if len(davErr.Raw) != 1 {
		t.Fatalf("error has %d elements, want 1", len(davErr.Raw))
	}
	if name, ok := davErr.Raw[0].XMLName(); !ok || name != want {
		t.Errorf("error element = %v, want %v", name, want)
	}
}

func TestSyncCollectionBadRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		backend Backend
		depth   string
		level   string
	}{
		{"sync-level infinite", newSyncTestBackend(), "0", "infinite"},
		{"no SyncBackend", testBackend{calendars: []Calendar{{Path: syncTestCalendarPath}}}, "0", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := serveSyncTest(t, tc.backend, "REPORT", tc.depth, syncCollectionBody("", tc.level, ""))
			defer res.Body.Close()
			if res.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %v, want %v", res.StatusCode, http.StatusBadRequest)
			}
		})
	}
}

func TestClientSyncCollection(t *testing.T) {
	backend := newSyncTestBackend()
	srv := httptest.NewServer(&Handler{Backend: backend})
	defer srv.Close()

	c, err := NewClient(nil, srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	compReq := CalendarCompRequest{Name: ical.CompCalendar, AllProps: true, AllComps: true}

	initial, err := c.SyncCollection(context.Background(), syncTestCalendarPath, &SyncQuery{CompRequest: compReq})
	if err != nil {
		t.Fatalf("initial SyncCollection: %v", err)
	}
	if initial.SyncToken != "2" {
		t.Errorf("initial sync token = %q, want %q", initial.SyncToken, "2")
	}
	var paths []string
	for _, co := range initial.Updated {
		paths = append(paths, co.Path)
		want := backend.objectMap[syncTestCalendarPath][len(paths)-1]
		if co.ETag != want.ETag {
			t.Errorf("%s: ETag = %q, want %q", co.Path, co.ETag, want.ETag)
		}
		var got, wantData bytes.Buffer
		if co.Data == nil {
			t.Errorf("%s: missing calendar data", co.Path)
			continue
		}
		if err := ical.NewEncoder(&got).Encode(co.Data); err != nil {
			t.Fatalf("encode: %v", err)
		}
		if err := ical.NewEncoder(&wantData).Encode(want.Data); err != nil {
			t.Fatalf("encode: %v", err)
		}
		if got.String() != wantData.String() {
			t.Errorf("%s: calendar data = %q, want %q", co.Path, got.String(), wantData.String())
		}
	}
	sort.Strings(paths)
	if want := []string{syncTestCalendarPath + "a.ics", syncTestCalendarPath + "b.ics"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("initial updated = %v, want %v", paths, want)
	}
	if len(initial.Deleted) != 0 {
		t.Errorf("initial deleted = %v, want none", initial.Deleted)
	}

	incremental, err := c.SyncCollection(context.Background(), syncTestCalendarPath, &SyncQuery{CompRequest: compReq, SyncToken: "1"})
	if err != nil {
		t.Fatalf("incremental SyncCollection: %v", err)
	}
	if len(incremental.Updated) != 1 || incremental.Updated[0].Path != syncTestCalendarPath+"b.ics" {
		t.Errorf("incremental updated = %+v, want %q", incremental.Updated, syncTestCalendarPath+"b.ics")
	}
	if want := []string{syncTestCalendarPath + "c.ics"}; !reflect.DeepEqual(incremental.Deleted, want) {
		t.Errorf("incremental deleted = %v, want %v", incremental.Deleted, want)
	}

	if _, err := c.SyncCollection(context.Background(), syncTestCalendarPath, &SyncQuery{CompRequest: compReq, SyncToken: "stale"}); err == nil {
		t.Error("SyncCollection with an invalid token succeeded, want error")
	}
}
