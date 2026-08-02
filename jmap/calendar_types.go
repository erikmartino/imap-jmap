package jmap

// CalendarRights defines access rights for a Calendar per JMAP for Calendars.
type CalendarRights struct {
	MayReadItems  bool `json:"mayReadItems"`
	MayWriteItems bool `json:"mayWriteItems"`
	MayAdmin      bool `json:"mayAdmin"`
	MayDelete     bool `json:"mayDelete"`
}

// Calendar represents a Calendar object per JMAP for Calendars.
type Calendar struct {
	ID          Id             `json:"id"`
	Name        string         `json:"name"`
	Description *string        `json:"description,omitempty"`
	Color       *string        `json:"color,omitempty"`
	SortOrder   uint64         `json:"sortOrder"`
	IsDefault   bool           `json:"isDefault"`
	IsVisible   bool           `json:"isVisible"`
	MyRights    CalendarRights `json:"myRights"`
}

// JSCalendarLocation defines a location object per RFC 8984 Section 4.2.1.
type JSCalendarLocation struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Rel         string `json:"rel,omitempty"`
	TimeZone    string `json:"timeZone,omitempty"`
}

// JSCalendarParticipant defines a participant object per RFC 8984 Section 4.4.1.
type JSCalendarParticipant struct {
	Name        string `json:"name,omitempty"`
	Email       string `json:"email,omitempty"`
	Role        string `json:"role,omitempty"`        // "owner", "attendee", "chair"
	Status      string `json:"status,omitempty"`      // "needs-action", "accepted", "declined", "tentative"
	Kind        string `json:"kind,omitempty"`        // "individual", "group", "resource", "location"
	ExpectReply bool   `json:"expectReply,omitempty"`
}

// JSCalendarRecurrenceRule defines a recurrence rule object per RFC 8984 Section 4.3.1.
type JSCalendarRecurrenceRule struct {
	Frequency string   `json:"frequency"`       // "daily", "weekly", "monthly", "yearly"
	Interval  uint64   `json:"interval,omitempty"`
	Until     string   `json:"until,omitempty"` // RFC 3339 timestamp
	Count     uint64   `json:"count,omitempty"`
	ByDay     []string `json:"byDay,omitempty"` // "mo", "tu", "we", "th", "fr", "sa", "su"
}

// JSCalendarAlert defines an alert/alarm object per RFC 8984 Section 4.5.1.
type JSCalendarAlert struct {
	Trigger     string `json:"trigger"`          // ISO 8601 duration e.g. "-PT15M"
	Action      string `json:"action,omitempty"` // "display", "email"
	Description string `json:"description,omitempty"`
}

// CalendarEvent represents a JSCalendar Event object per RFC 8984 & JMAP for Calendars.
type CalendarEvent struct {
	ID              Id                                 `json:"id"`
	CalendarIDs     map[Id]bool                        `json:"calendarIds"`
	Type            string                             `json:"@type"` // Always "Event"
	Title           string                             `json:"title"`
	Description     string                             `json:"description,omitempty"`
	Start           string                             `json:"start"`
	Duration        string                             `json:"duration,omitempty"`
	TimeZone        string                             `json:"timeZone,omitempty"`
	Location        *JSCalendarLocation                `json:"location,omitempty"`
	Status          string                             `json:"status,omitempty"`         // "confirmed", "tentative", "cancelled"
	FreeBusyStatus  string                             `json:"freeBusyStatus,omitempty"` // "free", "busy", "tentative"
	Privacy         string                             `json:"privacy,omitempty"`        // "public", "private", "secret"
	Participants    map[string]*JSCalendarParticipant  `json:"participants,omitempty"`
	RecurrenceRules []*JSCalendarRecurrenceRule        `json:"recurrenceRules,omitempty"`
	Alerts          map[string]*JSCalendarAlert        `json:"alerts,omitempty"`
	Created         string                             `json:"created,omitempty"`
	Updated         string                             `json:"updated,omitempty"`
	Keywords        map[string]bool                    `json:"keywords,omitempty"`
}
