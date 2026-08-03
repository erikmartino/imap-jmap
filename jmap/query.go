package jmap

import (
	"fmt"
	"sort"
	"strings"
)

// parseQueryPosition extracts the "position" argument per RFC 8620 Section 5.5: a non-negative
// integer defaulting to 0. A negative value is rejected with an invalidArguments error so no
// query handler can ever index a slice with a negative position.
func parseQueryPosition(args map[string]any) (position int, errMsg string) {
	posVal, ok := args["position"].(float64)
	if !ok {
		return 0, ""
	}
	position = int(posVal)
	if position < 0 {
		return 0, fmt.Sprintf("position must be a non-negative integer, got %v", posVal)
	}
	return position, ""
}

// FilterCondition represents Email/query filter condition properties per RFC 8621 Section 4.5.1.
type FilterCondition struct {
	InMailbox          *Id     `json:"inMailbox,omitempty"`
	InMailboxOtherThan []Id    `json:"inMailboxOtherThan,omitempty"`
	Before             *string `json:"before,omitempty"`
	After              *string `json:"after,omitempty"`
	MinSize            *uint64 `json:"minSize,omitempty"`
	MaxSize            *uint64 `json:"maxSize,omitempty"`
	From               *string `json:"from,omitempty"`
	To                 *string `json:"to,omitempty"`
	CC                 *string `json:"cc,omitempty"`
	BCC                *string `json:"bcc,omitempty"`
	Subject            *string `json:"subject,omitempty"`
	Text               *string `json:"text,omitempty"`
	HasAttachment      *bool   `json:"hasAttachment,omitempty"`
}

// Comparator defines sorting rules per RFC 8621 Section 4.5.2.
type Comparator struct {
	Property    string `json:"property"`
	IsAscending bool   `json:"isAscending"`
	Collation   string `json:"collation,omitempty"`
}

// MatchesFilter checks if an email matches a filter object (FilterCondition or FilterOperator) per RFC 8621 Section 4.5.
func MatchesFilter(em *Email, filter map[string]any) bool {
	if len(filter) == 0 {
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

	// cc
	if ccRaw, ok := filter["cc"].(string); ok && ccRaw != "" {
		if !matchAddresses(em.CC, ccRaw) {
			return false
		}
	}

	// bcc
	if bccRaw, ok := filter["bcc"].(string); ok && bccRaw != "" {
		if !matchAddresses(em.BCC, bccRaw) {
			return false
		}
	}

	// body
	if bodyRaw, ok := filter["body"].(string); ok && bodyRaw != "" {
		if !matchBody(em, bodyRaw) {
			return false
		}
	}

	// hasKeyword
	if kwRaw, ok := filter["hasKeyword"].(string); ok && kwRaw != "" {
		if em.Keywords == nil || !em.Keywords[kwRaw] {
			return false
		}
	}

	// notKeyword
	if notKwRaw, ok := filter["notKeyword"].(string); ok && notKwRaw != "" {
		if em.Keywords != nil && em.Keywords[notKwRaw] {
			return false
		}
	}

	// header
	if headerRaw, ok := filter["header"].([]any); ok && len(headerRaw) > 0 {
		headerName, _ := headerRaw[0].(string)
		headerVal := ""
		if len(headerRaw) > 1 {
			headerVal, _ = headerRaw[1].(string)
		}
		if !matchHeader(em, headerName, headerVal) {
			return false
		}
	}

	// text
	if textRaw, ok := filter["text"].(string); ok && textRaw != "" {
		needle := strings.ToLower(textRaw)
		textMatch := strings.Contains(strings.ToLower(em.Subject), needle) ||
			strings.Contains(strings.ToLower(em.Preview), needle) ||
			matchAddresses(em.From, needle) ||
			matchAddresses(em.To, needle) ||
			matchAddresses(em.CC, needle) ||
			matchAddresses(em.BCC, needle) ||
			matchBody(em, needle)
		if !textMatch {
			return false
		}
	}

	return true
}

func matchBody(em *Email, needle string) bool {
	needle = strings.ToLower(needle)
	if strings.Contains(strings.ToLower(em.Preview), needle) {
		return true
	}
	for _, val := range em.BodyValues {
		if strings.Contains(strings.ToLower(val.Value), needle) {
			return true
		}
	}
	return false
}

func matchHeader(em *Email, headerName, headerValue string) bool {
	headerName = strings.ToLower(headerName)
	headerValue = strings.ToLower(headerValue)

	checkHeaders := func(headers []EmailHeader) bool {
		for _, h := range headers {
			if strings.ToLower(h.Name) == headerName {
				if headerValue == "" || strings.Contains(strings.ToLower(h.Value), headerValue) {
					return true
				}
			}
		}
		return false
	}

	if checkHeaders(em.BodyStructure.Headers) {
		return true
	}
	for _, p := range em.TextBody {
		if checkHeaders(p.Headers) {
			return true
		}
	}
	for _, p := range em.HTMLBody {
		if checkHeaders(p.Headers) {
			return true
		}
	}
	return false
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
