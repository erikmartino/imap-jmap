package jmap

import "strings"

// Principal represents a user, group, resource, or location principal per draft-ietf-jmap-principals.
type Principal struct {
	ID                 Id              `json:"id"`
	Type               string          `json:"type"` // "individual", "group", "resource", "location", "other"
	Name               string          `json:"name"`
	Description        string          `json:"description,omitempty"`
	Email              string          `json:"email,omitempty"`
	CalendarAddress    string          `json:"calendarAddress,omitempty"`
	Members            map[string]bool `json:"members,omitempty"`
	MemberOf           map[string]bool `json:"memberOf,omitempty"`
	Secret             bool            `json:"secret"`
	MayGetAvailability bool            `json:"mayGetAvailability"`
	MayShareWith       bool            `json:"mayShareWith"`
	AccountIDs         map[string]bool `json:"accountIds,omitempty"`
}

// AvailabilityWindow represents a free/busy window per draft-ietf-jmap-principals.
type AvailabilityWindow struct {
	UTCStart       string `json:"utcStart"`
	UTCEnd         string `json:"utcEnd"`
	FreeBusyStatus string `json:"freeBusyStatus"` // "free", "busy", "tentative", "unavailable"
}

// MatchPrincipal checks if a principal matches the given query filter.
func MatchPrincipal(p *Principal, filter map[string]any) bool {
	if filter == nil {
		return true
	}
	if pType, ok := filter["type"].(string); ok && pType != "" {
		if p.Type != pType {
			return false
		}
	}
	if name, ok := filter["name"].(string); ok && name != "" {
		if !strings.Contains(strings.ToLower(p.Name), strings.ToLower(name)) {
			return false
		}
	}
	if email, ok := filter["email"].(string); ok && email != "" {
		if !strings.Contains(strings.ToLower(p.Email), strings.ToLower(email)) {
			return false
		}
	}
	if text, ok := filter["text"].(string); ok && text != "" {
		lText := strings.ToLower(text)
		matchName := strings.Contains(strings.ToLower(p.Name), lText)
		matchEmail := strings.Contains(strings.ToLower(p.Email), lText)
		matchDesc := strings.Contains(strings.ToLower(p.Description), lText)
		if !matchName && !matchEmail && !matchDesc {
			return false
		}
	}
	if accIDs, ok := filter["accountIds"].([]any); ok {
		matched := false
		for _, raw := range accIDs {
			if accStr, ok := raw.(string); ok {
				if p.AccountIDs != nil && p.AccountIDs[accStr] {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
