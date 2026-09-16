package jmapcalendar

import "context"

// CalendarsCapabilityURI is the standard JMAP Calendars capability URI.
const CalendarsCapabilityURI = "urn:ietf:params:jmap:calendars"

// CalendarsParseCapabilityURI is the JMAP capability URI advertising support for the
// CalendarEvent/parse method per draft-ietf-jmap-calendars Section 1.5.3.
const CalendarsParseCapabilityURI = "urn:ietf:params:jmap:calendars:parse"

const (
	// Default calendar capability limits per draft-ietf-jmap-calendars-27 Section 1.5.1 and 5.11.
	DefaultMinDateTime              = "1900-01-01T00:00:00"
	DefaultMaxDateTime              = "9999-12-31T23:59:59"
	DefaultMaxExpandedQueryDuration = "P730D"
)

// CalendarsCapability defines the capability object for "urn:ietf:params:jmap:calendars"
// per draft-ietf-jmap-calendars Section 1.5.1.
type CalendarsCapability struct {
	MaxCalendarsPerEvent     *uint64 `json:"maxCalendarsPerEvent"`
	MayCreateCalendar        bool    `json:"mayCreateCalendar"`
	MinDateTime              string  `json:"minDateTime"`
	MaxDateTime              string  `json:"maxDateTime"`
	MaxExpandedQueryDuration string  `json:"maxExpandedQueryDuration"`
	MaxParticipantsPerEvent  *uint64 `json:"maxParticipantsPerEvent"`
}

type calendarsCapabilityKey struct{}

// WithCalendarsCapability returns a new context carrying the given CalendarsCapability limits.
func WithCalendarsCapability(ctx context.Context, cap CalendarsCapability) context.Context {
	return context.WithValue(ctx, calendarsCapabilityKey{}, cap)
}

// CalendarsCapabilityFromContext extracts CalendarsCapability limits from context, or returns default limits.
func CalendarsCapabilityFromContext(ctx context.Context) CalendarsCapability {
	if ctx != nil {
		if cap, ok := ctx.Value(calendarsCapabilityKey{}).(CalendarsCapability); ok {
			if cap.MinDateTime == "" {
				cap.MinDateTime = DefaultMinDateTime
			}
			if cap.MaxDateTime == "" {
				cap.MaxDateTime = DefaultMaxDateTime
			}
			if cap.MaxExpandedQueryDuration == "" {
				cap.MaxExpandedQueryDuration = DefaultMaxExpandedQueryDuration
			}
			return cap
		}
	}
	return CalendarsCapability{
		MinDateTime:              DefaultMinDateTime,
		MaxDateTime:              DefaultMaxDateTime,
		MaxExpandedQueryDuration: DefaultMaxExpandedQueryDuration,
	}
}
