package jmap

// This file re-exports all calendar domain types from the jmapcalendar sub-package
// as type aliases, preserving full backward compatibility for all existing callers.

import (
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmappush"
)

// CalendarRights defines access rights for a Calendar per JMAP for Calendars.
type CalendarRights = jmapcalendar.CalendarRights

// FullCalendarRights returns a CalendarRights with all permissions granted.
var FullCalendarRights = jmapcalendar.FullCalendarRights

// Calendar represents a Calendar object per JMAP for Calendars.
type Calendar = jmapcalendar.Calendar

// NDay represents a day of the week with optional nth-occurrence per RFC 8984 Section 4.3.3.
type NDay = jmapcalendar.NDay

// OffsetTrigger defines an offset trigger object per RFC 8984 Section 4.5.2.
type OffsetTrigger = jmapcalendar.OffsetTrigger

// JSCalendarLocation defines a location object per RFC 8984 Section 4.2.5.
type JSCalendarLocation = jmapcalendar.JSCalendarLocation

// JSCalendarParticipant defines a participant object per RFC 8984 Section 4.4.6.
type JSCalendarParticipant = jmapcalendar.JSCalendarParticipant

// JSCalendarRecurrenceRule defines a recurrence rule object per RFC 8984 Section 4.3.3.
type JSCalendarRecurrenceRule = jmapcalendar.JSCalendarRecurrenceRule

// JSCalendarAlert defines an alert/alarm object per RFC 8984 Section 4.5.2.
type JSCalendarAlert = jmapcalendar.JSCalendarAlert

// CalendarAlert represents a triggered calendar alert per draft-ietf-jmap-calendars-27 Section 8.
type CalendarAlert = jmappush.CalendarAlert

// ParticipantIdentity represents a URI that identifies the user within an account in an
// event's participants per draft-ietf-jmap-calendars Section 3.
type ParticipantIdentity = jmapcalendar.ParticipantIdentity

// CalendarEventNotificationPerson identifies who made a change to a calendar event per
// draft-ietf-jmap-calendars Section 7.2.
type CalendarEventNotificationPerson = jmapcalendar.CalendarEventNotificationPerson

// CalendarEventNotification records a change made by an external entity to an event in a
// calendar the user is subscribed to per draft-ietf-jmap-calendars Section 7.2.
type CalendarEventNotification = jmapcalendar.CalendarEventNotification

// CalendarEvent represents a JSCalendar Event object per RFC 8984 & JMAP for Calendars.
type CalendarEvent = jmapcalendar.CalendarEvent
