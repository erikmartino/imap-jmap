package dav

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"

	"imap-jmap/jmap"
)

// CalDAVBackend implements caldav.Backend bridging WebDAV CalDAV requests to jmap.CalendarsBackend (RFC 4791).
type CalDAVBackend struct {
	Backend jmap.CalendarsBackend
}

// NewCalDAVBackend initializes a new CalDAVBackend.
func NewCalDAVBackend(backend jmap.CalendarsBackend) *CalDAVBackend {
	return &CalDAVBackend{
		Backend: backend,
	}
}

func (b *CalDAVBackend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	return "/caldav/principals/user", nil
}

func (b *CalDAVBackend) CalendarHomeSetPath(ctx context.Context) (string, error) {
	return "/caldav/calendars/", nil
}

func (b *CalDAVBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	if b.Backend == nil {
		return []caldav.Calendar{
			{
				Path:        "/caldav/calendars/default",
				Name:        "Personal Calendar",
				Description: "Default Personal Calendar",
			},
		}, nil
	}

	cals, _, err := b.Backend.GetCalendars(ctx, nil)
	if err != nil {
		return nil, err
	}

	var list []caldav.Calendar
	for _, c := range cals {
		desc := ""
		if c.Description != nil {
			desc = *c.Description
		}
		list = append(list, caldav.Calendar{
			Path:        "/caldav/calendars/" + string(c.ID),
			Name:        c.Name,
			Description: desc,
		})
	}
	return list, nil
}

func (b *CalDAVBackend) GetCalendar(ctx context.Context, path string) (*caldav.Calendar, error) {
	cals, err := b.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}

	for _, c := range cals {
		if c.Path == path || strings.HasSuffix(path, c.Path) {
			return &c, nil
		}
	}
	return &caldav.Calendar{
		Path:        path,
		Name:        "Calendar",
		Description: "CalDAV Calendar",
	}, nil
}

func (b *CalDAVBackend) CreateCalendar(ctx context.Context, cal *caldav.Calendar) error {
	if b.Backend == nil {
		return nil
	}
	desc := cal.Description
	_, err := b.Backend.CreateCalendar(ctx, &jmap.Calendar{
		Name:        cal.Name,
		Description: &desc,
	})
	return err
}

func (b *CalDAVBackend) DeleteCalendar(ctx context.Context, path string) error {
	if b.Backend == nil {
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 {
		calID := parts[len(parts)-1]
		_, err := b.Backend.DeleteCalendar(ctx, jmap.Id(calID))
		return err
	}
	return nil
}

func (b *CalDAVBackend) ListCalendarObjects(ctx context.Context, path string, req *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	if b.Backend == nil {
		return []caldav.CalendarObject{}, nil
	}

	events, _, err := b.Backend.GetCalendarEvents(ctx, nil)
	if err != nil {
		return nil, err
	}

	var list []caldav.CalendarObject
	for _, ev := range events {
		icsStr, err := jmap.BuildITIPRequest(ev, "user@example.com")
		if err != nil {
			continue
		}
		dec := ical.NewDecoder(strings.NewReader(icsStr))
		calObj, err := dec.Decode()
		if err != nil {
			continue
		}

		list = append(list, caldav.CalendarObject{
			Path:    path + "/" + string(ev.ID) + ".ics",
			Data:    calObj,
			ModTime: time.Now(),
		})
	}
	return list, nil
}

func (b *CalDAVBackend) GetCalendarObject(ctx context.Context, path string, req *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	objs, err := b.ListCalendarObjects(ctx, "/caldav/calendars/default", req)
	if err != nil {
		return nil, err
	}

	for _, obj := range objs {
		if obj.Path == path || strings.HasSuffix(path, obj.Path) {
			return &obj, nil
		}
	}
	return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
}

func (b *CalDAVBackend) PutCalendarObject(ctx context.Context, path string, cal *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	if b.Backend != nil && cal != nil {
		for _, comp := range cal.Children {
			if comp.Name == "VEVENT" {
				uidProp := comp.Props.Get("UID")
				summaryProp := comp.Props.Get("SUMMARY")
				dtstartProp := comp.Props.Get("DTSTART")
				locationProp := comp.Props.Get("LOCATION")
				descriptionProp := comp.Props.Get("DESCRIPTION")
				durationProp := comp.Props.Get("DURATION")

				uidStr := ""
				if uidProp != nil {
					uidStr = uidProp.Value
				} else {
					parts := strings.Split(strings.Trim(path, "/"), "/")
					if len(parts) > 0 {
						uidStr = strings.TrimSuffix(parts[len(parts)-1], ".ics")
					}
				}
				summaryStr := ""
				if summaryProp != nil {
					summaryStr = summaryProp.Value
				}
				startStr := ""
				if dtstartProp != nil {
					startStr = dtstartProp.Value
				}

				ev := &jmap.CalendarEvent{
					ID:    jmap.Id(uidStr),
					Title: summaryStr,
					Start: startStr,
				}
				if locationProp != nil {
					ev.Location = &jmap.JSCalendarLocation{Name: locationProp.Value}
				}
				if descriptionProp != nil {
					ev.Description = descriptionProp.Value
				}
				if durationProp != nil {
					ev.Duration = durationProp.Value
				}

				_, _ = b.Backend.CreateCalendarEvent(ctx, ev)
			}
		}
	}
	return &caldav.CalendarObject{
		Path: path,
		Data: cal,
	}, nil
}

func (b *CalDAVBackend) QueryCalendarObjects(ctx context.Context, path string, query *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	return b.ListCalendarObjects(ctx, path, nil)
}

func (b *CalDAVBackend) DeleteCalendarObject(ctx context.Context, path string) error {
	if b.Backend == nil {
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 {
		filename := parts[len(parts)-1]
		evID := strings.TrimSuffix(filename, ".ics")
		_, err := b.Backend.DeleteCalendarEvent(ctx, jmap.Id(evID))
		return err
	}
	return nil
}
