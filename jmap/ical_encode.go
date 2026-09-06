package jmap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// This file is the JSCalendar (RFC 8984) → iCalendar (RFC 5545) serializer using github.com/emersion/go-ical.
// It builds standard *ical.Calendar ASTs and encodes them via ical.NewEncoder for full
// RFC 5545 line folding, escaping, parameter quoting, and CRLF formatting.

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

func newRawProp(name, value string) *ical.Prop {
	p := ical.NewProp(name)
	p.Value = value
	return p
}

// addDateTimeProp adds a DTSTART/DTEND/RECURRENCE-ID property honouring all-day
// (VALUE=DATE), floating, UTC (trailing Z) and zoned (TZID) representations per RFC 5545
// Sections 3.3.4/3.3.5 and 3.8.2.
func addDateTimeProp(comp *ical.Component, name, value, timeZone string, allDay bool) {
	if value == "" {
		return
	}
	prop := ical.NewProp(name)
	if allDay {
		prop.Params.Set("VALUE", "DATE")
		prop.Value = icalCompactDate(value)
		comp.Props.Set(prop)
		return
	}
	compact := icalCompactDateTime(value)
	cleanCompact := strings.TrimSuffix(compact, "Z")
	switch {
	case timeZone != "" && timeZone != "Etc/UTC" && timeZone != "UTC":
		prop.Params.Set("TZID", timeZone)
		prop.Value = cleanCompact
	default:
		prop.Value = cleanCompact + "Z"
	}
	comp.Props.Set(prop)
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

func addAttendeeProp(comp *ical.Component, key string, p *JSCalendarParticipant) {
	addr := participantAddress(key, p)
	if addr == "" {
		return
	}
	prop := ical.NewProp(ical.PropAttendee)
	prop.Value = "mailto:" + addr
	prop.Params.Set("CUTYPE", icalCUTypeFor(p))
	prop.Params.Set("ROLE", icalRoleFor(p))
	prop.Params.Set("PARTSTAT", icalPartStatFor(p))
	if p.ExpectReply {
		prop.Params.Set("RSVP", "TRUE")
	}
	for _, delegate := range sortedTrueKeys(p.DelegatedTo) {
		prop.Params.Add("DELEGATED-TO", delegate)
	}
	for _, delegator := range sortedTrueKeys(p.DelegatedFrom) {
		prop.Params.Add("DELEGATED-FROM", delegator)
	}
	for _, member := range sortedTrueKeys(p.MemberOf) {
		prop.Params.Add("MEMBER", member)
	}
	if p.Name != "" {
		prop.Params.Set("CN", p.Name)
	}
	if p.ScheduleStatus != "" {
		prop.Params.Set("SCHEDULE-STATUS", p.ScheduleStatus)
	}
	if key != "" {
		prop.Params.Set("X-KEY", key)
	}
	comp.Props.Add(prop)
}

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

func buildAlarmComponent(key string, alert *JSCalendarAlert) *ical.Component {
	if alert == nil {
		return nil
	}
	action := "DISPLAY"
	if strings.EqualFold(alert.Action, "email") {
		action = "EMAIL"
	}
	alarm := ical.NewComponent(ical.CompAlarm)
	if key != "" {
		alarm.Props.Set(newRawProp("X-KEY", key))
	}
	alarm.Props.Set(newRawProp(ical.PropAction, action))

	trigger, _ := alert.Trigger.(map[string]any)
	switch {
	case trigger != nil && trigger["offset"] != nil:
		offset, _ := trigger["offset"].(string)
		tProp := newRawProp(ical.PropTrigger, offset)
		if related, _ := trigger["relativeTo"].(string); strings.EqualFold(related, "end") {
			tProp.Params.Set("RELATED", "END")
		}
		alarm.Props.Set(tProp)
	case trigger != nil && trigger["when"] != nil:
		when, _ := trigger["when"].(string)
		tProp := newRawProp(ical.PropTrigger, icalCompactDateTime(when))
		tProp.Params.Set("VALUE", "DATE-TIME")
		alarm.Props.Set(tProp)
	default:
		alarm.Props.Set(newRawProp(ical.PropTrigger, "-PT15M"))
	}

	desc := alert.Description
	if desc == "" {
		desc = "Reminder"
	}
	alarm.Props.SetText(ical.PropDescription, desc)
	return alarm
}

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

func buildEventComponent(ev *CalendarEvent, organizerEmail, onlyAttendee, statusOverride string) *ical.Component {
	compName := ical.CompEvent
	switch ev.Type {
	case "Task":
		compName = ical.CompToDo
	case "Group":
		compName = ical.CompJournal
	}
	comp := ical.NewComponent(compName)
	if ev.Type != "" && ev.Type != "Event" {
		comp.Props.SetText("X-JSCALENDAR-TYPE", ev.Type)
	}

	uid := eventUID(ev)
	if uid == "" {
		uid = fmt.Sprintf("event-%d", time.Now().UnixNano())
	}
	comp.Props.SetText(ical.PropUID, uid)
	comp.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())

	seqProp := ical.NewProp(ical.PropSequence)
	seqProp.Value = fmt.Sprintf("%d", ev.Sequence)
	comp.Props.Set(seqProp)

	if ev.Created != "" {
		cProp := ical.NewProp(ical.PropCreated)
		cProp.Value = icalCompactDateTime(ev.Created)
		comp.Props.Set(cProp)
	}
	if ev.Updated != "" {
		lmProp := ical.NewProp(ical.PropLastModified)
		lmProp.Value = icalCompactDateTime(ev.Updated)
		comp.Props.Set(lmProp)
	}
	if ev.Title != "" {
		comp.Props.SetText(ical.PropSummary, ev.Title)
	}
	if ev.Description != "" {
		comp.Props.SetText(ical.PropDescription, ev.Description)
	}

	if ev.Start != "" {
		addDateTimeProp(comp, ical.PropDateTimeStart, ev.Start, ev.TimeZone, ev.ShowWithoutTime)
	}
	if ev.Due != "" {
		addDateTimeProp(comp, ical.PropDue, ev.Due, ev.TimeZone, ev.ShowWithoutTime)
	}
	if ev.EstimatedDuration != "" {
		comp.Props.SetText("ESTIMATED-DURATION", ev.EstimatedDuration)
	}
	if ev.PercentComplete > 0 || (ev.Type == "Task" && ev.PercentComplete != 0) {
		pProp := ical.NewProp(ical.PropPercentComplete)
		pProp.Value = fmt.Sprintf("%d", ev.PercentComplete)
		comp.Props.Set(pProp)
	}
	if ev.Progress != "" {
		comp.Props.SetText("X-PROGRESS", ev.Progress)
		if ev.Status == "" && statusOverride == "" {
			switch strings.ToLower(ev.Progress) {
			case "completed":
				comp.Props.SetText(ical.PropStatus, "COMPLETED")
			case "in-process":
				comp.Props.SetText(ical.PropStatus, "IN-PROCESS")
			case "needs-action":
				comp.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
			case "cancelled":
				comp.Props.SetText(ical.PropStatus, "CANCELLED")
			}
		}
	}
	if ev.ProgressUpdated != "" {
		puProp := ical.NewProp("X-PROGRESS-UPDATED")
		puProp.Value = icalCompactDateTime(ev.ProgressUpdated)
		comp.Props.Set(puProp)
	}
	if ev.Source != "" {
		comp.Props.SetText(ical.PropSource, ev.Source)
	}
	if len(ev.Entries) > 0 {
		if data, err := json.Marshal(ev.Entries); err == nil {
			comp.Props.SetText("X-JSCALENDAR-ENTRIES", string(data))
		}
	}

	if ev.Type != "Task" || ev.Due == "" {
		if ev.ShowWithoutTime {
			if ev.Duration != "" && !strings.Contains(ev.Duration, "T") {
				comp.Props.Set(newRawProp(ical.PropDuration, ev.Duration))
			} else {
				comp.Props.Set(newRawProp(ical.PropDuration, "P1D"))
			}
		} else if ev.Duration != "" {
			comp.Props.Set(newRawProp(ical.PropDuration, ev.Duration))
		}
	}

	if ev.RecurrenceID != "" {
		addDateTimeProp(comp, ical.PropRecurrenceID, ev.RecurrenceID, ev.RecurrenceIDTimeZone, ev.ShowWithoutTime)
	}

	if len(ev.RecurrenceRules) == 0 && ev.RecurrenceRule != nil {
		if v := buildRRULE(ev.RecurrenceRule); v != "" {
			comp.Props.Set(newRawProp(ical.PropRecurrenceRule, v))
		}
	} else if len(ev.RecurrenceRules) > 0 {
		if v := buildRRULE(ev.RecurrenceRules[0]); v != "" {
			comp.Props.Set(newRawProp(ical.PropRecurrenceRule, v))
		}
	}

	if len(ev.ExcludedRecurrenceRules) == 0 && ev.ExcludedRecurrenceRule != nil {
		if v := buildRRULE(ev.ExcludedRecurrenceRule); v != "" {
			comp.Props.Add(newRawProp("EXRULE", v))
		}
	}
	for _, rule := range ev.ExcludedRecurrenceRules {
		if v := buildRRULE(rule); v != "" {
			comp.Props.Add(newRawProp("EXRULE", v))
		}
	}

	if exdates := excludedRecurrenceDates(ev); len(exdates) > 0 {
		comp.Props.Set(newRawProp(ical.PropExceptionDates, strings.Join(exdates, ",")))
	}

	status := statusOverride
	if status == "" {
		status = icalStatusFor(ev.Status)
	}
	if status != "" {
		comp.Props.SetText(ical.PropStatus, status)
	}

	switch strings.ToLower(ev.Privacy) {
	case "private":
		comp.Props.SetText(ical.PropClass, "PRIVATE")
	case "secret":
		comp.Props.SetText(ical.PropClass, "CONFIDENTIAL")
	case "public":
		comp.Props.SetText(ical.PropClass, "PUBLIC")
	}

	switch strings.ToLower(ev.FreeBusyStatus) {
	case "free":
		comp.Props.Set(newRawProp("TRANSP", "TRANSPARENT"))
	case "busy":
		comp.Props.Set(newRawProp("TRANSP", "OPAQUE"))
	}

	if ev.Priority > 0 {
		pProp := ical.NewProp(ical.PropPriority)
		pProp.Value = fmt.Sprintf("%d", ev.Priority)
		comp.Props.Set(pProp)
	}

	if ev.Color != "" {
		comp.Props.Set(newRawProp("COLOR", ev.Color))
	}

	addLocationAndGeo(comp, ev)

	if cats := categoryList(ev); cats != "" {
		comp.Props.Set(newRawProp(ical.PropCategories, cats))
	}

	if org := organizerAddress(ev); org != "" {
		orgProp := newRawProp(ical.PropOrganizer, "mailto:"+org)
		if ev.SentBy != "" {
			orgProp.Params.Set("SENT-BY", ev.SentBy)
		}
		comp.Props.Set(orgProp)
	} else if organizerEmail != "" {
		orgProp := newRawProp(ical.PropOrganizer, "mailto:"+organizerEmail)
		if ev.SentBy != "" {
			orgProp.Params.Set("SENT-BY", ev.SentBy)
		}
		comp.Props.Set(orgProp)
	} else if ev.SentBy != "" {
		orgProp := newRawProp(ical.PropOrganizer, "mailto:nobody@example.com")
		orgProp.Params.Set("SENT-BY", ev.SentBy)
		comp.Props.Set(orgProp)
	}

	if ev.DescriptionContentType != "" && ev.DescriptionContentType != "text/plain" {
		comp.Props.Set(newRawProp("X-DESCRIPTION-CONTENT-TYPE", ev.DescriptionContentType))
	}
	if ev.ShowWithoutTime {
		comp.Props.Set(newRawProp("X-SHOW-WITHOUT-TIME", "true"))
	}
	if ev.Locale != "" {
		comp.Props.Set(newRawProp("X-LOCALE", ev.Locale))
	}
	if ev.RequestStatus != "" {
		comp.Props.Set(newRawProp("REQUEST-STATUS", ev.RequestStatus))
	}
	if ev.Method != "" {
		comp.Props.SetText("X-JSCALENDAR-METHOD", ev.Method)
	}
	if ev.UseDefaultAlerts {
		comp.Props.Set(newRawProp("X-DEFAULT-ALERTS", "true"))
	}
	if ev.HideAttendees {
		comp.Props.Set(newRawProp("X-HIDE-ATTENDEES", "true"))
	}
	if ev.RecurrenceID != "" {
		comp.Props.SetText("X-RECURRENCE-ID", ev.RecurrenceID)
	}
	if ev.RecurrenceIDTimeZone != "" {
		comp.Props.SetText("X-RECURRENCE-ID-TZID", ev.RecurrenceIDTimeZone)
	}
	if len(ev.Locations) > 0 {
		if data, err := json.Marshal(ev.Locations); err == nil {
			comp.Props.SetText("X-JSCALENDAR-LOCATIONS", string(data))
		}
	}
	if len(ev.RecurrenceOverrides) > 0 {
		if data, err := json.Marshal(ev.RecurrenceOverrides); err == nil {
			comp.Props.SetText("X-JSCALENDAR-RECURRENCE-OVERRIDES", string(data))
		}
	}
	if len(ev.Participants) > 0 {
		if data, err := json.Marshal(ev.Participants); err == nil {
			comp.Props.SetText("X-JSCALENDAR-PARTICIPANTS", string(data))
		}
	}
	if len(ev.Alerts) > 0 {
		if data, err := json.Marshal(ev.Alerts); err == nil {
			comp.Props.SetText("X-JSCALENDAR-ALERTS", string(data))
		}
	}
	if len(ev.VirtualLocations) > 0 {
		if data, err := json.Marshal(ev.VirtualLocations); err == nil {
			comp.Props.SetText("X-JSCALENDAR-VIRTUAL-LOCATIONS", string(data))
		}
	}

	for _, key := range sortedParticipantKeys(ev.Participants) {
		p := ev.Participants[key]
		if strings.EqualFold(ev.Method, "REQUEST") && isOwnerParticipant(p) {
			continue
		}
		if onlyAttendee != "" && participantAddress(key, p) != onlyAttendee {
			continue
		}
		addAttendeeProp(comp, key, p)
	}

	for _, key := range sortedLinkKeys(ev.Links) {
		link := ev.Links[key]
		if link != nil && link.Href != "" {
			prop := newRawProp("ATTACH", link.Href)
			if key != "" {
				prop.Params.Set("X-KEY", key)
			}
			comp.Props.Add(prop)
		}
	}

	for _, key := range sortedVirtualLocationKeys(ev.VirtualLocations) {
		vl := ev.VirtualLocations[key]
		if vl != nil && vl.URI != "" {
			prop := newRawProp("CONFERENCE", vl.URI)
			prop.Params.Set("VALUE", "URI")
			if key != "" {
				prop.Params.Set("X-KEY", key)
			}
			comp.Props.Add(prop)
		}
	}

	for _, key := range sortedAlertKeys(ev.Alerts) {
		if alarm := buildAlarmComponent(key, ev.Alerts[key]); alarm != nil {
			comp.Children = append(comp.Children, alarm)
		}
	}

	return comp
}

func addLocationAndGeo(comp *ical.Component, ev *CalendarEvent) {
	for _, key := range sortedLocationKeys(ev.Locations) {
		loc := ev.Locations[key]
		if loc == nil {
			continue
		}
		if loc.Name != "" {
			comp.Props.SetText(ical.PropLocation, loc.Name)
		}
		if loc.Coordinates != "" {
			geo := strings.TrimPrefix(loc.Coordinates, "geo:")
			geo = strings.ReplaceAll(geo, ",", ";")
			comp.Props.Set(newRawProp(ical.PropGeo, geo))
		}
		return
	}
}

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

// CalendarEventToICalendar constructs an RFC 5545 *ical.Calendar from a JSCalendar CalendarEvent.
func CalendarEventToICalendar(ev *CalendarEvent, method, organizerEmail, onlyAttendee, statusOverride string) *ical.Calendar {
	cal := ical.NewCalendar()
	prodID := "-//IMAP-JMAP Server//NONSGML v1.0//EN"
	if ev.ProdID != "" {
		prodID = ev.ProdID
	}
	cal.Props.SetText(ical.PropProductID, prodID)
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropCalendarScale, "GREGORIAN")
	if method == "" && ev.Method != "" {
		method = ev.Method
	}
	if method != "" {
		cal.Props.SetText(ical.PropMethod, method)
	}

	comp := buildEventComponent(ev, organizerEmail, onlyAttendee, statusOverride)
	cal.Children = append(cal.Children, comp)

	for recurrenceID, override := range ev.RecurrenceOverrides {
		if excluded, _ := override["excluded"].(bool); excluded {
			continue
		}
		ovEv := *ev
		ovEv.RecurrenceRules = nil
		ovEv.ExcludedRecurrenceRules = nil
		ovEv.RecurrenceID = recurrenceID
		if ovBytes, err := json.Marshal(override); err == nil {
			_ = json.Unmarshal(ovBytes, &ovEv)
		}

		ovComp := buildEventComponent(&ovEv, organizerEmail, onlyAttendee, statusOverride)
		cal.Children = append(cal.Children, ovComp)
	}

	return cal
}

// EncodeCalDAVEvent builds a complete RFC 5545 / RFC 4791 VCALENDAR string for storing in CalDAV (no METHOD property).
func EncodeCalDAVEvent(ev *CalendarEvent) string {
	return encodeICalendar(ev, "", "", "", "")
}

func encodeICalendar(ev *CalendarEvent, method, organizerEmail, onlyAttendee, statusOverride string) string {
	cal := CalendarEventToICalendar(ev, method, organizerEmail, onlyAttendee, statusOverride)
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return ""
	}
	return buf.String()
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

func sortedVirtualLocationKeys(m map[string]*JSCalendarVirtualLocation) []string {
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
