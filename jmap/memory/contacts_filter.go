package memory

import (
	"encoding/json"
	"strings"

	"imap-jmap/jmap"
)

// decodeJSONField marshals a decode-time value and unmarshals it into a typed
// destination, the same way JSON patches from JMAP /set flow through the server.
func decodeJSONField(src any, dest any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dest)
}

// containsFold reports a case-insensitive substring match.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func matchesCardName(name *jmap.JSContactName, q string) bool {
	if name == nil {
		return false
	}
	if containsFold(name.Full, q) {
		return true
	}
	for _, c := range name.Components {
		if c != nil && containsFold(c.Value, q) {
			return true
		}
	}
	return false
}

func matchesNameKind(name *jmap.JSContactName, kind, q string) bool {
	if name == nil {
		return false
	}
	for _, c := range name.Components {
		if c != nil && c.Kind == kind && containsFold(c.Value, q) {
			return true
		}
	}
	return false
}

func matchesNickname(nicknames map[string]*jmap.JSContactNickname, q string) bool {
	for _, n := range nicknames {
		if n != nil && (containsFold(n.Name, q)) {
			return true
		}
	}
	return false
}

func matchesOrganization(orgs map[string]*jmap.JSContactOrganization, q string) bool {
	for _, o := range orgs {
		if o == nil {
			continue
		}
		if containsFold(o.Name, q) {
			return true
		}
		for _, u := range o.Units {
			if containsFold(u, q) {
				return true
			}
		}
	}
	return false
}

func matchesEmails(emails map[string]*jmap.JSContactEmailAddress, q string) bool {
	for _, e := range emails {
		if e == nil {
			continue
		}
		if containsFold(e.Address, q) || containsFold(e.Label, q) {
			return true
		}
	}
	return false
}

func matchesPhones(phones map[string]*jmap.JSContactPhone, q string) bool {
	for _, p := range phones {
		if p == nil {
			continue
		}
		if containsFold(p.Number, q) || containsFold(p.Label, q) {
			return true
		}
	}
	return false
}

func matchesOnlineService(services map[string]*jmap.JSContactOnlineService, q string) bool {
	for _, s := range services {
		if s == nil {
			continue
		}
		if containsFold(s.Service, q) || containsFold(s.URI, q) {
			return true
		}
	}
	return false
}

func matchesAddresses(addrs map[string]*jmap.JSContactAddress, q string) bool {
	for _, a := range addrs {
		if a == nil {
			continue
		}
		for _, part := range []string{a.Full, a.Street, a.Locality, a.Region, a.Postcode, a.Country, a.CountryCode} {
			if containsFold(part, q) {
				return true
			}
		}
	}
	return false
}

func matchesNotes(notes map[string]*jmap.JSContactNote, q string) bool {
	for _, n := range notes {
		if n != nil && containsFold(n.Note, q) {
			return true
		}
	}
	return false
}

// matchesCardText searches the free-text fields of a Card used by the RFC 9610
// "text" filter condition (title/name, emails, phones, addresses, orgs, notes).
func matchesCardText(card *jmap.Card, q string) bool {
	if matchesCardName(card.Name, q) {
		return true
	}
	if matchesNickname(card.Nicknames, q) {
		return true
	}
	if matchesEmails(card.Emails, q) {
		return true
	}
	if matchesPhones(card.Phones, q) {
		return true
	}
	if matchesAddresses(card.Addresses, q) {
		return true
	}
	if matchesOrganization(card.Organizations, q) {
		return true
	}
	if matchesNotes(card.Notes, q) {
		return true
	}
	for _, t := range card.Titles {
		if t != nil && containsFold(t.Name, q) {
			return true
		}
	}
	return false
}
