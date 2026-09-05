package jmap

import (
	"sort"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

func matchEventText(ev *CalendarEvent, q string) bool {
	if containsFold(ev.Title, q) || containsFold(ev.Description, q) {
		return true
	}
	for _, loc := range ev.Locations {
		if loc != nil && (containsFold(loc.Name, q) || containsFold(loc.Description, q)) {
			return true
		}
	}
	for _, p := range ev.Participants {
		if p != nil && (containsFold(p.Name, q) || containsFold(p.Email, q)) {
			return true
		}
	}
	return false
}

// MatchCalendarEvent reports whether an event satisfies a CalendarEvent filter
// condition per RFC 8984 / draft-ietf-jmap-calendars Section 5.11.
func MatchCalendarEvent(ev *CalendarEvent, filter map[string]any) bool {
	loc := time.UTC
	if tz, ok := filter["__timeZone"].(string); ok && tz != "" {
		loc = loadLocation(tz)
	}
	return matchCalendarEventInLoc(ev, filter, loc)
}

func matchCalendarEventInLoc(ev *CalendarEvent, filter map[string]any, loc *time.Location) bool {
	if filter == nil {
		return true
	}
	if match, isOp := EvalFilterOperator(filter, func(cond map[string]any) bool {
		return matchCalendarEventInLoc(ev, cond, loc)
	}); isOp {
		return match
	}
	for k, v := range filter {
		switch k {
		case "__timeZone":
		case "inCalendar":
			calID, ok := v.(string)
			if !ok || !ev.CalendarIDs[Id(calID)] {
				return false
			}
		case "inCalendars":
			raw, ok := v.([]any)
			if !ok {
				return false
			}
			matched := false
			for _, item := range raw {
				if calID, ok := item.(string); ok && ev.CalendarIDs[Id(calID)] {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		case "title":
			s, _ := v.(string)
			if !containsFold(ev.Title, s) {
				return false
			}
		case "description":
			s, _ := v.(string)
			if !containsFold(ev.Description, s) {
				return false
			}
		case "location":
			s, _ := v.(string)
			matchedLoc := false
			for _, loc := range ev.Locations {
				if loc != nil && (containsFold(loc.Name, s) || containsFold(loc.Description, s)) {
					matchedLoc = true
					break
				}
			}
			if !matchedLoc {
				return false
			}
		case "text":
			s, _ := v.(string)
			if !matchEventText(ev, s) {
				return false
			}
		case "after":
			s, _ := v.(string)
			if !eventEndsAfter(ev, s, loc) {
				return false
			}
		case "before":
			s, _ := v.(string)
			if !eventStartsBefore(ev, s, loc) {
				return false
			}
		case "uid":
			s, _ := v.(string)
			if ev.UID != s {
				return false
			}
		case "owner":
			s, _ := v.(string)
			matchedOwner := false
			for _, p := range ev.Participants {
				if p != nil && (p.Role == "owner" || (p.Roles != nil && p.Roles["owner"])) {
					if containsFold(p.Email, s) || containsFold(p.Name, s) {
						matchedOwner = true
						break
					}
				}
			}
			if !matchedOwner {
				return false
			}
		case "attendee":
			s, _ := v.(string)
			matchedAttendee := false
			for _, p := range ev.Participants {
				if p != nil && (containsFold(p.Email, s) || containsFold(p.Name, s)) {
					matchedAttendee = true
					break
				}
			}
			if !matchedAttendee {
				return false
			}
		case "updatedBefore":
			s, _ := v.(string)
			if s != "" && ev.Updated >= s {
				return false
			}
		case "updatedAfter":
			s, _ := v.(string)
			if s != "" && ev.Updated < s {
				return false
			}
		}
	}
	return true
}

func parseRFC3339(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseFloatingDateTime(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func eventEndTime(ev *CalendarEvent) (time.Time, bool) {
	start, ok := parseRFC3339(ev.Start)
	if !ok {
		return time.Time{}, false
	}
	if ev.Duration == "" {
		return start, true
	}
	d, ok := ParseISODuration(ev.Duration)
	if !ok {
		return start, true
	}
	return start.Add(d), true
}

// RecurrenceInstance defines an expanded instance of a recurring event per RFC 8984.
type RecurrenceInstance struct {
	RecurrenceID string
	Start        time.Time
	End          time.Time
}

func jsWeekdayToRRule(nd *NDay) (rrule.Weekday, bool) {
	if nd == nil {
		return rrule.Weekday{}, false
	}
	var wd rrule.Weekday
	switch strings.ToLower(nd.Day) {
	case "mo":
		wd = rrule.MO
	case "tu":
		wd = rrule.TU
	case "we":
		wd = rrule.WE
	case "th":
		wd = rrule.TH
	case "fr":
		wd = rrule.FR
	case "sa":
		wd = rrule.SA
	case "su":
		wd = rrule.SU
	default:
		return rrule.Weekday{}, false
	}
	if nd.Nth != 0 {
		wd = wd.Nth(nd.Nth)
	}
	return wd, true
}

func jsFrequencyToRRule(freq string) (rrule.Frequency, bool) {
	switch strings.ToLower(freq) {
	case "yearly":
		return rrule.YEARLY, true
	case "monthly":
		return rrule.MONTHLY, true
	case "weekly":
		return rrule.WEEKLY, true
	case "daily":
		return rrule.DAILY, true
	case "hourly":
		return rrule.HOURLY, true
	case "minutely":
		return rrule.MINUTELY, true
	case "secondly":
		return rrule.SECONDLY, true
	}
	return 0, false
}

func jsMonthToRRule(m string) (int, bool) {
	m = strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(m)), "L")
	if m == "" {
		return 0, false
	}
	n := 0
	for _, c := range m {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > 12 {
		return 0, false
	}
	return n, true
}

func buildRRuleOption(rule *JSCalendarRecurrenceRule, dtstart time.Time) (rrule.ROption, bool) {
	if rule == nil {
		return rrule.ROption{}, false
	}
	freq, ok := jsFrequencyToRRule(rule.Frequency)
	if !ok {
		return rrule.ROption{}, false
	}
	opt := rrule.ROption{Freq: freq, Dtstart: dtstart}

	opt.Interval = int(rule.Interval)
	if opt.Interval == 0 {
		opt.Interval = 1
	}
	if rule.Count > 0 {
		opt.Count = int(rule.Count)
	}
	if rule.Until != "" {
		if u, ok := parseRFC3339(rule.Until); ok {
			opt.Until = u
		} else if u, ok := parseFloatingDateTime(rule.Until); ok {
			opt.Until = u
		}
	}
	if rule.FirstDayOfWeek != "" {
		if wd, ok := jsWeekdayToRRule(&NDay{Day: rule.FirstDayOfWeek}); ok {
			opt.Wkst = wd
		}
	}
	for _, nd := range rule.ByDay {
		if wd, ok := jsWeekdayToRRule(nd); ok {
			opt.Byweekday = append(opt.Byweekday, wd)
		}
	}
	opt.Bymonthday = append(opt.Bymonthday, rule.ByMonthDay...)
	opt.Byyearday = append(opt.Byyearday, rule.ByYearDay...)
	opt.Byweekno = append(opt.Byweekno, rule.ByWeekNo...)
	opt.Bysetpos = append(opt.Bysetpos, rule.BySetPosition...)
	for _, m := range rule.ByMonth {
		if n, ok := jsMonthToRRule(m); ok {
			opt.Bymonth = append(opt.Bymonth, n)
		}
	}
	for _, h := range rule.ByHour {
		opt.Byhour = append(opt.Byhour, int(h))
	}
	for _, mn := range rule.ByMinute {
		opt.Byminute = append(opt.Byminute, int(mn))
	}
	for _, s := range rule.BySecond {
		opt.Bysecond = append(opt.Bysecond, int(s))
	}
	return opt, true
}

func overrideStart(recID string, patch map[string]any) (time.Time, bool) {
	if patch != nil {
		if s, ok := patch["start"].(string); ok && s != "" {
			if t, ok := parseRFC3339(s); ok {
				return t, true
			}
			if t, ok := parseFloatingDateTime(s); ok {
				return t, true
			}
		}
	}
	if t, ok := parseRFC3339(recID); ok {
		return t, true
	}
	if t, ok := parseFloatingDateTime(recID); ok {
		return t, true
	}
	return time.Time{}, false
}

// ExpandRecurrenceInstances expands an event's recurrenceRules over [start, horizon] per RFC 8984.
func ExpandRecurrenceInstances(ev *CalendarEvent, horizon time.Time) []RecurrenceInstance {
	start, ok := parseRFC3339(ev.Start)
	if !ok {
		if start, ok = parseFloatingDateTime(ev.Start); !ok {
			return nil
		}
	}

	duration := time.Duration(0)
	if ev.Duration != "" {
		if d, ok := ParseISODuration(ev.Duration); ok {
			duration = d
		}
	}

	masterInst := RecurrenceInstance{
		RecurrenceID: ev.Start,
		Start:        start,
		End:          start.Add(duration),
	}

	hasRules := false
	for _, r := range ev.RecurrenceRules {
		if r != nil {
			hasRules = true
			break
		}
	}
	if !hasRules && len(ev.RecurrenceOverrides) == 0 {
		return []RecurrenceInstance{masterInst}
	}

	if horizon.IsZero() {
		horizon = start.AddDate(5, 0, 0)
	}

	starts := make(map[string]time.Time)

	if hasRules {
		set := &rrule.Set{}
		set.DTStart(start)
		for _, rule := range ev.RecurrenceRules {
			opt, ok := buildRRuleOption(rule, start)
			if !ok {
				continue
			}
			rr, err := rrule.NewRRule(opt)
			if err != nil {
				continue
			}
			set.RRule(rr)
		}
		for _, rule := range ev.ExcludedRecurrenceRules {
			opt, ok := buildRRuleOption(rule, start)
			if !ok {
				continue
			}
			rr, err := rrule.NewRRule(opt)
			if err != nil {
				continue
			}
			for i, t := range rr.Between(start.Add(-time.Second), horizon, true) {
				if i >= 5000 {
					break
				}
				set.ExDate(t)
			}
		}
		occ := set.Between(start.Add(-time.Second), horizon, true)
		for i, t := range occ {
			if i >= 5000 {
				break
			}
			starts[t.UTC().Format(time.RFC3339)] = t
		}
	} else {
		starts[start.UTC().Format(time.RFC3339)] = start
	}

	for recID := range ev.RecurrenceOverrides {
		if _, present := starts[recID]; present {
			continue
		}
		if t, ok := overrideStart(recID, ev.RecurrenceOverrides[recID]); ok {
			starts[t.UTC().Format(time.RFC3339)] = t
		}
	}

	results := make([]RecurrenceInstance, 0, len(starts))
	for recID, t := range starts {
		if ev.Excluded != nil && ev.Excluded[recID] {
			continue
		}
		instStart := t
		instDur := duration
		if ov, ok := ev.RecurrenceOverrides[recID]; ok {
			if excluded, _ := ov["excluded"].(bool); excluded {
				continue
			}
			if s, ok := overrideStart(recID, ov); ok {
				instStart = s
			}
			if ds, ok := ov["duration"].(string); ok && ds != "" {
				if d, ok := ParseISODuration(ds); ok {
					instDur = d
				}
			}
		}
		results = append(results, RecurrenceInstance{
			RecurrenceID: recID,
			Start:        instStart,
			End:          instStart.Add(instDur),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if !results[i].Start.Equal(results[j].Start) {
			return results[i].Start.Before(results[j].Start)
		}
		return results[i].RecurrenceID < results[j].RecurrenceID
	})

	if len(results) == 0 {
		return []RecurrenceInstance{masterInst}
	}
	return results
}


func eventIsFloating(ev *CalendarEvent) bool {
	if ev.TimeZone != "" {
		return false
	}
	if _, ok := parseRFC3339(ev.Start); ok {
		return false
	}
	return true
}

func eventInstantInLoc(ev *CalendarEvent, t time.Time, loc *time.Location) time.Time {
	if eventIsFloating(ev) {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
	}
	return t
}

func eventEndsAfter(ev *CalendarEvent, date string, loc *time.Location) bool {
	ref, ok := parseLocalDateTimeBound(date, loc)
	if !ok {
		return false
	}
	for _, inst := range ExpandRecurrenceInstances(ev, ref.AddDate(1, 0, 0)) {
		if eventInstantInLoc(ev, inst.End, loc).After(ref) {
			return true
		}
	}
	return false
}

func eventStartsBefore(ev *CalendarEvent, date string, loc *time.Location) bool {
	ref, ok := parseLocalDateTimeBound(date, loc)
	if !ok {
		return false
	}
	for _, inst := range ExpandRecurrenceInstances(ev, ref) {
		if eventInstantInLoc(ev, inst.Start, loc).Before(ref) {
			return true
		}
	}
	return false
}

func SortCalendarEvents(events []*CalendarEvent, comparators []Comparator) {
	sort.SliceStable(events, func(i, j int) bool {
		for _, comp := range comparators {
			var cmp int
			switch comp.Property {
			case "start":
				cmp = strings.Compare(events[i].Start, events[j].Start)
			case "uid":
				cmp = strings.Compare(events[i].UID, events[j].UID)
			case "recurrenceId":
				cmp = strings.Compare(events[i].RecurrenceID, events[j].RecurrenceID)
			case "created":
				cmp = strings.Compare(events[i].Created, events[j].Created)
			case "updated":
				cmp = strings.Compare(events[i].Updated, events[j].Updated)
			case "title":
				cmp = strings.Compare(strings.ToLower(events[i].Title), strings.ToLower(events[j].Title))
			}
			if cmp != 0 {
				if comp.IsAscending {
					return cmp < 0
				}
				return cmp > 0
			}
		}
		if events[i].Start != events[j].Start {
			return events[i].Start < events[j].Start
		}
		return events[i].ID < events[j].ID
	})
}
