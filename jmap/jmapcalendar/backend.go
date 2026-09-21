package jmapcalendar

import (
	"context"

	"imap-jmap/jmap/jmapcore"
)

// CalendarsBackend defines the storage interface for JMAP Calendars & JSCalendar (RFC 8984) resources.
type CalendarsBackend interface {
	// Calendars
	CalendarState(ctx context.Context) string
	CalendarChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetCalendars(ctx context.Context, ids []jmapcore.Id) (list []*Calendar, notFound []jmapcore.Id, err error)
	GetAllCalendars(ctx context.Context) ([]*Calendar, error)
	CreateCalendar(ctx context.Context, cal *Calendar) (*Calendar, error)
	UpdateCalendar(ctx context.Context, id jmapcore.Id, patch map[string]any) (*Calendar, error)
	DeleteCalendar(ctx context.Context, id jmapcore.Id) (bool, error)
	SetDefaultCalendar(ctx context.Context, id jmapcore.Id) error
	CalendarHasEvents(ctx context.Context, id jmapcore.Id) (bool, error)

	// CalendarEvents (JSCalendar RFC 8984)
	CalendarEventState(ctx context.Context) string
	CalendarEventChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetCalendarEvents(ctx context.Context, ids []jmapcore.Id) (list []*CalendarEvent, notFound []jmapcore.Id, err error)
	GetAllCalendarEvents(ctx context.Context) ([]*CalendarEvent, error)
	CreateCalendarEvent(ctx context.Context, event *CalendarEvent) (*CalendarEvent, error)
	UpdateCalendarEvent(ctx context.Context, id jmapcore.Id, patch map[string]any) (*CalendarEvent, error)
	DeleteCalendarEvent(ctx context.Context, id jmapcore.Id) (bool, error)
	QueryCalendarEvents(ctx context.Context, filter map[string]any, sort []jmapcore.Comparator, position int, limit *uint64, expandRecurrences bool) (ids []jmapcore.Id, total int, err error)

	// ParticipantIdentities (draft-ietf-jmap-calendars Section 3)
	ParticipantIdentityState(ctx context.Context) string
	ParticipantIdentityChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetParticipantIdentities(ctx context.Context, ids []jmapcore.Id) (list []*ParticipantIdentity, notFound []jmapcore.Id, err error)
	GetAllParticipantIdentities(ctx context.Context) ([]*ParticipantIdentity, error)
	CreateParticipantIdentity(ctx context.Context, identity *ParticipantIdentity) (*ParticipantIdentity, error)
	UpdateParticipantIdentity(ctx context.Context, id jmapcore.Id, patch map[string]any) (*ParticipantIdentity, error)
	DeleteParticipantIdentity(ctx context.Context, id jmapcore.Id) (bool, error)
	SetDefaultParticipantIdentity(ctx context.Context, id jmapcore.Id) error

	// CalendarEventNotifications (draft-ietf-jmap-calendars Section 7)
	CalendarEventNotificationState(ctx context.Context) string
	CalendarEventNotificationChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetCalendarEventNotifications(ctx context.Context, ids []jmapcore.Id) (list []*CalendarEventNotification, notFound []jmapcore.Id, err error)
	GetAllCalendarEventNotifications(ctx context.Context) ([]*CalendarEventNotification, error)
	CreateCalendarEventNotification(ctx context.Context, notification *CalendarEventNotification) (*CalendarEventNotification, error)
	DeleteCalendarEventNotification(ctx context.Context, id jmapcore.Id) (bool, error)
	QueryCalendarEventNotifications(ctx context.Context, filter map[string]any, sort []jmapcore.Comparator, position int, limit *uint64) (ids []jmapcore.Id, total int, err error)

	// ShareNotifications (RFC 9670)
	ShareNotificationState(ctx context.Context) string
	ShareNotificationChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetShareNotifications(ctx context.Context, ids []jmapcore.Id) (list []*ShareNotification, notFound []jmapcore.Id, err error)
	GetAllShareNotifications(ctx context.Context) ([]*ShareNotification, error)
	CreateShareNotification(ctx context.Context, notification *ShareNotification) (*ShareNotification, error)
	DeleteShareNotification(ctx context.Context, id jmapcore.Id) (bool, error)
	QueryShareNotifications(ctx context.Context, filter map[string]any, sort []jmapcore.Comparator, position int, limit *uint64) (ids []jmapcore.Id, total int, err error)
}
