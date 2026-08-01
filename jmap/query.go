package jmap

import (
	"sort"
	"strings"
)

// FilterCondition represents Email/query filter condition properties per RFC 8621 Section 4.5.1.
type FilterCondition struct {
	InMailbox          *Id       `json:"inMailbox,omitempty"`
	InMailboxOtherThan []Id      `json:"inMailboxOtherThan,omitempty"`
	Before             *string   `json:"before,omitempty"`
	After              *string   `json:"after,omitempty"`
	MinSize            *uint64   `json:"minSize,omitempty"`
	MaxSize            *uint64   `json:"maxSize,omitempty"`
	From               *string   `json:"from,omitempty"`
	To                 *string   `json:"to,omitempty"`
	CC                 *string   `json:"cc,omitempty"`
	BCC                *string   `json:"bcc,omitempty"`
	Subject            *string   `json:"subject,omitempty"`
	Text               *string   `json:"text,omitempty"`
	HasAttachment      *bool     `json:"hasAttachment,omitempty"`
}

// Comparator defines sorting rules per RFC 8621 Section 4.5.2.
type Comparator struct {
	Property    string `json:"property"`
	IsAscending bool   `json:"isAscending"`
	Collation   string `json:"collation,omitempty"`
}

// MatchesFilter checks if an email matches a filter object (FilterCondition or FilterOperator) per RFC 8621 Section 4.5.
func MatchesFilter(em *Email, filter map[string]any) bool {
	if filter == nil || len(filter) == 0 {
		return true
	}

	// Check if this is a FilterOperator object (contains "operator" key)
	if opRaw, ok := filter["operator"].(string); ok {
		condsRaw, ok := filter["conditions"].([]any)
		if !ok {
			return true
		}

		op := strings.ToUpper(opRaw)
		switch op {
		case "AND":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					if !MatchesFilter(em, condMap) {
						return false
					}
				}
			}
			return true

		case "OR":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					if MatchesFilter(em, condMap) {
						return true
					}
				}
			}
			return len(condsRaw) == 0

		case "NOT":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					if MatchesFilter(em, condMap) {
						return false
					}
				}
			}
			return true
		}
	}

	// Evaluate FilterCondition properties

	// inMailbox
	if inMbRaw, ok := filter["inMailbox"].(string); ok && inMbRaw != "" {
		if !em.MailboxIDs[Id(inMbRaw)] {
			return false
		}
	}

	// inMailboxOtherThan
	if otherRaw, ok := filter["inMailboxOtherThan"].([]any); ok && len(otherRaw) > 0 {
		excludeMap := make(map[Id]bool, len(otherRaw))
		for _, v := range otherRaw {
			if s, ok := v.(string); ok {
				excludeMap[Id(s)] = true
			}
		}
		inOther := false
		for mbID := range em.MailboxIDs {
			if !excludeMap[mbID] {
				inOther = true
				break
			}
		}
		if !inOther {
			return false
		}
	}

	// before
	if beforeRaw, ok := filter["before"].(string); ok && beforeRaw != "" {
		if em.ReceivedAt >= beforeRaw {
			return false
		}
	}

	// after
	if afterRaw, ok := filter["after"].(string); ok && afterRaw != "" {
		if em.ReceivedAt < afterRaw {
			return false
		}
	}

	// minSize
	if minSizeRaw, ok := filter["minSize"].(float64); ok {
		if float64(em.Size) < minSizeRaw {
			return false
		}
	}

	// maxSize
	if maxSizeRaw, ok := filter["maxSize"].(float64); ok {
		if float64(em.Size) > maxSizeRaw {
			return false
		}
	}

	// hasAttachment
	if hasAttRaw, ok := filter["hasAttachment"].(bool); ok {
		if em.HasAttachment != hasAttRaw {
			return false
		}
	}

	// subject
	if subjRaw, ok := filter["subject"].(string); ok && subjRaw != "" {
		if !strings.Contains(strings.ToLower(em.Subject), strings.ToLower(subjRaw)) {
			return false
		}
	}

	// from
	if fromRaw, ok := filter["from"].(string); ok && fromRaw != "" {
		if !matchAddresses(em.From, fromRaw) {
			return false
		}
	}

	// to
	if toRaw, ok := filter["to"].(string); ok && toRaw != "" {
		if !matchAddresses(em.To, toRaw) {
			return false
		}
	}

	// text
	if textRaw, ok := filter["text"].(string); ok && textRaw != "" {
		needle := strings.ToLower(textRaw)
		textMatch := strings.Contains(strings.ToLower(em.Subject), needle) ||
			strings.Contains(strings.ToLower(em.Preview), needle) ||
			matchAddresses(em.From, needle) ||
			matchAddresses(em.To, needle)
		if !textMatch {
			return false
		}
	}

	return true
}

func matchAddresses(addrs []EmailAddress, needle string) bool {
	needle = strings.ToLower(needle)
	for _, addr := range addrs {
		if strings.Contains(strings.ToLower(addr.Name), needle) ||
			strings.Contains(strings.ToLower(addr.Email), needle) {
			return true
		}
	}
	return false
}

// SortEmails sorts emails in-place using RFC 8621 Section 4.5.2 comparators.
func SortEmails(emails []*Email, comparators []Comparator) {
	if len(comparators) == 0 {
		// Default sort: receivedAt descending
		comparators = []Comparator{
			{Property: "receivedAt", IsAscending: false},
		}
	}

	sort.SliceStable(emails, func(i, j int) bool {
		a, b := emails[i], emails[j]
		for _, comp := range comparators {
			var cmp int
			switch comp.Property {
			case "receivedAt":
				cmp = strings.Compare(a.ReceivedAt, b.ReceivedAt)
			case "sentAt":
				cmp = strings.Compare(a.SentAt, b.SentAt)
			case "subject":
				cmp = strings.Compare(strings.ToLower(a.Subject), strings.ToLower(b.Subject))
			case "size":
				if a.Size < b.Size {
					cmp = -1
				} else if a.Size > b.Size {
					cmp = 1
				}
			}

			if cmp != 0 {
				if !comp.IsAscending {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return i < j
	})
}
