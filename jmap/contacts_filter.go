package jmap

import (
	"sort"
	"strings"
)

// containsFold reports a case-insensitive substring match.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func matchesCardName(name *JSContactName, q string) bool {
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

func matchesNameKind(name *JSContactName, kind, q string) bool {
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

func matchesNickname(nicknames map[string]*JSContactNickname, q string) bool {
	for _, n := range nicknames {
		if n != nil && (containsFold(n.Name, q)) {
			return true
		}
	}
	return false
}

func matchesOrganization(orgs map[string]*JSContactOrganization, q string) bool {
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

func matchesEmails(emails map[string]*JSContactEmailAddress, q string) bool {
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

func matchesPhones(phones map[string]*JSContactPhone, q string) bool {
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

func matchesOnlineService(services map[string]*JSContactOnlineService, q string) bool {
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

func matchesAddresses(addrs map[string]*JSContactAddress, q string) bool {
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

func matchesNotes(notes map[string]*JSContactNote, q string) bool {
	for _, n := range notes {
		if n != nil && containsFold(n.Note, q) {
			return true
		}
	}
	return false
}

// matchesCardText searches the free-text fields of a Card used by the RFC 9610
// "text" filter condition (title/name, emails, phones, addresses, orgs, notes).
func matchesCardText(card *Card, q string) bool {
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

// MatchCard reports whether a card satisfies an RFC 9610 Section 3.3.1 filter condition or FilterOperator.
func MatchCard(card *Card, filter map[string]any) bool {
	if filter == nil {
		return true
	}
	if match, isOp := EvalFilterOperator(filter, func(cond map[string]any) bool {
		return MatchCard(card, cond)
	}); isOp {
		return match
	}

	for k, v := range filter {
		if k == "operator" || k == "conditions" {
			continue
		}
		switch k {
		case "inAddressBook":
			ab, ok := v.(string)
			if !ok || !card.AddressBookIDs[Id(ab)] {
				return false
			}
		case "uid":
			s, _ := v.(string)
			if card.Uid != s {
				return false
			}
		case "hasMember":
			s, _ := v.(string)
			if !card.Members[s] {
				return false
			}
		case "kind":
			s, _ := v.(string)
			if card.Kind != s {
				return false
			}
		case "createdBefore":
			s, _ := v.(string)
			if card.Created == "" || card.Created >= s {
				return false
			}
		case "createdAfter":
			s, _ := v.(string)
			if card.Created == "" || card.Created < s {
				return false
			}
		case "updatedBefore":
			s, _ := v.(string)
			if card.Updated == "" || card.Updated >= s {
				return false
			}
		case "updatedAfter":
			s, _ := v.(string)
			if card.Updated == "" || card.Updated < s {
				return false
			}
		case "name":
			s, _ := v.(string)
			if !matchesCardName(card.Name, s) {
				return false
			}
		case "name/given", "name/surname", "name/surname2":
			s, _ := v.(string)
			if !matchesNameKind(card.Name, strings.TrimPrefix(k, "name/"), s) {
				return false
			}
		case "nickname":
			s, _ := v.(string)
			if !matchesNickname(card.Nicknames, s) {
				return false
			}
		case "organization":
			s, _ := v.(string)
			if !matchesOrganization(card.Organizations, s) {
				return false
			}
		case "email":
			s, _ := v.(string)
			if !matchesEmails(card.Emails, s) {
				return false
			}
		case "phone":
			s, _ := v.(string)
			if !matchesPhones(card.Phones, s) {
				return false
			}
		case "onlineService":
			s, _ := v.(string)
			if !matchesOnlineService(card.OnlineServices, s) {
				return false
			}
		case "address":
			s, _ := v.(string)
			if !matchesAddresses(card.Addresses, s) {
				return false
			}
		case "note":
			s, _ := v.(string)
			if !matchesNotes(card.Notes, s) {
				return false
			}
		case "text":
			s, _ := v.(string)
			if !matchesCardText(card, s) {
				return false
			}
		}
	}
	return true
}

func GetCardNameComponent(card *Card, kind string) string {
	if card == nil || card.Name == nil {
		return ""
	}
	for _, comp := range card.Name.Components {
		if comp != nil && comp.Kind == kind {
			return comp.Value
		}
	}
	return ""
}

func SortCards(cards []*Card, comparators []Comparator) {
	if len(comparators) == 0 {
		return
	}
	sort.SliceStable(cards, func(i, j int) bool {
		c1, c2 := cards[i], cards[j]
		for _, comp := range comparators {
			var v1, v2 string
			switch comp.Property {
			case "created":
				v1, v2 = c1.Created, c2.Created
			case "updated":
				v1, v2 = c1.Updated, c2.Updated
			case "name/given":
				v1 = GetCardNameComponent(c1, "given")
				v2 = GetCardNameComponent(c2, "given")
			case "name/surname":
				v1 = GetCardNameComponent(c1, "surname")
				v2 = GetCardNameComponent(c2, "surname")
			case "name/surname2":
				v1 = GetCardNameComponent(c1, "surname2")
				v2 = GetCardNameComponent(c2, "surname2")
			default:
				continue
			}

			if v1 == v2 {
				continue
			}
			if comp.IsAscending {
				return v1 < v2
			}
			return v1 > v2
		}
		return string(c1.ID) < string(c2.ID)
	})
}
