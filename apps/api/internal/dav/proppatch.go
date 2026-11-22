package dav

import (
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
)

// propPatchForbidden answers a PROPPATCH with every property it names refused
// 403. Nothing a client can set here is stored per client: a calendar's name
// and colour are shared by its members, so one device may not change them.
func propPatchForbidden(w http.ResponseWriter, r *http.Request, href string) {
	names, err := propPatchNames(io.LimitReader(r.Body, maxReportBody))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	type prop struct {
		XMLName xml.Name
	}
	type propList struct {
		Props []prop `xml:",any"`
	}
	type propstat struct {
		Prop   propList `xml:"DAV: prop"`
		Status string   `xml:"DAV: status"`
	}
	type response struct {
		Href     string   `xml:"DAV: href"`
		Propstat propstat `xml:"DAV: propstat"`
	}
	type multistatus struct {
		XMLName  xml.Name `xml:"DAV: multistatus"`
		Response response `xml:"DAV: response"`
	}
	ms := multistatus{Response: response{
		Href:     (&url.URL{Path: href}).EscapedPath(),
		Propstat: propstat{Status: "HTTP/1.1 403 Forbidden"},
	}}
	for _, n := range names {
		ms.Response.Propstat.Prop.Props = append(ms.Response.Propstat.Prop.Props, prop{XMLName: n})
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(ms)
}

// propPatchNames lists the properties a DAV:propertyupdate sets or removes:
// the children of each DAV:prop inside DAV:set or DAV:remove.
func propPatchNames(body io.Reader) ([]xml.Name, error) {
	dec := xml.NewDecoder(body)
	var stack []xml.Name
	var names []xml.Name
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if len(stack) == 3 && stack[2] == davName("prop") &&
				(stack[1] == davName("set") || stack[1] == davName("remove")) {
				names = append(names, el.Name)
			}
			stack = append(stack, el.Name)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return names, nil
}

func davName(local string) xml.Name { return xml.Name{Space: "DAV:", Local: local} }
