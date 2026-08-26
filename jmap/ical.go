package jmap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// icalTimeToRFC3339 converts an iCalendar date/date-time value (RFC 5545 Section 3.3.4/3.3.5)
// into an RFC 3339 string. Date-only values ("20260825") map to "2026-08-25". Local (floating)
// date-times keep no offset; UTC values gain the "Z" suffix; explicit offsets are preserved.
func icalTimeToRFC3339(value string, dateOnly bool) string {
	v := strings.TrimSpace(value)
	switch {
	case dateOnly:
		if len(v) == 8 && isAllDigits(v) {
			return v[:4] + "-" + v[4:6] + "-" + v[6:8]
		}
		return v
	case len(v) == 15 && v[8] == 'T' && isAllDigits(v[:8]) && isAllDigits(v[9:15]):
		// "YYYYMMDDTHHMMSS" floating local time
		return v[:4] + "-" + v[4:6] + "-" + v[6:8] + "T" + v[9:11] + ":" + v[11:13] + ":" + v[13:15]
	case len(v) == 16 && v[8] == 'T' && v[15] == 'Z' && isAllDigits(v[:8]) && isAllDigits(v[9:15]):
		// "YYYYMMDDTHHMMSSZ" UTC
		return v[:4] + "-" + v[4:6] + "-" + v[6:8] + "T" + v[9:11] + ":" + v[11:13] + ":" + v[13:15] + "Z"
	case len(v) == 20 && v[8] == 'T' && isAllDigits(v[:8]) && isAllDigits(v[9:15]):
		// "YYYYMMDDTHHMMSS+HHMM" / "-HHMM" explicit offset
		return v[:4] + "-" + v[4:6] + "-" + v[6:8] + "T" + v[9:11] + ":" + v[11:13] + ":" + v[13:15] +
			v[15:16] + v[16:18] + ":" + v[18:20]
	}
	return v
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// icalDurationToISODuration normalizes an iCalendar duration (RFC 5545 Section 3.3.6) into
// the ISO 8601 form used by JSCalendar: the syntaxes are identical, so only trailing
// whitespace and commas are cleaned up.
func icalDurationToISODuration(v string) string {
	return strings.ReplaceAll(strings.TrimSpace(v), ",", "")
}

// IcalDurationBetween computes an ISO 8601 duration from DTSTART to DTEND; on any parse
// failure it returns an empty string so the caller falls back to the stored DURATION.
func IcalDurationBetween(start, end string) string {
	return icalDurationBetween(start, end)
}

func icalDurationBetween(start, end string) string {
	sT, okS := parseRFC3339Time(start)
	eT, okE := parseRFC3339Time(end)
	if !okS || !okE || eT.Before(sT) {
		return ""
	}
	d := eT.Sub(sT)
	if d == 0 {
		return ""
	}
	return formatISODuration(d)
}

func formatISODuration(d time.Duration) string {
	if d == 0 {
		return "PT0S"
	}
	totalSecs := int64(d.Seconds())
	days := totalSecs / 86400
	remSecs := totalSecs % 86400
	hours := remSecs / 3600
	remSecs = remSecs % 3600
	mins := remSecs / 60
	secs := remSecs % 60

	if days > 0 && hours == 0 && mins == 0 && secs == 0 {
		if days%7 == 0 {
			return fmt.Sprintf("P%dW", days/7)
		}
		return fmt.Sprintf("P%dD", days)
	}

	var sb strings.Builder
	sb.WriteString("P")
	if days > 0 {
		fmt.Fprintf(&sb, "%dD", days)
	}
	if hours > 0 || mins > 0 || secs > 0 {
		sb.WriteString("T")
		if hours > 0 {
			fmt.Fprintf(&sb, "%dH", hours)
		}
		if mins > 0 {
			fmt.Fprintf(&sb, "%dM", mins)
		}
		if secs > 0 {
			fmt.Fprintf(&sb, "%dS", secs)
		}
	}
	return sb.String()
}

// parseRFC3339Time parses an RFC 3339 timestamp, tolerating full timestamps,
// local timestamps, fractional seconds, and date-only values.
func parseRFC3339Time(s string) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseRecurrenceRule converts an RFC 5545 RRULE value into a JSCalendar
// RecurrenceRule object per RFC 8984 Section 4.3.3.
func parseRecurrenceRule(value string) *JSCalendarRecurrenceRule {
	rule := &JSCalendarRecurrenceRule{Type: "RecurrenceRule"}
	for _, part := range strings.Split(value, ";") {
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(part[:eq]))
		val := strings.TrimSpace(part[eq+1:])
		switch key {
		case "FREQ":
			switch strings.ToUpper(val) {
			case "DAILY":
				rule.Frequency = "daily"
			case "WEEKLY":
				rule.Frequency = "weekly"
			case "MONTHLY":
				rule.Frequency = "monthly"
			case "YEARLY":
				rule.Frequency = "yearly"
			}
		case "INTERVAL":
			if n, err := strconv.ParseUint(val, 10, 64); err == nil {
				rule.Interval = n
			}
		case "COUNT":
			if n, err := strconv.ParseUint(val, 10, 64); err == nil {
				rule.Count = n
			}
		case "UNTIL":
			rule.Until = icalTimeToRFC3339(val, false)
		case "BYDAY":
			for _, day := range strings.Split(val, ",") {
				day = strings.ToLower(strings.TrimSpace(day))
				if day == "" {
					continue
				}
				nd := &NDay{Day: day}
				if len(day) > 2 {
					if n, err := strconv.Atoi(day[:len(day)-2]); err == nil {
						nd.Nth = n
						nd.Day = day[len(day)-2:]
					}
				}
				rule.ByDay = append(rule.ByDay, nd)
			}
		case "BYMONTHDAY":
			rule.ByMonthDay = parseIntList(val)
		case "BYMONTH":
			for _, m := range strings.Split(val, ",") {
				if m = strings.TrimSpace(m); m != "" {
					if n, err := strconv.Atoi(m); err == nil && n >= 1 && n <= 12 {
						rule.ByMonth = append(rule.ByMonth, fmt.Sprintf("%02d", n))
					} else {
						rule.ByMonth = append(rule.ByMonth, m)
					}
				}
			}
		case "BYYEARDAY":
			rule.ByYearDay = parseIntList(val)
		case "BYWEEKNO":
			rule.ByWeekNo = parseIntList(val)
		case "BYHOUR":
			rule.ByHour = parseUintList(val)
		case "BYMINUTE":
			rule.ByMinute = parseUintList(val)
		case "BYSECOND":
			rule.BySecond = parseUintList(val)
		case "BYSETPOS":
			rule.BySetPosition = parseIntList(val)
		case "WKST":
			rule.FirstDayOfWeek = strings.ToLower(val)
		}
	}
	return rule
}

func parseIntList(val string) []int {
	var out []int
	for _, part := range strings.Split(val, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func parseUintList(val string) []uint32 {
	var out []uint32
	for _, part := range strings.Split(val, ",") {
		if n, err := strconv.ParseUint(strings.TrimSpace(part), 10, 32); err == nil {
			out = append(out, uint32(n))
		}
	}
	return out
}

// icalParticipantKey extracts the address key (mailto: prefix removed) used for the
// participants map from an iCalendar cal-address value.
func icalParticipantKey(addr string) string {
	addr = strings.TrimSpace(addr)
	if lower := strings.ToLower(addr); strings.HasPrefix(lower, "mailto:") {
		return addr[len("mailto:"):]
	}
	return addr
}

// parseGoICalAttendee converts an ATTENDEE property into a JSCalendar participant.
func parseGoICalAttendee(prop ical.Prop, role string) *JSCalendarParticipant {
	p := &JSCalendarParticipant{
		Type:  "Participant",
		Email: icalParticipantKey(prop.Value),
	}
	switch strings.ToUpper(prop.Params.Get("ROLE")) {
	case "CHAIR":
		role = "chair"
	case "OPT-PARTICIPANT":
		role = "optional"
	case "NON-PARTICIPANT":
		role = "informational"
	case "REQ-PARTICIPANT":
		role = "attendee"
	}
	p.Roles = map[string]bool{role: true}
	if cn := prop.Params.Get("CN"); cn != "" {
		p.Name = cn
	}
	switch partStat := strings.ToUpper(prop.Params.Get("PARTSTAT")); partStat {
	case "ACCEPTED":
		p.ParticipationStatus = "accepted"
	case "DECLINED":
		p.ParticipationStatus = "declined"
	case "TENTATIVE":
		p.ParticipationStatus = "tentative"
	case "DELEGATED":
		p.ParticipationStatus = "delegated"
	case "COMPLETED", "NEEDS-ACTION":
		p.ParticipationStatus = "needs-action"
	}
	switch cutype := strings.ToUpper(prop.Params.Get("CUTYPE")); cutype {
	case "GROUP":
		p.Kind = "group"
	case "RESOURCE":
		p.Kind = "resource"
	case "ROOM":
		p.Kind = "location"
	}
	if strings.EqualFold(prop.Params.Get("RSVP"), "TRUE") {
		p.ExpectReply = true
	}
	if to := prop.Params.Get("DELEGATED-TO"); to != "" {
		p.DelegatedTo = map[string]bool{icalParticipantKey(to): true}
	}
	if from := prop.Params.Get("DELEGATED-FROM"); from != "" {
		p.DelegatedFrom = map[string]bool{icalParticipantKey(from): true}
	}
	if member := prop.Params.Get("MEMBER"); member != "" {
		p.MemberOf = map[string]bool{icalParticipantKey(member): true}
	}
	p.Role = role
	p.Status = p.ParticipationStatus
	return p
}

// ParseICalendar parses an iCalendar (RFC 5545) data stream into JSCalendar
// CalendarEvent objects using github.com/emersion/go-ical, following the conversion
// described by draft-ietf-calext-jscalendar-icalendar for the properties a calendar client needs.
func ParseICalendar(data []byte) ([]*CalendarEvent, error) {
	dec := ical.NewDecoder(bytes.NewReader(data))
	cal, err := dec.Decode()
	if err != nil {
		return nil, fmt.Errorf("not a valid iCalendar stream: %w", err)
	}

	var method, prodID string
	if prop := cal.Props.Get(ical.PropMethod); prop != nil {
		method = strings.ToLower(prop.Value)
	}
	if prop := cal.Props.Get(ical.PropProductID); prop != nil {
		prodID = prop.Value
	}

	var events []*CalendarEvent
	var walk func(comp *ical.Component)
	walk = func(comp *ical.Component) {
		for _, child := range comp.Children {
			if child.Name == ical.CompEvent {
				if ev := icalComponentToCalendarEvent(child, method, prodID); ev != nil {
					events = append(events, ev)
				}
			}
			walk(child)
		}
	}
	walk(cal.Component)

	if len(events) == 0 {
		return nil, fmt.Errorf("iCalendar stream contains no VEVENT components")
	}
	return events, nil
}

// icalComponentToCalendarEvent converts a parsed VEVENT component into a CalendarEvent.
func icalComponentToCalendarEvent(comp *ical.Component, method, prodID string) *CalendarEvent {
	ev := &CalendarEvent{
		Type:                   "Event",
		DescriptionContentType: "text/plain",
		Method:                 method,
		ProdID:                 prodID,
	}

	var start, end string

	for name, propList := range comp.Props {
		for _, p := range propList {
			switch strings.ToUpper(name) {
			case ical.PropUID:
				ev.UID = strings.TrimSpace(p.Value)
			case ical.PropSummary:
				if text, err := p.Text(); err == nil {
					ev.Title = text
				} else {
					ev.Title = unescapeICalText(p.Value)
				}
			case ical.PropDescription:
				if text, err := p.Text(); err == nil {
					ev.Description = text
				} else {
					ev.Description = unescapeICalText(p.Value)
				}
			case ical.PropDateTimeStart:
				rawVal := strings.TrimSpace(p.Value)
				tzID := p.Params.Get("TZID")
				startDateOnly := strings.EqualFold(p.Params.Get("VALUE"), "DATE")
				if startDateOnly {
					start = icalTimeToRFC3339(rawVal, true)
					ev.ShowWithoutTime = true
				} else {
					start = icalTimeToRFC3339(rawVal, false)
				}
				ev.Start = start
				if tzID != "" {
					ev.TimeZone = tzID
				} else if strings.HasSuffix(rawVal, "Z") {
					ev.TimeZone = "Etc/UTC"
				}
			case ical.PropDateTimeEnd:
				rawVal := strings.TrimSpace(p.Value)
				dateOnly := strings.EqualFold(p.Params.Get("VALUE"), "DATE")
				end = icalTimeToRFC3339(rawVal, dateOnly)
			case ical.PropDuration:
				if dur, err := p.Duration(); err == nil && dur > 0 {
					ev.Duration = formatISODuration(dur)
				} else {
					ev.Duration = icalDurationToISODuration(p.Value)
				}
			case ical.PropStatus:
				switch strings.ToUpper(strings.TrimSpace(p.Value)) {
				case "CONFIRMED":
					ev.Status = "confirmed"
				case "TENTATIVE":
					ev.Status = "tentative"
				case "CANCELLED", "CANCELED":
					ev.Status = "cancelled"
				}
			case "TRANSP":
				if strings.EqualFold(strings.TrimSpace(p.Value), "TRANSPARENT") {
					ev.FreeBusyStatus = "free"
				} else {
					ev.FreeBusyStatus = "busy"
				}
			case ical.PropClass:
				switch strings.ToUpper(strings.TrimSpace(p.Value)) {
				case "PRIVATE":
					ev.Privacy = "private"
				case "CONFIDENTIAL":
					ev.Privacy = "secret"
				default:
					ev.Privacy = "public"
				}
			case ical.PropLocation:
				if p.Value != "" {
					locName, err := p.Text()
					if err != nil {
						locName = unescapeICalText(p.Value)
					}
					if ev.Locations == nil {
						ev.Locations = make(map[string]*JSCalendarLocation)
					}
					hash := sha256.Sum256([]byte(locName))
					key := hex.EncodeToString(hash[:])[:40]
					ev.Locations[key] = &JSCalendarLocation{
						Type: "Location",
						Name: locName,
					}
				}
			case "GEO":
				if idx := strings.IndexByte(p.Value, ';'); idx > 0 {
					ev.Locations = ensureLocations(ev.Locations)
					for _, loc := range ev.Locations {
						loc.Coordinates = p.Value
					}
				}
			case ical.PropOrganizer:
				email := icalParticipantKey(p.Value)
				if email != "" {
					if ev.Participants == nil {
						ev.Participants = make(map[string]*JSCalendarParticipant)
					}
					participant := &JSCalendarParticipant{
						Type:  "Participant",
						Email: email,
						Roles: map[string]bool{"owner": true},
						Role:  "owner",
					}
					if cn := p.Params.Get("CN"); cn != "" {
						participant.Name = cn
					}
					ev.Participants[email] = participant
				}
			case ical.PropAttendee:
				email := icalParticipantKey(p.Value)
				if email != "" {
					if ev.Participants == nil {
						ev.Participants = make(map[string]*JSCalendarParticipant)
					}
					ev.Participants[email] = parseGoICalAttendee(p, "attendee")
				}
			case ical.PropRecurrenceRule:
				if rule := parseRecurrenceRule(p.Value); rule != nil && rule.Frequency != "" {
					ev.RecurrenceRules = append(ev.RecurrenceRules, rule)
				}
			case "EXRULE":
				if rule := parseRecurrenceRule(p.Value); rule != nil && rule.Frequency != "" {
					ev.ExcludedRecurrenceRules = append(ev.ExcludedRecurrenceRules, rule)
				}
			case ical.PropPriority:
				if n, err := strconv.ParseUint(strings.TrimSpace(p.Value), 10, 32); err == nil {
					ev.Priority = uint32(n)
				}
			case "COLOR":
				ev.Color = strings.TrimSpace(p.Value)
			case "ATTACH":
				if p.Value != "" {
					if ev.Links == nil {
						ev.Links = make(map[string]*JSCalendarLink)
					}
					hash := sha256.Sum256([]byte(p.Value))
					key := hex.EncodeToString(hash[:])[:40]
					ev.Links[key] = &JSCalendarLink{Type: "Link", Href: p.Value}
				}
			case ical.PropExceptionDates:
				for _, ex := range strings.Split(p.Value, ",") {
					if ex = strings.TrimSpace(ex); ex == "" {
						continue
					}
					dateOnly := strings.EqualFold(p.Params.Get("VALUE"), "DATE")
					if ev.Excluded == nil {
						ev.Excluded = make(map[string]bool)
					}
					ev.Excluded[icalTimeToRFC3339(ex, dateOnly)] = true
				}
			case ical.PropRecurrenceID:
				dateOnly := strings.EqualFold(p.Params.Get("VALUE"), "DATE")
				ev.RecurrenceID = icalTimeToRFC3339(p.Value, dateOnly)
			case ical.PropCategories:
				if ev.Categories == nil {
					ev.Categories = make(map[string]bool)
				}
				for _, cat := range strings.Split(p.Value, ",") {
					if cat = strings.TrimSpace(unescapeICalText(cat)); cat != "" {
						ev.Categories[cat] = true
					}
				}
			case ical.PropSequence:
				if n, err := strconv.ParseUint(strings.TrimSpace(p.Value), 10, 32); err == nil {
					ev.Sequence = uint32(n)
				}
			case ical.PropCreated:
				ev.Created = icalTimeToRFC3339(p.Value, false)
			case ical.PropLastModified:
				ev.Updated = icalTimeToRFC3339(p.Value, false)
			case ical.PropDateTimeStamp:
				if ev.Updated == "" {
					ev.Updated = icalTimeToRFC3339(p.Value, false)
				}
			}
		}
	}

	for i, child := range comp.Children {
		if child.Name != ical.CompAlarm {
			continue
		}
		if alert := parseGoICalAlarm(child); alert != nil {
			if ev.Alerts == nil {
				ev.Alerts = make(map[string]*JSCalendarAlert)
			}
			ev.Alerts[fmt.Sprintf("alert-%d", i+1)] = alert
		}
	}

	if ev.UID == "" {
		ev.UID = string(ev.ID)
	}
	if ev.Duration == "" && start != "" && end != "" {
		ev.Duration = icalDurationBetween(start, end)
	}
	if ev.Updated == "" {
		ev.Updated = ev.Created
	}
	return ev
}

// parseGoICalAlarm converts a VALARM component into a JSCalendar Alert.
func parseGoICalAlarm(comp *ical.Component) *JSCalendarAlert {
	alert := &JSCalendarAlert{Type: "Alert", Action: "display"}
	for name, props := range comp.Props {
		for _, p := range props {
			switch strings.ToUpper(name) {
			case ical.PropAction:
				if strings.EqualFold(p.Value, "EMAIL") {
					alert.Action = "email"
				} else {
					alert.Action = "display"
				}
			case ical.PropTrigger:
				if strings.EqualFold(p.Params.Get("VALUE"), "DATE-TIME") {
					alert.Trigger = map[string]any{
						"@type": "AbsoluteTrigger",
						"when":  icalTimeToRFC3339(p.Value, false),
					}
				} else {
					trigger := map[string]any{"@type": "OffsetTrigger", "offset": strings.TrimSpace(p.Value)}
					if strings.EqualFold(p.Params.Get("RELATED"), "END") {
						trigger["relativeTo"] = "end"
					}
					alert.Trigger = trigger
				}
			case ical.PropDescription:
				if text, err := p.Text(); err == nil {
					alert.Description = text
				} else {
					alert.Description = unescapeICalText(p.Value)
				}
			}
		}
	}
	return alert
}

func ensureLocations(m map[string]*JSCalendarLocation) map[string]*JSCalendarLocation {
	if m == nil {
		return make(map[string]*JSCalendarLocation)
	}
	return m
}
