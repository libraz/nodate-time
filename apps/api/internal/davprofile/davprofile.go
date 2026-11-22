// Package davprofile builds the Apple configuration profile that sets up a
// CalDAV account on an iPhone or a Mac in one step. The profile is unsigned
// and carries the app password in the clear, so it is only ever handed to the
// device it was made for.
package davprofile

import (
	"bytes"
	"encoding/xml"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/dav"
)

// ContentType is the media type a browser hands to the profile installer.
const ContentType = "application/x-apple-aspen-config"

// accountPayloadType is the payload that configures a CalDAV account.
const accountPayloadType = "com.apple.caldav.account"

// Params is what one profile is built from.
type Params struct {
	// AppName names the profile, its organization and the account.
	AppName string
	// DeviceName is the label the user gave the app password.
	DeviceName string
	// Email is the CalDAV username.
	Email    string
	Password string
	// PublicURL is the address the API answers at from outside; the server's
	// host, port, TLS and principal URL are taken from it.
	PublicURL string
	// ProfileUUID identifies this profile; the account payload's UUID is
	// derived from it.
	ProfileUUID uuid.UUID
	// UserUUID keys the profile identifier, so a profile made again for the
	// same user replaces the installed one.
	UserUUID uuid.UUID
}

// Build renders the profile as an XML property list.
func Build(p Params) []byte {
	u, err := url.Parse(p.PublicURL)
	if err != nil {
		u = &url.URL{}
	}
	host := u.Hostname()
	useSSL := u.Scheme == "https"
	port := 80
	if useSSL {
		port = 443
	}
	if n, err := strconv.Atoi(u.Port()); err == nil {
		port = n
	}
	principal := strings.TrimRight(p.PublicURL, "/") + dav.MountPath + "/"
	identifier := payloadIdentifier(host, p.UserUUID)

	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	key(&b, "PayloadContent")
	b.WriteString("<array>\n<dict>\n")
	str(&b, "CalDAVAccountDescription", p.AppName)
	str(&b, "CalDAVHostName", host)
	integer(&b, "CalDAVPort", port)
	str(&b, "CalDAVPrincipalURL", principal)
	str(&b, "CalDAVUsername", p.Email)
	str(&b, "CalDAVPassword", p.Password)
	boolean(&b, "CalDAVUseSSL", useSSL)
	str(&b, "PayloadIdentifier", identifier+".account")
	str(&b, "PayloadType", accountPayloadType)
	str(&b, "PayloadUUID", accountUUID(p.ProfileUUID).String())
	integer(&b, "PayloadVersion", 1)
	b.WriteString("</dict>\n</array>\n")
	str(&b, "PayloadDescription", p.DeviceName)
	str(&b, "PayloadDisplayName", p.AppName)
	str(&b, "PayloadIdentifier", identifier)
	str(&b, "PayloadOrganization", p.AppName)
	boolean(&b, "PayloadRemovalDisallowed", false)
	str(&b, "PayloadType", "Configuration")
	str(&b, "PayloadUUID", p.ProfileUUID.String())
	integer(&b, "PayloadVersion", 1)
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes()
}

// payloadIdentifier is the host's labels reversed, then "caldav" and the user.
func payloadIdentifier(host string, user uuid.UUID) string {
	labels := slices.DeleteFunc(strings.Split(host, "."), func(s string) bool { return s == "" })
	slices.Reverse(labels)
	return strings.Join(append(labels, "caldav", user.String()), ".")
}

// accountUUID derives the account payload's UUID, so one profile always
// carries the same one.
func accountUUID(profile uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(profile, []byte(accountPayloadType))
}

func key(b *bytes.Buffer, k string) {
	b.WriteString("<key>")
	_ = xml.EscapeText(b, []byte(k))
	b.WriteString("</key>\n")
}

func str(b *bytes.Buffer, k, v string) {
	key(b, k)
	b.WriteString("<string>")
	_ = xml.EscapeText(b, []byte(v))
	b.WriteString("</string>\n")
}

func integer(b *bytes.Buffer, k string, v int) {
	key(b, k)
	b.WriteString("<integer>" + strconv.Itoa(v) + "</integer>\n")
}

func boolean(b *bytes.Buffer, k string, v bool) {
	key(b, k)
	if v {
		b.WriteString("<true/>\n")
	} else {
		b.WriteString("<false/>\n")
	}
}
