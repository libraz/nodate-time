package dav

import (
	"context"
	"net/http"

	goical "github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

// PutCalendarObject implements caldav.Backend. Writing through CalDAV is
// refused.
func (b *backend) PutCalendarObject(context.Context, string, *goical.Calendar, *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	return nil, httpError(http.StatusForbidden)
}

// DeleteCalendarObject implements caldav.Backend. Deleting through CalDAV is
// refused.
func (b *backend) DeleteCalendarObject(context.Context, string) error {
	return httpError(http.StatusForbidden)
}
