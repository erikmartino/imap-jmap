package jmapcalendar

import "context"

// Comparator defines sorting rules per RFC 8621 Section 4.4.2.
// This mirrors jmap.Comparator; jmap re-exports this type as a type alias.
type Comparator struct {
	Property    string `json:"property"`
	IsAscending bool   `json:"isAscending"`
	Collation   string `json:"collation,omitempty"`
	Keyword     string `json:"keyword,omitempty"`
}

// CalendarsBackend defines the storage interface for JMAP Calendars & JSCalendar (RFC 8984) resources.
type CalendarsBackend interface {
	// Calendars
	CalendarState(ctx context.Context) string
	CalendarChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetCalendars(ctx context.Context, ids []Id) (list []*Calendar, notFound []Id, err error)
	GetAllCalendars(ctx context.Context) ([]*Calendar, error)
	CreateCalendar(ctx context.Context, cal *Calendar) (*Calendar, error)
	UpdateCalendar(ctx context.Context, id Id, patch map[string]any) (*Calendar, error)
	DeleteCalendar(ctx context.Context, id Id) (bool, error)
	SetDefaultCalendar(ctx context.Context, id Id) error
	CalendarHasEvents(ctx context.Context, id Id) (bool, error)

	// CalendarEvents (JSCalendar RFC 8984)
	CalendarEventState(ctx context.Context) string
	CalendarEventChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetCalendarEvents(ctx context.Context, ids []Id) (list []*CalendarEvent, notFound []Id, err error)
	GetAllCalendarEvents(ctx context.Context) ([]*CalendarEvent, error)
	CreateCalendarEvent(ctx context.Context, event *CalendarEvent) (*CalendarEvent, error)
	UpdateCalendarEvent(ctx context.Context, id Id, patch map[string]any) (*CalendarEvent, error)
	DeleteCalendarEvent(ctx context.Context, id Id) (bool, error)
	QueryCalendarEvents(ctx context.Context, filter map[string]any, sort []Comparator, position int, limit *uint64, expandRecurrences bool) (ids []Id, total int, err error)

	// ParticipantIdentities (draft-ietf-jmap-calendars Section 3)
	ParticipantIdentityState(ctx context.Context) string
	ParticipantIdentityChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetParticipantIdentities(ctx context.Context, ids []Id) (list []*ParticipantIdentity, notFound []Id, err error)
	GetAllParticipantIdentities(ctx context.Context) ([]*ParticipantIdentity, error)
	CreateParticipantIdentity(ctx context.Context, identity *ParticipantIdentity) (*ParticipantIdentity, error)
	UpdateParticipantIdentity(ctx context.Context, id Id, patch map[string]any) (*ParticipantIdentity, error)
	DeleteParticipantIdentity(ctx context.Context, id Id) (bool, error)
	SetDefaultParticipantIdentity(ctx context.Context, id Id) error

	// CalendarEventNotifications (draft-ietf-jmap-calendars Section 7)
	CalendarEventNotificationState(ctx context.Context) string
	CalendarEventNotificationChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetCalendarEventNotifications(ctx context.Context, ids []Id) (list []*CalendarEventNotification, notFound []Id, err error)
	GetAllCalendarEventNotifications(ctx context.Context) ([]*CalendarEventNotification, error)
	CreateCalendarEventNotification(ctx context.Context, notification *CalendarEventNotification) (*CalendarEventNotification, error)
	DeleteCalendarEventNotification(ctx context.Context, id Id) (bool, error)
	QueryCalendarEventNotifications(ctx context.Context, filter map[string]any, sort []Comparator, position int, limit *uint64) (ids []Id, total int, err error)

	// ShareNotifications (RFC 9670)
	ShareNotificationState(ctx context.Context) string
	ShareNotificationChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetShareNotifications(ctx context.Context, ids []Id) (list []*ShareNotification, notFound []Id, err error)
	GetAllShareNotifications(ctx context.Context) ([]*ShareNotification, error)
	CreateShareNotification(ctx context.Context, notification *ShareNotification) (*ShareNotification, error)
	DeleteShareNotification(ctx context.Context, id Id) (bool, error)
}
