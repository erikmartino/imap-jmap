package jmap

import (
	"encoding/json"
)

// CalendarRights defines access rights for a Calendar per JMAP for Calendars.
type CalendarRights struct {
	MayReadFreeBusy  bool `json:"mayReadFreeBusy"`
	MayReadItems     bool `json:"mayReadItems"`
	MayWriteAll      bool `json:"mayWriteAll"`
	MayWriteOwn      bool `json:"mayWriteOwn"`
	MayUpdatePrivate bool `json:"mayUpdatePrivate"`
	MayRSVP          bool `json:"mayRSVP"`
	MayDelete        bool `json:"mayDelete"`
	MayShare         bool `json:"mayShare"`
}

func FullCalendarRights() CalendarRights {
	return CalendarRights{
		MayReadFreeBusy:  true,
		MayReadItems:     true,
		MayWriteAll:      true,
		MayWriteOwn:      true,
		MayUpdatePrivate: true,
		MayRSVP:          true,
		MayDelete:        true,
		MayShare:         true,
	}
}

// EnforceInvariants ensures that mayWriteAll implies mayWriteOwn, mayUpdatePrivate, and mayRSVP.
func (r *CalendarRights) EnforceInvariants() {
	if r.MayWriteAll {
		r.MayWriteOwn = true
		r.MayUpdatePrivate = true
		r.MayRSVP = true
	}
}

type CalendarShare struct {
	Rights CalendarRights `json:"rights"`
}

// Calendar represents a Calendar object per JMAP for Calendars.
type Calendar struct {
	ID                       Id                          `json:"id"`
	Name                     string                      `json:"name"`
	Description              *string                     `json:"description,omitempty"`
	Color                    *string                     `json:"color,omitempty"`
	SortOrder                uint64                      `json:"sortOrder"`
	IsDefault                bool                        `json:"isDefault"`
	IsVisible                bool                        `json:"isVisible"`
	IsSubscribed             bool                        `json:"isSubscribed"`
	IncludeInAvailability    string                      `json:"includeInAvailability,omitempty"`
	DefaultAlertsWithTime    map[string]*JSCalendarAlert `json:"defaultAlertsWithTime,omitempty"`
	DefaultAlertsWithoutTime map[string]*JSCalendarAlert `json:"defaultAlertsWithoutTime,omitempty"`
	TimeZone                 string                      `json:"timeZone,omitempty"`
	ShareWith                map[string]*CalendarShare   `json:"shareWith,omitempty"`
	MyRights                 CalendarRights              `json:"myRights"`
}

// NDay represents a day of the week with optional nth-occurrence per RFC 8984 Section 4.3.3.
type NDay struct {
	Day string `json:"day"` // "mo", "tu", "we", "th", "fr", "sa", "su"
	Nth int    `json:"nth,omitempty"`
}

func (n *NDay) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		n.Day = s
		return nil
	}
	type rawNDay NDay
	var raw rawNDay
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*n = NDay(raw)
	return nil
}

// OffsetTrigger defines an offset trigger object per RFC 8984 Section 4.5.2.
type OffsetTrigger struct {
	Type       string `json:"@type"` // "OffsetTrigger"
	Offset     string `json:"offset"`
	RelativeTo string `json:"relativeTo,omitempty"` // "start", "end"
}

// AbsoluteTrigger defines an absolute trigger object per RFC 8984 Section 4.5.2.
type AbsoluteTrigger struct {
	Type string `json:"@type"` // "AbsoluteTrigger"
	When string `json:"when"`
}

// JSCalendarLocation defines a location object per RFC 8984 Section 4.2.5.
type JSCalendarLocation struct {
	Type          string                     `json:"@type,omitempty"` // "Location"
	Name          string                     `json:"name,omitempty"`
	Description   string                     `json:"description,omitempty"`
	Rel           string                     `json:"rel,omitempty"`
	TimeZone      string                     `json:"timeZone,omitempty"`
	LocationTypes map[string]bool            `json:"locationTypes,omitempty"`
	RelativeTo    string                     `json:"relativeTo,omitempty"`
	Coordinates   string                     `json:"coordinates,omitempty"`
	Links         map[string]*JSCalendarLink `json:"links,omitempty"`
}

// JSCalendarParticipant defines a participant object per RFC 8984 Section 4.4.6.
type JSCalendarParticipant struct {
	Type                string                     `json:"@type,omitempty"` // "Participant"
	Name                string                     `json:"name,omitempty"`
	Email               string                     `json:"email,omitempty"`
	Role                string                     `json:"role,omitempty"`                // Deprecated/compat: "owner", "attendee", "chair"
	Roles               map[string]bool            `json:"roles,omitempty"`               // "owner", "attendee", "chair"
	Status              string                     `json:"status,omitempty"`              // Deprecated/compat: "needs-action", "accepted", "declined", "tentative"
	ParticipationStatus string                     `json:"participationStatus,omitempty"` // "needs-action", "accepted", "declined", "tentative"
	Kind                string                     `json:"kind,omitempty"`                // "individual", "group", "resource", "location"
	ExpectReply         bool                       `json:"expectReply,omitempty"`
	SendTo              map[string]string          `json:"sendTo,omitempty"`
	DelegatedTo         map[string]bool            `json:"delegatedTo,omitempty"`
	DelegatedFrom       map[string]bool            `json:"delegatedFrom,omitempty"`
	MemberOf            map[string]bool            `json:"memberOf,omitempty"`
	ScheduleAgent       string                     `json:"scheduleAgent,omitempty"`
	ScheduleStatus      string                     `json:"scheduleStatus,omitempty"`
	InvitedBy           string                     `json:"invitedBy,omitempty"`
	Links               map[string]*JSCalendarLink `json:"links,omitempty"`
	Language            string                     `json:"language,omitempty"`
	LocationID          string                     `json:"locationId,omitempty"`
}

func (p *JSCalendarParticipant) UnmarshalJSON(data []byte) error {
	type rawParticipant JSCalendarParticipant
	var raw rawParticipant
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = JSCalendarParticipant(raw)
	if p.Roles == nil && p.Role != "" {
		p.Roles = map[string]bool{p.Role: true}
	}
	if p.Role == "" && len(p.Roles) > 0 {
		for r := range p.Roles {
			p.Role = r
			break
		}
	}
	if p.ParticipationStatus == "" && p.Status != "" {
		p.ParticipationStatus = p.Status
	}
	if p.Status == "" && p.ParticipationStatus != "" {
		p.Status = p.ParticipationStatus
	}
	if p.Type == "" {
		p.Type = "Participant"
	}
	return nil
}

// JSCalendarRecurrenceRule defines a recurrence rule object per RFC 8984 Section 4.3.3.
type JSCalendarRecurrenceRule struct {
	Type           string   `json:"@type,omitempty"` // "RecurrenceRule"
	Frequency      string   `json:"frequency"`       // "daily", "weekly", "monthly", "yearly"
	Interval       uint64   `json:"interval,omitempty"`
	RScale         string   `json:"rscale,omitempty"`
	Skip           string   `json:"skip,omitempty"`
	FirstDayOfWeek string   `json:"firstDayOfWeek,omitempty"`
	Until          string   `json:"until,omitempty"` // RFC 3339 timestamp
	Count          uint64   `json:"count,omitempty"`
	ByDay          []*NDay  `json:"byDay,omitempty"`
	ByMonthDay     []int    `json:"byMonthDay,omitempty"`
	ByMonth        []string `json:"byMonth,omitempty"`
	ByYearDay      []int    `json:"byYearDay,omitempty"`
	ByWeekNo       []int    `json:"byWeekNo,omitempty"`
	ByHour         []uint32 `json:"byHour,omitempty"`
	ByMinute       []uint32 `json:"byMinute,omitempty"`
	BySecond       []uint32 `json:"bySecond,omitempty"`
	BySetPosition  []int    `json:"bySetPosition,omitempty"`
}

// JSCalendarAlert defines an alert/alarm object per RFC 8984 Section 4.5.2.
type JSCalendarAlert struct {
	Type         string                         `json:"@type,omitempty"` // "Alert"
	Trigger      any                            `json:"trigger"`         // OffsetTrigger, AbsoluteTrigger, or string (ISO 8601 duration)
	Action       string                         `json:"action,omitempty"`// "display", "email"
	Description  string                         `json:"description,omitempty"`
	Acknowledged string                         `json:"acknowledged,omitempty"`
	RelatedTo    map[string]*JSCalendarRelation `json:"relatedTo,omitempty"`
}

func (a *JSCalendarAlert) UnmarshalJSON(data []byte) error {
	type rawAlert JSCalendarAlert
	var raw struct {
		rawAlert
		TriggerRaw json.RawMessage `json:"trigger"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*a = JSCalendarAlert(raw.rawAlert)
	if len(raw.TriggerRaw) > 0 {
		var s string
		if err := json.Unmarshal(raw.TriggerRaw, &s); err == nil {
			a.Trigger = map[string]any{
				"@type":  "OffsetTrigger",
				"offset": s,
			}
		} else {
			var m map[string]any
			if err := json.Unmarshal(raw.TriggerRaw, &m); err == nil {
				a.Trigger = m
			}
		}
	}
	if a.Type == "" {
		a.Type = "Alert"
	}
	return nil
}

// JSCalendarLink defines a link or attachment object per RFC 8984 Section 4.2.7.
type JSCalendarLink struct {
	Type        string `json:"@type,omitempty"` // "Link"
	Href        string `json:"href"`
	Cid         string `json:"cid,omitempty"`
	Rel         string `json:"rel,omitempty"`         // "enclosure", "describedby", etc.
	ContentType string `json:"contentType,omitempty"` // Content type e.g. "application/pdf"
	Size        uint64 `json:"size,omitempty"`
	Display     string `json:"display,omitempty"`
	Title       string `json:"title,omitempty"`
}

// JSCalendarVirtualLocation defines a virtual location object per RFC 8984 Section 4.2.6.
type JSCalendarVirtualLocation struct {
	Type        string          `json:"@type,omitempty"` // "VirtualLocation"
	URI         string          `json:"uri"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Features    map[string]bool `json:"features,omitempty"`
}

// JSCalendarRelation defines a relation object per RFC 8984 Section 4.1.3.
type JSCalendarRelation struct {
	Relation map[string]bool `json:"relation,omitempty"`
}

// JSCalendarTimeZone defines a timeZone property object per RFC 8984 Section 4.7.2.
type JSCalendarTimeZone struct {
	TZID string `json:"tzId,omitempty"`
}

// CalendarEvent represents a JSCalendar Event object per RFC 8984 & JMAP for Calendars.
type CalendarEvent struct {
	ID                     Id                                    `json:"id"`
	CalendarIDs            map[Id]bool                           `json:"calendarIds"`
	Type                   string                                `json:"@type"` // Always "Event"
	Title                  string                                `json:"title"`
	Description            string                                `json:"description,omitempty"`
	DescriptionContentType string                                `json:"descriptionContentType,omitempty"`
	ShowWithoutTime        bool                                  `json:"showWithoutTime"`
	Start                  string                                `json:"start"`
	Duration               string                                `json:"duration,omitempty"`
	TimeZone               string                                `json:"timeZone,omitempty"`
	Locations              map[string]*JSCalendarLocation        `json:"locations,omitempty"`
	VirtualLocations       map[string]*JSCalendarVirtualLocation `json:"virtualLocations,omitempty"`
	Links                  map[string]*JSCalendarLink            `json:"links,omitempty"`
	Locale                 string                                `json:"locale,omitempty"`
	Categories             map[string]bool                       `json:"categories,omitempty"`
	Color                  string                                `json:"color,omitempty"`
	Status                 string                                `json:"status,omitempty"`         // "confirmed", "tentative", "cancelled"
	FreeBusyStatus         string                                `json:"freeBusyStatus,omitempty"` // "free", "busy", "tentative"
	Privacy                string                                `json:"privacy,omitempty"`        // "public", "private", "secret"
	Priority               uint32                                `json:"priority,omitempty"`
	ReplyTo                map[string]string                     `json:"replyTo,omitempty"`
	SentBy                 string                                `json:"sentBy,omitempty"`
	RequestStatus          string                                `json:"requestStatus,omitempty"`
	UseDefaultAlerts       bool                                  `json:"useDefaultAlerts"`
	Localizations          map[string]map[string]any             `json:"localizations,omitempty"`
	TimeZones              map[string]*JSCalendarTimeZone        `json:"timeZones,omitempty"`
	Participants           map[string]*JSCalendarParticipant     `json:"participants,omitempty"`
	RecurrenceRules         []*JSCalendarRecurrenceRule           `json:"recurrenceRules,omitempty"`
	RecurrenceID            string                                `json:"recurrenceId,omitempty"`
	RecurrenceIDTimeZone    string                                `json:"recurrenceIdTimeZone,omitempty"`
	ExcludedRecurrenceRules []*JSCalendarRecurrenceRule           `json:"excludedRecurrenceRules,omitempty"`
	RecurrenceOverrides     map[string]map[string]any             `json:"recurrenceOverrides,omitempty"`
	Excluded                map[string]bool                       `json:"excluded,omitempty"`
	Alerts                  map[string]*JSCalendarAlert           `json:"alerts,omitempty"`
	RelatedTo              map[string]*JSCalendarRelation        `json:"relatedTo,omitempty"`
	ProdID                 string                                `json:"prodId,omitempty"`
	Sequence               uint32                                `json:"sequence,omitempty"`
	Method                 string                                `json:"method,omitempty"`
	Due                    string                                `json:"due,omitempty"`
	EstimatedDuration      string                                `json:"estimatedDuration,omitempty"`
	PercentComplete        uint32                                `json:"percentComplete,omitempty"`
	Progress               string                                `json:"progress,omitempty"`
	ProgressUpdated        string                                `json:"progressUpdated,omitempty"`
	Entries                map[string]map[string]any             `json:"entries,omitempty"`
	Source                 string                                `json:"source,omitempty"`
	Created                string                                `json:"created,omitempty"`
	Updated                string                                `json:"updated,omitempty"`
	UID                    string                                `json:"uid,omitempty"`
	Keywords               map[string]bool                       `json:"keywords,omitempty"`
}
