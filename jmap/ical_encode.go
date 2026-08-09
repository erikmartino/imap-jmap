package jmap

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file is the JSCalendar (RFC 8984) → iCalendar (RFC 5545) serializer used to build
// iTIP (RFC 5546) scheduling messages carried over iMIP (RFC 6047). It is intentionally
// comprehensive so that a scheduling message is lossless for the properties real clients
// depend on: recurrence, alarms, locations, full participant metadata, all-day/timezone
// handling, and RFC 5545 Section 3.3.11 TEXT escaping.
//
// Content lines are emitted unfolded. RFC 5545 Section 3.1 folding is a SHOULD for the
// 75-octet soft limit; every interoperable parser (including this package's own) accepts
// unfolded lines, and not folding keeps the output stable for substring inspection.

// escapeICalText applies RFC 5545 Section 3.3.11 TEXT escaping: backslash, newline,
// semicolon and comma are escaped so a value can never break the line/param structure.
func escapeICalText(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		"\r\n", `\n`,
		"\n", `\n`,
		"\r", `\n`,
		`;`, `\;`,
		`,`, `\,`,
	)
	return r.Replace(s)
}

// unescapeICalText reverses escapeICalText for values read back from iCalendar.
func unescapeICalText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n', 'N':
				b.WriteByte('\n')
			case '\\', ';', ',':
				b.WriteByte(s[i+1])
			default:
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// icalCompactDateTime turns an RFC 3339 / JSCalendar date-time string into the compact
// iCalendar form ("2026-09-01T10:00:00Z" -> "20260901T100000Z", floating kept floating).
func icalCompactDateTime(v string) string {
	v = strings.ReplaceAll(v, "-", "")
	v = strings.ReplaceAll(v, ":", "")
	if dot := strings.IndexByte(v, '.'); dot >= 0 {
		// strip fractional seconds up to a trailing Z / offset
		end := dot + 1
		for end < len(v) && v[end] >= '0' && v[end] <= '9' {
			end++
		}
		v = v[:dot] + v[end:]
	}
	return v
}

// icalCompactDate turns a date(-time) string into the compact all-day DATE form YYYYMMDD.
func icalCompactDate(v string) string {
	if t, ok := parseRFC3339Time(v); ok {
		return t.Format("20060102")
	}
	d := icalCompactDateTime(v)
	if len(d) >= 8 {
		return d[:8]
	}
	return d
}

// writeDateTimeProp writes a DTSTART/DTEND/RECURRENCE-ID property honouring all-day
// (VALUE=DATE), floating, UTC (trailing Z) and zoned (TZID) representations per RFC 5545
// Sections 3.3.4/3.3.5 and 3.8.2.
func writeDateTimeProp(sb *strings.Builder, name, value, timeZone string, allDay bool) {
	if value == "" {
		return
	}
	if allDay {
		fmt.Fprintf(sb, "%s;VALUE=DATE:%s\r\n", name, icalCompactDate(value))
		return
	}
	compact := icalCompactDateTime(value)
	switch {
	case strings.HasSuffix(compact, "Z"):
		fmt.Fprintf(sb, "%s:%s\r\n", name, compact)
	case timeZone != "" && timeZone != "Etc/UTC" && timeZone != "UTC":
		fmt.Fprintf(sb, "%s;TZID=%s:%s\r\n", name, timeZone, compact)
	default:
		fmt.Fprintf(sb, "%s:%s\r\n", name, compact)
	}
}

// icalRoleFor maps a JSCalendar participant's roles to an iCalendar ROLE parameter.
func icalRoleFor(p *JSCalendarParticipant) string {
	has := func(r string) bool {
		return (p.Roles != nil && p.Roles[r]) || p.Role == r
	}
	switch {
	case has("chair"):
		return "CHAIR"
	case has("optional"):
		return "OPT-PARTICIPANT"
	case has("informational"):
		return "NON-PARTICIPANT"
	default:
		return "REQ-PARTICIPANT"
	}
}

// icalCUTypeFor maps a JSCalendar participant kind to an iCalendar CUTYPE parameter.
func icalCUTypeFor(p *JSCalendarParticipant) string {
	switch p.Kind {
	case "group":
		return "GROUP"
	case "resource":
		return "RESOURCE"
	case "location":
		return "ROOM"
	default:
		return "INDIVIDUAL"
	}
}

// icalPartStatFor maps a JSCalendar participationStatus to an iCalendar PARTSTAT parameter.
func icalPartStatFor(p *JSCalendarParticipant) string {
	status := p.ParticipationStatus
	if status == "" {
		status = p.Status
	}
	switch strings.ToLower(status) {
	case "accepted":
		return "ACCEPTED"
	case "declined":
		return "DECLINED"
	case "tentative":
		return "TENTATIVE"
	case "delegated":
		return "DELEGATED"
	default:
		return "NEEDS-ACTION"
	}
}

// writeAttendee writes one ATTENDEE property with full parameters. The basic form is
// "ATTENDEE;CUTYPE=..;ROLE=..;PARTSTAT=..;CN=Name:mailto:addr"; RSVP / DELEGATED-* /
// MEMBER params are added only when set so simple participants stay compact.
func writeAttendee(sb *strings.Builder, key string, p *JSCalendarParticipant) {
	addr := participantAddress(key, p)
	if addr == "" {
		return
	}
	sb.WriteString("ATTENDEE")
	fmt.Fprintf(sb, ";CUTYPE=%s", icalCUTypeFor(p))
	fmt.Fprintf(sb, ";ROLE=%s", icalRoleFor(p))
	fmt.Fprintf(sb, ";PARTSTAT=%s", icalPartStatFor(p))
	if p.ExpectReply {
		sb.WriteString(";RSVP=TRUE")
	}
	for _, delegate := range sortedTrueKeys(p.DelegatedTo) {
		fmt.Fprintf(sb, ";DELEGATED-TO=\"%s\"", delegate)
	}
	for _, delegator := range sortedTrueKeys(p.DelegatedFrom) {
		fmt.Fprintf(sb, ";DELEGATED-FROM=\"%s\"", delegator)
	}
	for _, member := range sortedTrueKeys(p.MemberOf) {
		fmt.Fprintf(sb, ";MEMBER=\"%s\"", member)
	}
	if p.Name != "" {
		fmt.Fprintf(sb, ";CN=%s", p.Name)
	}
	fmt.Fprintf(sb, ":mailto:%s\r\n", addr)
}

// sortedTrueKeys returns the true-valued keys of a JSCalendar set in sorted order, so
// serialized output (EXDATE, CATEGORIES, DELEGATED-*, MEMBER, …) is deterministic.
func sortedTrueKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// buildRRULE serializes a JSCalendar RecurrenceRule (RFC 8984 Section 4.3.3) into an
// RFC 5545 RRULE value (all byX parts, interval, count/until, WKST, bySetPos).
func buildRRULE(rule *JSCalendarRecurrenceRule) string {
	if rule == nil || rule.Frequency == "" {
		return ""
	}
	parts := []string{"FREQ=" + strings.ToUpper(rule.Frequency)}
	if rule.Interval > 1 {
		parts = append(parts, fmt.Sprintf("INTERVAL=%d", rule.Interval))
	}
	if rule.Count > 0 {
		parts = append(parts, fmt.Sprintf("COUNT=%d", rule.Count))
	}
	if rule.Until != "" {
		parts = append(parts, "UNTIL="+icalCompactDateTime(rule.Until))
	}
	if len(rule.ByDay) > 0 {
		days := make([]string, 0, len(rule.ByDay))
		for _, nd := range rule.ByDay {
			if nd == nil || nd.Day == "" {
				continue
			}
			if nd.Nth != 0 {
				days = append(days, fmt.Sprintf("%d%s", nd.Nth, strings.ToUpper(nd.Day)))
			} else {
				days = append(days, strings.ToUpper(nd.Day))
			}
		}
		if len(days) > 0 {
			parts = append(parts, "BYDAY="+strings.Join(days, ","))
		}
	}
	if v := joinInts(rule.ByMonthDay); v != "" {
		parts = append(parts, "BYMONTHDAY="+v)
	}
	if len(rule.ByMonth) > 0 {
		months := make([]string, 0, len(rule.ByMonth))
		for _, m := range rule.ByMonth {
			months = append(months, strings.TrimLeft(strings.TrimSpace(m), "0"))
		}
		parts = append(parts, "BYMONTH="+strings.Join(months, ","))
	}
	if v := joinInts(rule.ByYearDay); v != "" {
		parts = append(parts, "BYYEARDAY="+v)
	}
	if v := joinInts(rule.ByWeekNo); v != "" {
		parts = append(parts, "BYWEEKNO="+v)
	}
	if v := joinUints(rule.ByHour); v != "" {
		parts = append(parts, "BYHOUR="+v)
	}
	if v := joinUints(rule.ByMinute); v != "" {
		parts = append(parts, "BYMINUTE="+v)
	}
	if v := joinUints(rule.BySecond); v != "" {
		parts = append(parts, "BYSECOND="+v)
	}
	if v := joinInts(rule.BySetPosition); v != "" {
		parts = append(parts, "BYSETPOS="+v)
	}
	if rule.FirstDayOfWeek != "" {
		parts = append(parts, "WKST="+strings.ToUpper(rule.FirstDayOfWeek))
	}
	return strings.Join(parts, ";")
}

func joinInts(v []int) string {
	if len(v) == 0 {
		return ""
	}
	out := make([]string, len(v))
	for i, n := range v {
		out[i] = fmt.Sprintf("%d", n)
	}
	return strings.Join(out, ",")
}

func joinUints(v []uint32) string {
	if len(v) == 0 {
		return ""
	}
	out := make([]string, len(v))
	for i, n := range v {
		out[i] = fmt.Sprintf("%d", n)
	}
	return strings.Join(out, ",")
}

// writeAlarm serializes a JSCalendar Alert (RFC 8984 Section 4.5.2) into a VALARM
// sub-component (RFC 5545 Section 3.6.6): ACTION, TRIGGER (relative offset or absolute
// UTC), and DESCRIPTION.
func writeAlarm(sb *strings.Builder, alert *JSCalendarAlert) {
	if alert == nil {
		return
	}
	action := "DISPLAY"
	if strings.EqualFold(alert.Action, "email") {
		action = "EMAIL"
	}
	sb.WriteString("BEGIN:VALARM\r\n")
	fmt.Fprintf(sb, "ACTION:%s\r\n", action)

	trigger, _ := alert.Trigger.(map[string]any)
	switch {
	case trigger != nil && trigger["offset"] != nil:
		offset, _ := trigger["offset"].(string)
		if related, _ := trigger["relativeTo"].(string); strings.EqualFold(related, "end") {
			fmt.Fprintf(sb, "TRIGGER;RELATED=END:%s\r\n", offset)
		} else {
			fmt.Fprintf(sb, "TRIGGER:%s\r\n", offset)
		}
	case trigger != nil && trigger["when"] != nil:
		when, _ := trigger["when"].(string)
		fmt.Fprintf(sb, "TRIGGER;VALUE=DATE-TIME:%s\r\n", icalCompactDateTime(when))
	default:
		sb.WriteString("TRIGGER:-PT15M\r\n")
	}

	desc := alert.Description
	if desc == "" {
		desc = "Reminder"
	}
	fmt.Fprintf(sb, "DESCRIPTION:%s\r\n", escapeICalText(desc))
	sb.WriteString("END:VALARM\r\n")
}

// icalStatusFor maps a JSCalendar event status to an iCalendar STATUS value.
func icalStatusFor(status string) string {
	switch strings.ToLower(status) {
	case "confirmed":
		return "CONFIRMED"
	case "tentative":
		return "TENTATIVE"
	case "cancelled", "canceled":
		return "CANCELLED"
	}
	return ""
}

// writeVEVENT serializes a CalendarEvent as a full VEVENT body (no BEGIN/END), covering
// the properties an iTIP peer needs. organizerEmail is the fallback ORGANIZER when the
// event carries no organizer participant/replyTo. When onlyAttendee is non-empty, only
// that participant is emitted as ATTENDEE (used for METHOD:REPLY and hideAttendees).
func writeVEVENT(sb *strings.Builder, ev *CalendarEvent, organizerEmail, onlyAttendee, statusOverride string) {
	uid := eventUID(ev)
	now := time.Now().UTC().Format("20060102T150405Z")

	fmt.Fprintf(sb, "UID:%s\r\n", uid)
	fmt.Fprintf(sb, "DTSTAMP:%s\r\n", now)
	fmt.Fprintf(sb, "SEQUENCE:%d\r\n", ev.Sequence)
	if ev.Created != "" {
		fmt.Fprintf(sb, "CREATED:%s\r\n", icalCompactDateTime(ev.Created))
	}
	if ev.Updated != "" {
		fmt.Fprintf(sb, "LAST-MODIFIED:%s\r\n", icalCompactDateTime(ev.Updated))
	}
	if ev.Title != "" {
		fmt.Fprintf(sb, "SUMMARY:%s\r\n", escapeICalText(ev.Title))
	}
	if ev.Description != "" {
		fmt.Fprintf(sb, "DESCRIPTION:%s\r\n", escapeICalText(ev.Description))
	}
	writeDateTimeProp(sb, "DTSTART", ev.Start, ev.TimeZone, ev.ShowWithoutTime)
	if ev.Duration != "" {
		fmt.Fprintf(sb, "DURATION:%s\r\n", ev.Duration)
	}
	if ev.RecurrenceID != "" {
		writeDateTimeProp(sb, "RECURRENCE-ID", ev.RecurrenceID, ev.RecurrenceIDTimeZone, ev.ShowWithoutTime)
	}
	for _, rule := range ev.RecurrenceRules {
		if v := buildRRULE(rule); v != "" {
			fmt.Fprintf(sb, "RRULE:%s\r\n", v)
		}
	}
	for _, rule := range ev.ExcludedRecurrenceRules {
		if v := buildRRULE(rule); v != "" {
			fmt.Fprintf(sb, "EXRULE:%s\r\n", v)
		}
	}
	if exdates := excludedRecurrenceDates(ev); len(exdates) > 0 {
		fmt.Fprintf(sb, "EXDATE:%s\r\n", strings.Join(exdates, ","))
	}
	status := statusOverride
	if status == "" {
		status = icalStatusFor(ev.Status)
	}
	if status != "" {
		fmt.Fprintf(sb, "STATUS:%s\r\n", status)
	}
	switch strings.ToLower(ev.Privacy) {
	case "private":
		sb.WriteString("CLASS:PRIVATE\r\n")
	case "secret":
		sb.WriteString("CLASS:CONFIDENTIAL\r\n")
	case "public":
		sb.WriteString("CLASS:PUBLIC\r\n")
	}
	switch strings.ToLower(ev.FreeBusyStatus) {
	case "free":
		sb.WriteString("TRANSP:TRANSPARENT\r\n")
	case "busy":
		sb.WriteString("TRANSP:OPAQUE\r\n")
	}
	if ev.Priority > 0 {
		fmt.Fprintf(sb, "PRIORITY:%d\r\n", ev.Priority)
	}
	if ev.Color != "" {
		fmt.Fprintf(sb, "COLOR:%s\r\n", ev.Color)
	}
	writeLocationAndGeo(sb, ev)
	if cats := categoryList(ev); cats != "" {
		fmt.Fprintf(sb, "CATEGORIES:%s\r\n", cats)
	}
	if org := organizerAddress(ev); org != "" {
		fmt.Fprintf(sb, "ORGANIZER:mailto:%s\r\n", org)
	} else if organizerEmail != "" {
		fmt.Fprintf(sb, "ORGANIZER:mailto:%s\r\n", organizerEmail)
	}
	for _, key := range sortedParticipantKeys(ev.Participants) {
		p := ev.Participants[key]
		if isOwnerParticipant(p) {
			continue // the owner is the ORGANIZER, not an ATTENDEE
		}
		if onlyAttendee != "" && participantAddress(key, p) != onlyAttendee {
			continue
		}
		writeAttendee(sb, key, p)
	}
	for _, key := range sortedLinkKeys(ev.Links) {
		link := ev.Links[key]
		if link != nil && link.Href != "" {
			fmt.Fprintf(sb, "ATTACH:%s\r\n", link.Href)
		}
	}
	for _, key := range sortedAlertKeys(ev.Alerts) {
		writeAlarm(sb, ev.Alerts[key])
	}
}

// encodeICalendar builds a complete VCALENDAR (RFC 5545) carrying the event as a single
// VEVENT, with the given iTIP METHOD (RFC 5546). onlyAttendee/statusOverride tailor the
// component for REPLY / CANCEL.
func encodeICalendar(ev *CalendarEvent, method, organizerEmail, onlyAttendee, statusOverride string) string {
	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//IMAP-JMAP Server//NONSGML v1.0//EN\r\n")
	sb.WriteString("CALSCALE:GREGORIAN\r\n")
	if method != "" {
		fmt.Fprintf(&sb, "METHOD:%s\r\n", method)
	}
	sb.WriteString("BEGIN:VEVENT\r\n")
	writeVEVENT(&sb, ev, organizerEmail, onlyAttendee, statusOverride)
	sb.WriteString("END:VEVENT\r\n")
	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
}

// excludedRecurrenceDates gathers EXDATE values from both the excluded set and any
// recurrenceOverrides entry marked excluded:true (RFC 8984 Section 4.3.5).
func excludedRecurrenceDates(ev *CalendarEvent) []string {
	set := map[string]bool{}
	for d := range ev.Excluded {
		set[icalCompactDateTime(d)] = true
	}
	for recurrenceID, override := range ev.RecurrenceOverrides {
		if excluded, _ := override["excluded"].(bool); excluded {
			set[icalCompactDateTime(recurrenceID)] = true
		}
	}
	return sortedTrueKeys(set)
}

func categoryList(ev *CalendarEvent) string {
	set := map[string]bool{}
	for c := range ev.Categories {
		set[c] = true
	}
	for k := range ev.Keywords {
		set[k] = true
	}
	cats := sortedTrueKeys(set)
	for i, c := range cats {
		cats[i] = escapeICalText(c)
	}
	return strings.Join(cats, ",")
}

// writeLocationAndGeo emits the first named location as LOCATION and its coordinates as GEO.
func writeLocationAndGeo(sb *strings.Builder, ev *CalendarEvent) {
	for _, key := range sortedLocationKeys(ev.Locations) {
		loc := ev.Locations[key]
		if loc == nil {
			continue
		}
		if loc.Name != "" {
			fmt.Fprintf(sb, "LOCATION:%s\r\n", escapeICalText(loc.Name))
		}
		if loc.Coordinates != "" {
			geo := strings.TrimPrefix(loc.Coordinates, "geo:")
			geo = strings.ReplaceAll(geo, ",", ";")
			fmt.Fprintf(sb, "GEO:%s\r\n", geo)
		}
		return // one LOCATION/GEO pair is emitted (RFC 5545 allows a single LOCATION)
	}
}

func sortedParticipantKeys(m map[string]*JSCalendarParticipant) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedLinkKeys(m map[string]*JSCalendarLink) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedAlertKeys(m map[string]*JSCalendarAlert) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedLocationKeys(m map[string]*JSCalendarLocation) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
