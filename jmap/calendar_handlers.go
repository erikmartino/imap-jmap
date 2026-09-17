package jmap

import (
	"imap-jmap/jmap/jmapcalendar"
)

// RegisterCalendarHandlers registers JMAP for Calendars & JSCalendar method handlers into MethodRegistry.
func RegisterCalendarHandlers(r *MethodRegistry, backend CalendarsBackend, mailBackend MailBackend, principalsBackend PrincipalsBackend, blobBackend BlobBackend, resolver AccountResolver) {
	jmapcalendar.RegisterCalendarHandlers(r, backend, mailBackend, principalsBackend, blobBackend, resolver)
}

// ITIPMessage represents a parsed iTIP / iMIP scheduling message.
type (
	ITIPMessage        = jmapcalendar.ITIPMessage
	RecurrenceInstance = jmapcalendar.RecurrenceInstance
)

var (
	MatchCalendarEvent        = jmapcalendar.MatchCalendarEvent
	LoadLocation              = jmapcalendar.LoadLocation
	ParseLocalDateTimeBound   = jmapcalendar.ParseLocalDateTimeBound
	ParseISODuration          = jmapcalendar.ParseISODuration
	ComputeUTCStart           = jmapcalendar.ComputeUTCStart
	ComputeUTCEnd             = jmapcalendar.ComputeUTCEnd
	ParseRFC3339              = jmapcalendar.ParseRFC3339
	ExpandRecurrenceInstances = jmapcalendar.ExpandRecurrenceInstances
	SortCalendarEvents        = jmapcalendar.SortCalendarEvents
	BuildITIPReply            = jmapcalendar.BuildITIPReply
	BuildITIPRequest          = jmapcalendar.BuildITIPRequest
	BuildITIPCancel           = jmapcalendar.BuildITIPCancel
	BuildITIPAdd              = jmapcalendar.BuildITIPAdd
	BuildITIPRefresh          = jmapcalendar.BuildITIPRefresh
	BuildITIPCounter          = jmapcalendar.BuildITIPCounter
	ParseITIPMessage          = jmapcalendar.ParseITIPMessage
	ParseICalendar            = jmapcalendar.ParseICalendar
	CalendarEventToICalendar  = jmapcalendar.CalendarEventToICalendar
	EncodeCalDAVEvent         = jmapcalendar.EncodeCalDAVEvent
	IcalDurationBetween       = jmapcalendar.IcalDurationBetween
	ExpandGroupRecipients     = jmapcalendar.ExpandGroupRecipients
	expandGroupRecipients     = jmapcalendar.ExpandGroupRecipients
)
