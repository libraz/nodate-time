package davprofile

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// readPlist decodes the subset of the property list format Build writes.
func readPlist(t *testing.T, data []byte) map[string]any {
	t.Helper()
	require.True(t, bytes.Contains(data, []byte(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"`)), "plist DTD")
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		require.NoError(t, err)
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "plist" {
			v, err := readValue(d)
			require.NoError(t, err)
			return v.(map[string]any)
		}
	}
}

func nextStart(d *xml.Decoder) (xml.StartElement, bool, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return xml.StartElement{}, false, err
		}
		switch tt := tok.(type) {
		case xml.StartElement:
			return tt, true, nil
		case xml.EndElement:
			return xml.StartElement{}, false, nil
		}
	}
}

func readValue(d *xml.Decoder) (any, error) {
	se, ok, err := nextStart(d)
	if err != nil || !ok {
		return nil, fmt.Errorf("expected a value: %v", err)
	}
	return readElement(d, se)
}

func readElement(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		out := map[string]any{}
		for {
			k, ok, err := nextStart(d)
			if err != nil {
				return nil, err
			}
			if !ok {
				return out, nil
			}
			if k.Name.Local != "key" {
				return nil, fmt.Errorf("expected key, got %s", k.Name.Local)
			}
			var name string
			if err := d.DecodeElement(&name, &k); err != nil {
				return nil, err
			}
			if _, dup := out[name]; dup {
				return nil, fmt.Errorf("duplicate key %s", name)
			}
			v, err := readValue(d)
			if err != nil {
				return nil, err
			}
			out[name] = v
		}
	case "array":
		var out []any
		for {
			e, ok, err := nextStart(d)
			if err != nil {
				return nil, err
			}
			if !ok {
				return out, nil
			}
			v, err := readElement(d, e)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	case "string":
		var s string
		err := d.DecodeElement(&s, &se)
		return s, err
	case "integer":
		var s string
		if err := d.DecodeElement(&s, &se); err != nil {
			return nil, err
		}
		return strconv.Atoi(s)
	case "true", "false":
		if err := d.Skip(); err != nil && err != io.EOF {
			return nil, err
		}
		return se.Name.Local == "true", nil
	}
	return nil, fmt.Errorf("unexpected element %s", se.Name.Local)
}

func account(t *testing.T, top map[string]any) map[string]any {
	t.Helper()
	content, ok := top["PayloadContent"].([]any)
	require.True(t, ok, "PayloadContent is an array")
	require.Len(t, content, 1)
	return content[0].(map[string]any)
}

var (
	testProfile = uuid.MustParse("019a0000-0000-7000-8000-000000000001")
	testUser    = uuid.MustParse("019a0000-0000-7000-8000-0000000000aa")
)

func params(publicURL string) Params {
	return Params{
		AppName:     "Nodate Time",
		DeviceName:  "iPhone",
		Email:       "user@example.com",
		Password:    "ndt_ABCDEFGHJKMNPQRSTVWXYZ0123456789",
		PublicURL:   publicURL,
		ProfileUUID: testProfile,
		UserUUID:    testUser,
	}
}

func TestBuildCarriesTheAccount(t *testing.T) {
	top := readPlist(t, Build(params("https://cal.example.com/api")))

	require.Equal(t, "Configuration", top["PayloadType"])
	require.Equal(t, 1, top["PayloadVersion"])
	require.Equal(t, "com.example.cal.caldav."+testUser.String(), top["PayloadIdentifier"])
	require.Equal(t, testProfile.String(), top["PayloadUUID"])
	require.Equal(t, "Nodate Time", top["PayloadDisplayName"])
	require.Equal(t, "iPhone", top["PayloadDescription"])
	require.Equal(t, "Nodate Time", top["PayloadOrganization"])
	require.Equal(t, false, top["PayloadRemovalDisallowed"])

	acc := account(t, top)
	require.Equal(t, "com.apple.caldav.account", acc["PayloadType"])
	require.Equal(t, 1, acc["PayloadVersion"])
	require.Equal(t, "com.example.cal.caldav."+testUser.String()+".account", acc["PayloadIdentifier"])
	require.Equal(t, "Nodate Time", acc["CalDAVAccountDescription"])
	require.Equal(t, "cal.example.com", acc["CalDAVHostName"])
	require.Equal(t, 443, acc["CalDAVPort"])
	require.Equal(t, true, acc["CalDAVUseSSL"])
	require.Equal(t, "https://cal.example.com/api/dav/", acc["CalDAVPrincipalURL"])
	require.Equal(t, "user@example.com", acc["CalDAVUsername"])
	require.Equal(t, "ndt_ABCDEFGHJKMNPQRSTVWXYZ0123456789", acc["CalDAVPassword"])

	accUUID, err := uuid.Parse(acc["PayloadUUID"].(string))
	require.NoError(t, err)
	require.NotEqual(t, testProfile, accUUID)
}

func TestBuildIsDeterministic(t *testing.T) {
	require.Equal(t, Build(params("https://cal.example.com")), Build(params("https://cal.example.com")))

	other := params("https://cal.example.com")
	other.ProfileUUID = uuid.MustParse("019a0000-0000-7000-8000-000000000002")
	a := account(t, readPlist(t, Build(params("https://cal.example.com"))))
	b := account(t, readPlist(t, Build(other)))
	require.NotEqual(t, a["PayloadUUID"], b["PayloadUUID"], "the account UUID follows the profile")
}

func TestBuildServerFromPublicURL(t *testing.T) {
	for _, c := range []struct {
		url       string
		host      string
		port      int
		ssl       bool
		principal string
		ident     string
	}{
		{"https://cal.example.com", "cal.example.com", 443, true, "https://cal.example.com/dav/", "com.example.cal"},
		{"https://cal.example.com/", "cal.example.com", 443, true, "https://cal.example.com/dav/", "com.example.cal"},
		{"http://cal.example.com", "cal.example.com", 80, false, "http://cal.example.com/dav/", "com.example.cal"},
		{"https://cal.example.com:8443/api/", "cal.example.com", 8443, true, "https://cal.example.com:8443/api/dav/", "com.example.cal"},
		{"http://localhost:8080", "localhost", 8080, false, "http://localhost:8080/dav/", "localhost"},
	} {
		t.Run(c.url, func(t *testing.T) {
			top := readPlist(t, Build(params(c.url)))
			acc := account(t, top)
			require.Equal(t, c.host, acc["CalDAVHostName"])
			require.Equal(t, c.port, acc["CalDAVPort"])
			require.Equal(t, c.ssl, acc["CalDAVUseSSL"])
			require.Equal(t, c.principal, acc["CalDAVPrincipalURL"])
			require.Equal(t, c.ident+".caldav."+testUser.String(), top["PayloadIdentifier"], "no port in the identifier")
		})
	}
}

func TestBuildWithoutPublicURL(t *testing.T) {
	top := readPlist(t, Build(params("")))
	acc := account(t, top)
	require.Equal(t, "/dav/", acc["CalDAVPrincipalURL"])
	require.Equal(t, "caldav."+testUser.String(), top["PayloadIdentifier"])
}

func TestBuildEscapesText(t *testing.T) {
	p := params("https://cal.example.com")
	p.AppName = `A&B <"Cal">`
	p.DeviceName = `Tom's <iPhone> & "Mac"`
	p.Email = `a&b"<c>@example.com`
	data := Build(p)
	require.False(t, strings.Contains(string(data), `<"Cal">`), "raw markup must not survive")

	top := readPlist(t, data)
	require.Equal(t, p.AppName, top["PayloadDisplayName"])
	require.Equal(t, p.AppName, top["PayloadOrganization"])
	require.Equal(t, p.DeviceName, top["PayloadDescription"])
	acc := account(t, top)
	require.Equal(t, p.AppName, acc["CalDAVAccountDescription"])
	require.Equal(t, p.Email, acc["CalDAVUsername"])
}
