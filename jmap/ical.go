package jmap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// icalComponent is a parsed iCalendar (RFC 5545) component with its properties and
// nested components, used by ParseICalendar to convert iCalendar data to JSCalendar.
type icalComponent struct {
	name       string
	properties []icalProperty
	children   []*icalComponent
}

// icalProperty is a single unfolded iCalendar property line (RFC 5545 Section 3.1):
// name, parameters (e.g. "TZID=Europe/Paris", "CN=Joe", "VALUE=DATE"), and the value.
type icalProperty struct {
	name   string
	params map[string]string
	value  string
}

// icalParamValue extracts a parameter value, unwrapping surrounding double quotes.
func icalParamValue(params map[string]string, name string) string {
	v := params[name]
	return strings.Trim(v, `"`)
}

// parseICalComponent parses the unfolded lines of an iCalendar stream between a
// BEGIN:<name> line and its matching END:<name> line, returning the component and
// the index of the line after the END line.
func parseICalComponent(lines []string, start int) (*icalComponent, int) {
	if start >= len(lines) {
		return nil, start
	}
	comp := &icalComponent{}
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "BEGIN:") {
			child, next := parseICalComponent(lines, i+1)
			if child != nil {
				comp.children = append(comp.children, child)
			}
			i = next - 1
			continue
		}
		if strings.HasPrefix(line, "END:") {
			comp.name = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(line, "END:")))
			return comp, i + 1
		}
		prop, ok := parseICalProperty(line)
		if ok {
			comp.properties = append(comp.properties, prop)
		}
	}
	return comp, len(lines)
}

// parseICalProperty parses a single property line into name, parameters, and value.
func parseICalProperty(line string) (icalProperty, bool) {
	idx := strings.IndexByte(line, ':')
	if idx <= 0 {
		return icalProperty{}, false
	}
	head := line[:idx]
	value := line[idx+1:]
	name := head
	params := make(map[string]string)
	if semi := strings.IndexByte(head, ';'); semi >= 0 {
		name = head[:semi]
		for _, paramPart := range strings.Split(head[semi+1:], ";") {
			paramPart = strings.TrimSpace(paramPart)
			if paramPart == "" {
				continue
			}
			if eq := strings.IndexByte(paramPart, '='); eq >= 0 {
				pName := strings.ToUpper(strings.TrimSpace(paramPart[:eq]))
				pVal := strings.TrimSpace(paramPart[eq+1:])
				params[pName] = pVal
			} else {
				params[strings.ToUpper(paramPart)] = ""
			}
		}
	}
	return icalProperty{name: strings.ToUpper(name), params: params, value: value}, true
}

// unfoldICalLines unfolds RFC 5545 Section 3.1 continuation lines: a line beginning
// with a space or horizontal tab is a continuation of the previous logical line.
func unfoldICalLines(data []byte) []string {
	raw := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSuffix(line, "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

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

// icalDurationBetween computes an ISO 8601 duration from DTSTART to DTEND; on any parse
// failure it returns an empty string so the caller falls back to the stored DURATION.
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
	secs := int64(d.Seconds())
	if secs%60 != 0 {
		return fmt.Sprintf("PT%dS", secs)
	}
	mins := secs / 60
	if mins%60 != 0 {
		return fmt.Sprintf("PT%dM", mins)
	}
	hours := mins / 60
	if hours%24 != 0 {
		return fmt.Sprintf("PT%dH", hours)
	}
	days := hours / 24
	if days%7 != 0 {
		return fmt.Sprintf("P%dD", days)
	}
	return fmt.Sprintf("P%dW", days/7)
}

// parseRFC3339Time parses an RFC 3339 timestamp, tolerating both full timestamps and
// date-only values.
func parseRFC3339Time(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, true
	}
	t, err = time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
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

// parseICalAttendee converts an ATTENDEE property line into a JSCalendar participant,
// mapping ROLE, PARTSTAT, CUTYPE, RSVP, and DELEGATED-TO/FROM / MEMBER parameters
// (RFC 5545 Section 3.8.4.1) back to their JSCalendar equivalents (RFC 8984 Section 4.4.6).
func parseICalAttendee(prop icalProperty, role string) *JSCalendarParticipant {
	p := &JSCalendarParticipant{
		Type:  "Participant",
		Email: icalParticipantKey(prop.value),
	}
	// ROLE parameter overrides the caller's default role.
	switch strings.ToUpper(icalParamValue(prop.params, "ROLE")) {
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
	if cn := icalParamValue(prop.params, "CN"); cn != "" {
		p.Name = cn
	}
	switch partStat := strings.ToUpper(icalParamValue(prop.params, "PARTSTAT")); partStat {
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
	switch cutype := strings.ToUpper(icalParamValue(prop.params, "CUTYPE")); cutype {
	case "GROUP":
		p.Kind = "group"
	case "RESOURCE":
		p.Kind = "resource"
	case "ROOM":
		p.Kind = "location"
	}
	if strings.EqualFold(icalParamValue(prop.params, "RSVP"), "TRUE") {
		p.ExpectReply = true
	}
	if to := icalParamValue(prop.params, "DELEGATED-TO"); to != "" {
		p.DelegatedTo = map[string]bool{icalParticipantKey(to): true}
	}
	if from := icalParamValue(prop.params, "DELEGATED-FROM"); from != "" {
		p.DelegatedFrom = map[string]bool{icalParticipantKey(from): true}
	}
	if member := icalParamValue(prop.params, "MEMBER"); member != "" {
		p.MemberOf = map[string]bool{icalParticipantKey(member): true}
	}
	p.Role = role
	p.Status = p.ParticipationStatus
	return p
}

// ParseICalendar parses an iCalendar (RFC 5545) data stream into JSCalendar
// CalendarEvent objects, following the conversion described by
// draft-ietf-calext-jscalendar-icalendar for the properties a calendar client needs.
// It returns an error when the stream is not a valid VCALENDAR or contains no VEVENT.
func ParseICalendar(data []byte) ([]*CalendarEvent, error) {
	lines := unfoldICalLines(data)
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty iCalendar data")
	}

	cal, next := parseICalComponent(lines, 0)
	if cal == nil {
		return nil, fmt.Errorf("not a valid iCalendar stream: missing BEGIN:VCALENDAR")
	}
	// The root component is anonymous when the stream begins with BEGIN:VCALENDAR;
	// accept either the root itself or a direct child named VCALENDAR.
	if cal.name != "VCALENDAR" {
		found := false
		for _, child := range cal.children {
			if child.name == "VCALENDAR" {
				cal = child
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("not a valid iCalendar stream: missing BEGIN:VCALENDAR")
		}
	}

	var method, prodID string
	for _, p := range cal.properties {
		switch p.name {
		case "METHOD":
			method = strings.ToLower(p.value)
		case "PRODID":
			prodID = p.value
		}
	}

	var events []*CalendarEvent
	var walk func(comp *icalComponent)
	walk = func(comp *icalComponent) {
		for _, child := range comp.children {
			if child.name == "VEVENT" {
				if ev := icalEventToCalendarEvent(child, method, prodID); ev != nil {
					events = append(events, ev)
				}
			}
			walk(child)
		}
	}
	walk(cal)
	_ = next

	if len(events) == 0 {
		return nil, fmt.Errorf("iCalendar stream contains no VEVENT components")
	}
	return events, nil
}

// icalEventToCalendarEvent converts a parsed VEVENT component into a CalendarEvent.
func icalEventToCalendarEvent(comp *icalComponent, method, prodID string) *CalendarEvent {
	ev := &CalendarEvent{
		Type:                   "Event",
		DescriptionContentType: "text/plain",
		Method:                 method,
		ProdID:                 prodID,
	}

	var start, end string
	var startDateOnly bool

	for _, p := range comp.properties {
		switch p.name {
		case "UID":
			ev.UID = strings.TrimSpace(p.value)
		case "SUMMARY":
			ev.Title = unescapeICalText(p.value)
		case "DESCRIPTION":
			ev.Description = unescapeICalText(p.value)
		case "DTSTART":
			start = icalTimeToRFC3339(p.value, false)
			startDateOnly = strings.EqualFold(icalParamValue(p.params, "VALUE"), "DATE")
			if startDateOnly {
				start = icalTimeToRFC3339(p.value, true)
				ev.ShowWithoutTime = true
			}
			ev.Start = start
			if tz := icalParamValue(p.params, "TZID"); tz != "" {
				ev.TimeZone = tz
			}
		case "DTEND":
			end = icalTimeToRFC3339(p.value, strings.EqualFold(icalParamValue(p.params, "VALUE"), "DATE"))
		case "DURATION":
			ev.Duration = icalDurationToISODuration(p.value)
		case "STATUS":
			switch strings.ToUpper(p.value) {
			case "CONFIRMED":
				ev.Status = "confirmed"
			case "TENTATIVE":
				ev.Status = "tentative"
			case "CANCELLED", "CANCELED":
				ev.Status = "cancelled"
			}
		case "TRANSP":
			if strings.EqualFold(p.value, "TRANSPARENT") {
				ev.FreeBusyStatus = "free"
			} else {
				ev.FreeBusyStatus = "busy"
			}
		case "CLASS":
			switch strings.ToUpper(p.value) {
			case "PRIVATE":
				ev.Privacy = "private"
			case "CONFIDENTIAL":
				ev.Privacy = "secret"
			default:
				ev.Privacy = "public"
			}
		case "LOCATION":
			if p.value != "" {
				if ev.Locations == nil {
					ev.Locations = make(map[string]*JSCalendarLocation)
				}
				name := unescapeICalText(p.value)
				hash := sha256.Sum256([]byte(name))
				key := hex.EncodeToString(hash[:])[:40]
				ev.Locations[key] = &JSCalendarLocation{
					Type: "Location",
					Name: name,
				}
			}
		case "GEO":
			if idx := strings.IndexByte(p.value, ';'); idx > 0 {
				ev.Locations = ensureLocations(ev.Locations)
				if len(ev.Locations) > 0 {
					for _, loc := range ev.Locations {
						loc.Coordinates = p.value
					}
				}
			}
		case "ORGANIZER":
			email := icalParticipantKey(p.value)
			if email == "" {
				continue
			}
			if ev.Participants == nil {
				ev.Participants = make(map[string]*JSCalendarParticipant)
			}
			participant := &JSCalendarParticipant{
				Type:  "Participant",
				Email: email,
				Roles: map[string]bool{"owner": true},
				Role:  "owner",
			}
			if cn := icalParamValue(p.params, "CN"); cn != "" {
				participant.Name = cn
			}
			ev.Participants[email] = participant
		case "ATTENDEE":
			email := icalParticipantKey(p.value)
			if email == "" {
				continue
			}
			if ev.Participants == nil {
				ev.Participants = make(map[string]*JSCalendarParticipant)
			}
			ev.Participants[email] = parseICalAttendee(p, "attendee")
		case "RRULE":
			if rule := parseRecurrenceRule(p.value); rule != nil && rule.Frequency != "" {
				ev.RecurrenceRules = append(ev.RecurrenceRules, rule)
			}
		case "EXRULE":
			if rule := parseRecurrenceRule(p.value); rule != nil && rule.Frequency != "" {
				ev.ExcludedRecurrenceRules = append(ev.ExcludedRecurrenceRules, rule)
			}
		case "PRIORITY":
			if n, err := strconv.ParseUint(strings.TrimSpace(p.value), 10, 32); err == nil {
				ev.Priority = uint32(n)
			}
		case "COLOR":
			ev.Color = strings.TrimSpace(p.value)
		case "ATTACH":
			if p.value != "" {
				if ev.Links == nil {
					ev.Links = make(map[string]*JSCalendarLink)
				}
				hash := sha256.Sum256([]byte(p.value))
				key := hex.EncodeToString(hash[:])[:40]
				ev.Links[key] = &JSCalendarLink{Type: "Link", Href: p.value}
			}
		case "EXDATE":
			for _, ex := range strings.Split(p.value, ",") {
				if ex = strings.TrimSpace(ex); ex == "" {
					continue
				}
				dateOnly := strings.EqualFold(icalParamValue(p.params, "VALUE"), "DATE")
				if ev.Excluded == nil {
					ev.Excluded = make(map[string]bool)
				}
				ev.Excluded[icalTimeToRFC3339(ex, dateOnly)] = true
			}
		case "RECURRENCE-ID":
			dateOnly := strings.EqualFold(icalParamValue(p.params, "VALUE"), "DATE")
			ev.RecurrenceID = icalTimeToRFC3339(p.value, dateOnly)
		case "CATEGORIES":
			if ev.Categories == nil {
				ev.Categories = make(map[string]bool)
			}
			for _, cat := range strings.Split(p.value, ",") {
				if cat = strings.TrimSpace(unescapeICalText(cat)); cat != "" {
					ev.Categories[cat] = true
				}
			}
		case "SEQUENCE":
			if n, err := strconv.ParseUint(strings.TrimSpace(p.value), 10, 32); err == nil {
				ev.Sequence = uint32(n)
			}
		case "CREATED":
			ev.Created = icalTimeToRFC3339(p.value, false)
		case "LAST-MODIFIED":
			ev.Updated = icalTimeToRFC3339(p.value, false)
		case "DTSTAMP":
			if ev.Updated == "" {
				ev.Updated = icalTimeToRFC3339(p.value, false)
			}
		}
	}

	// VALARM sub-components become JSCalendar alerts (RFC 5545 Section 3.6.6 →
	// RFC 8984 Section 4.5.2).
	for i, child := range comp.children {
		if child.name != "VALARM" {
			continue
		}
		if alert := parseVAlarm(child); alert != nil {
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

// parseVAlarm converts a VALARM component into a JSCalendar Alert (RFC 8984 Section 4.5.2),
// mapping ACTION, TRIGGER (relative offset or absolute DATE-TIME, with RELATED=END), and
// DESCRIPTION.
func parseVAlarm(comp *icalComponent) *JSCalendarAlert {
	alert := &JSCalendarAlert{Type: "Alert", Action: "display"}
	for _, p := range comp.properties {
		switch p.name {
		case "ACTION":
			if strings.EqualFold(p.value, "EMAIL") {
				alert.Action = "email"
			} else {
				alert.Action = "display"
			}
		case "TRIGGER":
			if strings.EqualFold(icalParamValue(p.params, "VALUE"), "DATE-TIME") {
				alert.Trigger = map[string]any{
					"@type": "AbsoluteTrigger",
					"when":  icalTimeToRFC3339(p.value, false),
				}
			} else {
				trigger := map[string]any{"@type": "OffsetTrigger", "offset": strings.TrimSpace(p.value)}
				if strings.EqualFold(icalParamValue(p.params, "RELATED"), "END") {
					trigger["relativeTo"] = "end"
				}
				alert.Trigger = trigger
			}
		case "DESCRIPTION":
			alert.Description = unescapeICalText(p.value)
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
