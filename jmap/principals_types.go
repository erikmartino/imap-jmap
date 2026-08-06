package jmap

import (
	"encoding/json"
	"strings"
	"time"
)

// PrincipalsCapabilityURI is the JMAP capability URI for the Principal and
// ShareNotification data types per RFC 9670 Section 1.5.1.
const PrincipalsCapabilityURI = "urn:ietf:params:jmap:principals"

// PrincipalsOwnerCapabilityURI is only used as a key in an Account's accountCapabilities
// to indicate that the Account (and its data) is owned by a Principal (RFC 9670 Section 1.5.2).
const PrincipalsOwnerCapabilityURI = "urn:ietf:params:jmap:principals:owner"

// PrincipalsAvailabilityCapabilityURI advertises support for Principal/getAvailability
// per draft-ietf-jmap-calendars Section 1.5.2.
const PrincipalsAvailabilityCapabilityURI = "urn:ietf:params:jmap:principals:availability"

// DefaultMaxAvailabilityDuration is the maximum duration over which the server is
// prepared to calculate availability in a single Principal/getAvailability call.
const DefaultMaxAvailabilityDuration = "P30D"

// PrincipalCapability is the account-level capability object for
// "urn:ietf:params:jmap:principals" per RFC 9670 Section 1.5.1.
type PrincipalCapability struct {
	CurrentUserPrincipalID Id `json:"currentUserPrincipalId"`
}

// PrincipalsOwnerCapability is the account-level capability object for
// "urn:ietf:params:jmap:principals:owner" per RFC 9670 Section 1.5.2.
type PrincipalsOwnerCapability struct {
	AccountIDForPrincipal Id `json:"accountIdForPrincipal"`
	PrincipalID           Id `json:"principalId"`
}

// PrincipalsAvailabilityCapability is the account-level capability object for
// "urn:ietf:params:jmap:principals:availability" per draft-ietf-jmap-calendars Section 1.5.2.
type PrincipalsAvailabilityCapability struct {
	MaxAvailabilityDuration string `json:"maxAvailabilityDuration"`
}

// PrincipalCalendarsCapability is the "urn:ietf:params:jmap:calendars" property in a
// Principal's "capabilities" object per draft-ietf-jmap-calendars Section 2.1.
type PrincipalCalendarsCapability struct {
	AccountID          *Id               `json:"accountId"`
	MayGetAvailability bool              `json:"mayGetAvailability"`
	MayShareWith       bool              `json:"mayShareWith"`
	CalendarAddress    string            `json:"calendarAddress"`
	SendTo             map[string]string `json:"sendTo,omitempty"`
}

// Principal represents an individual, group, location, or resource per RFC 9670 Section 2.
type Principal struct {
	ID           Id                    `json:"id"`
	Type         string                `json:"type"` // "individual", "group", "resource", "location", "other"
	Name         string                `json:"name"`
	Description  *string               `json:"description"`
	Email        *string               `json:"email"`
	TimeZone     *string               `json:"timeZone"`
	Capabilities map[string]any        `json:"capabilities"`
	Accounts     map[string]*Account   `json:"accounts"`
}

// PrincipalTypeValues are the valid Principal "type" values per RFC 9670 Section 2.
var PrincipalTypeValues = map[string]bool{
	"individual": true,
	"group":      true,
	"resource":   true,
	"location":   true,
	"other":      true,
}

// principalSortableProperties are the sortable properties for Principal/query.
var principalSortableProperties = map[string]bool{
	"id": true, "name": true, "type": true, "email": true, "description": true, "timeZone": true,
}

// BusyPeriod describes a period of busy time for a Principal per
// draft-ietf-jmap-calendars Section 2.2.
type BusyPeriod struct {
	UTCStart   string         `json:"utcStart"`
	UTCEnd     string         `json:"utcEnd"`
	BusyStatus string         `json:"busyStatus"` // "confirmed", "tentative", "unavailable"
	Event      *CalendarEvent `json:"event"`      // null when details are not permitted
}

// MatchPrincipal reports whether a Principal satisfies a Principal/query FilterCondition
// per RFC 9670 Section 2.4.1. All given conditions must match.
func MatchPrincipal(p *Principal, filter map[string]any) bool {
	for k, v := range filter {
		switch k {
		case "accountIds":
			raw, _ := v.([]any)
			if len(raw) == 0 || p.Accounts == nil {
				return false
			}
			matched := false
			for _, item := range raw {
				if idStr, ok := item.(string); ok {
					if _, has := p.Accounts[idStr]; has {
						matched = true
						break
					}
				}
			}
			if !matched {
				return false
			}
		case "email":
			s, _ := v.(string)
			if p.Email == nil || !strings.Contains(*p.Email, s) {
				return false
			}
		case "name":
			s, _ := v.(string)
			if !strings.Contains(p.Name, s) {
				return false
			}
		case "text":
			s, _ := v.(string)
			desc := ""
			if p.Description != nil {
				desc = *p.Description
			}
			email := ""
			if p.Email != nil {
				email = *p.Email
			}
			if !strings.Contains(p.Name, s) && !strings.Contains(email, s) && !strings.Contains(desc, s) {
				return false
			}
		case "type":
			s, _ := v.(string)
			if p.Type != s {
				return false
			}
		case "timeZone":
			s, _ := v.(string)
			if p.TimeZone == nil || *p.TimeZone != s {
				return false
			}
		}
	}
	return true
}

// MergeNullEventBusyPeriods merges and splits BusyPeriod objects whose "event" property
// is null so that none overlap and any adjacent periods have a gap or differ in
// busyStatus, per draft-ietf-jmap-calendars Section 2.2. Overlapping ranges with
// different busyStatus values resolve by precedence: confirmed > unavailable > tentative.
func MergeNullEventBusyPeriods(periods []*BusyPeriod) []*BusyPeriod {
	var nulls []*BusyPeriod
	for _, p := range periods {
		if p != nil && p.Event == nil {
			nulls = append(nulls, p)
		}
	}
	if len(nulls) == 0 {
		return periods
	}

	sortByStart := make([]*BusyPeriod, len(nulls))
	copy(sortByStart, nulls)
	for i := 0; i < len(sortByStart); i++ {
		for j := i + 1; j < len(sortByStart); j++ {
			if sortByStart[j].UTCStart < sortByStart[i].UTCStart {
				sortByStart[i], sortByStart[j] = sortByStart[j], sortByStart[i]
			}
		}
	}

	merged := make([]*BusyPeriod, 0, len(sortByStart))
	for _, p := range sortByStart {
		if len(merged) == 0 {
			merged = append(merged, p)
			continue
		}
		last := merged[len(merged)-1]
		if p.UTCStart < last.UTCEnd || (p.UTCStart == last.UTCEnd && p.BusyStatus == last.BusyStatus) {
			if p.UTCEnd > last.UTCEnd {
				last.UTCEnd = p.UTCEnd
			}
			last.BusyStatus = busyStatusPrecedence(last.BusyStatus, p.BusyStatus)
		} else {
			merged = append(merged, p)
		}
	}

	var out []*BusyPeriod
	for _, p := range periods {
		if p.Event != nil {
			out = append(out, p)
		}
	}
	out = append(out, merged...)
	return out
}

// busyStatusPrecedence resolves two overlapping busyStatus values per
// draft-ietf-jmap-calendars Section 2.2: confirmed > unavailable > tentative.
func busyStatusPrecedence(a, b string) string {
	for _, s := range []string{"confirmed", "unavailable", "tentative"} {
		if a == s || b == s {
			return s
		}
	}
	return "unavailable"
}

// ParseRFC3339Time parses an RFC 3339 timestamp.
func ParseRFC3339Time(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// CloneCalendarEvent deep-copies a CalendarEvent via JSON round-trip.
func CloneCalendarEvent(ev *CalendarEvent) *CalendarEvent {
	if ev == nil {
		return nil
	}
	bytes, _ := json.Marshal(ev)
	var out CalendarEvent
	_ = json.Unmarshal(bytes, &out)
	return &out
}
