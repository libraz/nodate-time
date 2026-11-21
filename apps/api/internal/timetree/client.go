package timetree

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultBaseURL = "https://timetreeapp.com"
	clientHeader   = "web/2.1.0/ja"
	sessionCookie  = "_session_id"
	httpTimeout    = 60 * time.Second
)

// flexID reads an identifier that TimeTree sends as a number in some
// payloads and as a string in others.
type flexID string

func (f *flexID) UnmarshalJSON(b []byte) error {
	s := string(bytes.TrimSpace(b))
	if s == "null" {
		*f = ""
		return nil
	}
	if len(s) > 0 && s[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*f = flexID(str)
		return nil
	}
	*f = flexID(s)
	return nil
}

type ttCalendar struct {
	ID            flexID          `json:"id"`
	Name          string          `json:"name"`
	AliasCode     string          `json:"alias_code"`
	DeactivatedAt json.RawMessage `json:"deactivated_at"`
	// Users lists the calendar's people, those who left included.
	Users []ttCalendarUser `json:"calendar_users"`
}

// ttCalendarUser is one person of a calendar under the name shown there.
type ttCalendarUser struct {
	UserID flexID `json:"user_id"`
	Name   string `json:"name"`
}

type ttLabel struct {
	ID   flexID `json:"id"`
	Name string `json:"name"`
}

type ttEvent struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	Note          string            `json:"note"`
	Location      string            `json:"location"`
	URL           string            `json:"url"`
	StartAt       int64             `json:"start_at"`
	EndAt         int64             `json:"end_at"`
	StartTimezone string            `json:"start_timezone"`
	AllDay        bool              `json:"all_day"`
	LabelID       flexID            `json:"label_id"`
	AuthorID      flexID            `json:"author_id"`
	Attendees     []json.RawMessage `json:"attendees"`
	Recurrences   []string          `json:"recurrences"`
	RecurringUUID string            `json:"recurring_uuid"`
	Type          int               `json:"type"`
	Category      int               `json:"category"`
	DeletedAt     json.RawMessage   `json:"deleted_at"`
	DeactivatedAt json.RawMessage   `json:"deactivated_at"`
}

// isSet reports whether a nullable JSON field carries a value.
func isSet(raw json.RawMessage) bool {
	s := string(bytes.TrimSpace(raw))
	return s != "" && s != "null"
}

// client talks to the TimeTree web API with one login session.
type client struct {
	base string
	http *http.Client
}

func newClient(base string) (*client, error) {
	if base == "" {
		base = defaultBaseURL
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &client{base: base, http: &http.Client{Jar: jar, Timeout: httpTimeout}}, nil
}

// loginError carries the TimeTree error code of a refused login.
type loginError struct {
	status  int
	code    int
	hasCode bool
}

func (e *loginError) Error() string {
	if e.hasCode {
		return fmt.Sprintf("timetree login failed: HTTP %d, error code %d", e.status, e.code)
	}
	return fmt.Sprintf("timetree login failed: HTTP %d", e.status)
}

func (c *client) login(ctx context.Context, email, password string) error {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("generate device id: %w", err)
	}
	body, err := json.Marshal(map[string]string{
		"uid": email, "password": password, "uuid": hex.EncodeToString(raw),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+"/api/v1/auth/email/signin", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timetreea", clientHeader)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("timetree login: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		le := &loginError{status: resp.StatusCode}
		var payload struct {
			Code *int `json:"code"`
		}
		if json.Unmarshal(data, &payload) == nil && payload.Code != nil {
			le.code, le.hasCode = *payload.Code, true
		}
		return le
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return err
	}
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == sessionCookie {
			return nil
		}
	}
	return errors.New("timetree login: no session cookie in the response")
}

// get fetches path and returns the top-level JSON object.
func (c *client) get(ctx context.Context, path string) (map[string]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Timetreea", clientHeader)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("timetree GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("timetree GET %s: HTTP %d", path, resp.StatusCode)
	}
	var obj map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return nil, fmt.Errorf("timetree GET %s: decode: %w", path, err)
	}
	return obj, nil
}

// field decodes one required key of a response object.
func field(obj map[string]json.RawMessage, path, key string, out any) error {
	raw, ok := obj[key]
	if !ok {
		return fmt.Errorf("timetree GET %s: response has no %q", path, key)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("timetree GET %s: decode %q: %w", path, key, err)
	}
	return nil
}

// calendars lists the calendars that are not deactivated.
func (c *client) calendars(ctx context.Context) ([]ttCalendar, error) {
	const path = "/api/v1/calendars?since=0"
	obj, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var all []ttCalendar
	if err := field(obj, path, "calendars", &all); err != nil {
		return nil, err
	}
	live := all[:0]
	for _, cal := range all {
		if !isSet(cal.DeactivatedAt) {
			live = append(live, cal)
		}
	}
	return live, nil
}

func (c *client) labels(ctx context.Context, calendarID flexID) ([]ttLabel, error) {
	path := "/api/v1/calendar/" + url.PathEscape(string(calendarID)) + "/labels"
	obj, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var labels []ttLabel
	if err := field(obj, path, "calendar_labels", &labels); err != nil {
		return nil, err
	}
	return labels, nil
}

// events pages through the sync endpoint until the server reports no more
// chunks.
func (c *client) events(ctx context.Context, calendarID flexID) ([]ttEvent, error) {
	base := "/api/v1/calendar/" + url.PathEscape(string(calendarID)) + "/events/sync"
	path := base
	var out []ttEvent
	for {
		obj, err := c.get(ctx, path)
		if err != nil {
			return nil, err
		}
		var page []ttEvent
		if err := field(obj, path, "events", &page); err != nil {
			return nil, err
		}
		out = append(out, page...)
		var chunk bool
		if raw, ok := obj["chunk"]; ok {
			if err := json.Unmarshal(raw, &chunk); err != nil {
				return nil, fmt.Errorf("timetree GET %s: decode \"chunk\": %w", path, err)
			}
		}
		if !chunk {
			return out, nil
		}
		var since int64
		if err := field(obj, path, "since", &since); err != nil {
			return nil, err
		}
		next := base + "?since=" + strconv.FormatInt(since, 10)
		if next == path {
			return nil, fmt.Errorf("timetree GET %s: sync cursor did not advance", path)
		}
		path = next
	}
}

// latestByID folds repeated IDs into the last copy, which on a cursor feed is
// the newest, keeping the first copy's position. repeated counts the folds.
func latestByID(events []ttEvent) (out []ttEvent, repeated int) {
	at := make(map[string]int, len(events))
	out = make([]ttEvent, 0, len(events))
	for _, e := range events {
		if i, ok := at[e.ID]; ok && e.ID != "" {
			out[i] = e
			repeated++
			continue
		}
		at[e.ID] = len(out)
		out = append(out, e)
	}
	return out, repeated
}
