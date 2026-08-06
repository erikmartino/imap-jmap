package jmap

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// parseQueryPosition extracts the "position" argument per RFC 8620 Section 5.5: an integer
// defaulting to 0. A negative value is an offset from the end of the results (it is added to
// the total and clamped to 0, see NormalizePosition), so it is returned verbatim.
func parseQueryPosition(args map[string]any) (position int, errMsg string) {
	posVal, ok := args["position"].(float64)
	if !ok {
		return 0, ""
	}
	return int(posVal), ""
}

// NormalizePosition applies RFC 8620 Section 5.5 position semantics: a negative position is
// an offset from the end of the results — it is added to the total, and if still negative,
// clamped to 0.
func NormalizePosition(position, total int) int {
	if position < 0 {
		position += total
		if position < 0 {
			position = 0
		}
	}
	return position
}

// parseQueryAnchor extracts the "anchor" and "anchorOffset" arguments per RFC 8620
// Section 5.5. "anchor" must be an Id (string); "anchorOffset" defaults to 0, may be
// negative, and must be an integer. If no anchor is supplied, any anchorOffset argument
// is ignored. An empty anchor string is treated as absent.
func parseQueryAnchor(args map[string]any) (anchor string, offset int, errMsg string) {
	anchorRaw, hasAnchor := args["anchor"]
	if !hasAnchor || anchorRaw == nil {
		return "", 0, ""
	}
	anchor, ok := anchorRaw.(string)
	if !ok {
		return "", 0, fmt.Sprintf("anchor must be an Id (string), got %v", anchorRaw)
	}
	if offsetRaw, ok := args["anchorOffset"].(float64); ok {
		if offsetRaw != math.Trunc(offsetRaw) {
			return "", 0, fmt.Sprintf("anchorOffset must be an integer, got %v", offsetRaw)
		}
		offset = int(offsetRaw)
	}
	return anchor, offset, ""
}

// applyQueryAnchor positions a fully filtered and sorted id list per RFC 8620 Section 5.5:
// the index of the anchor within the results plus anchorOffset is used exactly as though it
// were the "position" argument (so a negative result is an offset from the end, clamped to
// 0), then the limit slices from there. It returns false when the anchor is not in the
// results; the caller MUST then reject the call with an anchorNotFound error.
func applyQueryAnchor(anchor string, offset int, ids []Id, limit *uint64) (position int, out []Id, found bool) {
	anchorIdx := -1
	for i, id := range ids {
		if string(id) == anchor {
			anchorIdx = i
			break
		}
	}
	if anchorIdx == -1 {
		return 0, nil, false
	}
	position = NormalizePosition(anchorIdx+offset, len(ids))
	end := len(ids)
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}
	if position >= end {
		out = []Id{}
	} else {
		out = ids[position:end]
	}
	return position, out, true
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

// Comparator defines sorting rules per RFC 8621 Section 4.4.2.
type Comparator struct {
	Property    string `json:"property"`
	IsAscending bool   `json:"isAscending"`
	Collation   string `json:"collation,omitempty"`
	Keyword     string `json:"keyword,omitempty"`
}

// parseComparators parses the "sort" argument per RFC 8621 Section 4.5.2.
func parseComparators(args map[string]any) []Comparator {
	var comparators []Comparator
	if sortRaw, ok := args["sort"].([]any); ok {
		for _, item := range sortRaw {
			if compMap, ok := item.(map[string]any); ok {
				prop, _ := compMap["property"].(string)
				asc, isBool := compMap["isAscending"].(bool)
				if !isBool {
					asc = true
				}
				coll, _ := compMap["collation"].(string)
				kw, _ := compMap["keyword"].(string)
				comparators = append(comparators, Comparator{
					Property:    prop,
					IsAscending: asc,
					Collation:   coll,
					Keyword:     kw,
				})
			}
		}
	}
	return comparators
}

// advertisedCollations are the collation algorithms the server supports (RFC 8620
// Section 5.5): "i;ascii-casemap" (case-insensitive, the default) and "i;octet"
// (case-sensitive binary comparison).
var advertisedCollations = map[string]bool{"i;ascii-casemap": true, "i;octet": true}

// validateComparators enforces RFC 8620 Section 5.5 sort validation: a comparator naming an
// unsupported property or collation must be rejected with an "unsupportedSort" error, and a
// keyword sort without its required "keyword" property is invalid (RFC 8621 Section 4.4.2).
// It returns an empty string when the sort is acceptable.
func validateComparators(comparators []Comparator, supported map[string]bool) (errType, errMsg string) {
	for _, c := range comparators {
		if !supported[c.Property] {
			return "unsupportedSort", fmt.Sprintf("sort property %q is not supported", c.Property)
		}
		if c.Collation != "" && !advertisedCollations[c.Collation] {
			return "unsupportedSort", fmt.Sprintf("collation %q is not supported", c.Collation)
		}
		switch c.Property {
		case "hasKeyword", "allInThreadHaveKeyword", "someInThreadHaveKeyword":
			if c.Keyword == "" {
				return MethodErrorInvalidArguments, fmt.Sprintf("sort property %q requires a \"keyword\" property", c.Property)
			}
		}
	}
	return "", ""
}

// computeQueryChanges derives the added/removed deltas for /queryChanges per RFC 8620
// Section 5.6: destroyed and updated objects are removed from the client's view, and any
// created or updated object still matching the query's filter is re-added at its real
// position in the current filtered and sorted result set (so the added array is sorted by
// index with the lowest first). When an "upToId" is supplied and exists in the results,
// added ids with a higher index than the anchor, and updated ids re-added beyond it, are
// omitted because the client has not cached past that point.
func computeQueryChanges(created, updated, destroyed, currentIDs []Id, upToId string) (added []map[string]any, removed []Id) {
	position := make(map[Id]int, len(currentIDs))
	for i, id := range currentIDs {
		position[id] = i
	}

	isChanged := make(map[Id]bool, len(created)+len(updated))
	for _, id := range created {
		isChanged[id] = true
	}
	for _, id := range updated {
		isChanged[id] = true
	}

	upToIndex := -1
	if upToId != "" {
		// upToId only truncates when it exists in the results; a missing id must leave the
		// deltas untouched (RFC 8620 Section 5.6: "exists in the results").
		if idx, ok := position[Id(upToId)]; ok {
			upToIndex = idx
		}
	}

	added = make([]map[string]any, 0, len(created)+len(updated))
	for _, id := range currentIDs {
		if isChanged[id] {
			if upToIndex >= 0 && position[id] > upToIndex {
				continue
			}
			added = append(added, map[string]any{"id": id, "index": position[id]})
		}
	}

	removed = make([]Id, 0, len(updated)+len(destroyed))
	removed = append(removed, destroyed...)
	for _, id := range updated {
		idx, isCurrent := position[id]
		if upToIndex >= 0 && isCurrent && idx > upToIndex {
			continue
		}
		removed = append(removed, id)
	}
	if removed == nil {
		removed = []Id{}
	}
	return added, removed
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

// emailSortableProperties is the set of Email properties the server supports sorting on
// (RFC 8621 Section 4.4.2): receivedAt (MUST) plus size, from, to, subject, sentAt,
// hasKeyword, allInThreadHaveKeyword and someInThreadHaveKeyword (SHOULD).
var emailSortableProperties = map[string]bool{
	"receivedAt": true, "size": true, "from": true, "to": true, "subject": true,
	"sentAt": true, "hasKeyword": true, "allInThreadHaveKeyword": true,
	"someInThreadHaveKeyword": true,
}

// emailMutableFilterProperties are the Email filter condition properties whose values can
// change after creation (RFC 8621 Section 4.1: "mailboxIds" and "keywords" are the only
// client-updatable Email properties), so a query filtered on them is not over immutable
// properties (RFC 8620 Section 5.6). All other filter conditions (before, after, minSize,
// maxSize, hasAttachment, from, to, cc, bcc, subject, body, text, header) are immutable.
var emailMutableFilterProperties = map[string]bool{
	"inMailbox": true, "inMailboxOtherThan": true, "hasKeyword": true, "notKeyword": true,
}

// emailMutableSortProperties are the Email sort properties that depend on mutable state:
// keywords are updatable, so keyword sorts are not over immutable properties (RFC 8620
// Section 5.6). All other Email sort properties (receivedAt, size, from, to, subject,
// sentAt) are fixed at creation and never change.
var emailMutableSortProperties = map[string]bool{
	"hasKeyword": true, "allInThreadHaveKeyword": true, "someInThreadHaveKeyword": true,
}

// upToIdTruncationApplicable reports whether the "upToId" argument may be honored for a
// /queryChanges call per RFC 8620 Section 5.6: the server may omit added/removed ids with
// a higher index than the anchor only when the query's filter and sort are both over
// immutable properties — "if they are not immutable, this argument is ignored". Filter
// Operator conditions are examined recursively; properties absent from the mutable sets are
// treated as immutable.
func upToIdTruncationApplicable(filter map[string]any, comparators []Comparator, mutableFilter, mutableSort map[string]bool) bool {
	for k, v := range filter {
		switch k {
		case "operator":
			continue
		case "conditions":
			if conds, ok := v.([]any); ok {
				for _, raw := range conds {
					if cond, ok := raw.(map[string]any); ok && !upToIdTruncationApplicable(cond, nil, mutableFilter, nil) {
						return false
					}
				}
			}
		default:
			if mutableFilter[k] {
				return false
			}
		}
	}
	for _, c := range comparators {
		if mutableSort[c.Property] {
			return false
		}
	}
	return true
}

// SortEmails sorts emails in-place using RFC 8621 Section 4.4.2 comparators. Keyword sort
// properties ("hasKeyword", "allInThreadHaveKeyword", "someInThreadHaveKeyword") require a
// "keyword" property on the Comparator. Thread sorts evaluate the keyword over the threads
// present in the given list; queries that must evaluate over the full store use
// sortEmailsWithContext.
func SortEmails(emails []*Email, comparators []Comparator) {
	threadHas := make(map[string]bool)
	threadLacks := make(map[string]bool)
	for _, em := range emails {
		for _, c := range comparators {
			if c.Property != "allInThreadHaveKeyword" && c.Property != "someInThreadHaveKeyword" {
				continue
			}
			key := threadKeywordKey(em.ThreadID, c.Keyword)
			if hasKeyword(em, c.Keyword) {
				threadHas[key] = true
			} else {
				threadLacks[key] = true
			}
		}
	}
	all := make(map[string]bool, len(threadHas))
	for key := range threadHas {
		all[key] = !threadLacks[key]
	}
	SortEmailsWithContext(emails, comparators, all, threadHas)
}

func threadKeywordKey(threadID Id, keyword string) string {
	return string(threadID) + "\x00" + keyword
}

// SortEmailsWithContext sorts emails per the comparators, using precomputed per-thread
// keyword answers: all[thread\x00keyword] is true when every Email in the thread has the
// keyword, any[...] when at least one has it.
func SortEmailsWithContext(emails []*Email, comparators []Comparator, all, any map[string]bool) {
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
				cmp = compareStrings(baseSubject(a.Subject), baseSubject(b.Subject), comp.Collation)
			case "size":
				if a.Size < b.Size {
					cmp = -1
				} else if a.Size > b.Size {
					cmp = 1
				}
			case "from":
				cmp = compareStrings(firstAddress(a.From), firstAddress(b.From), comp.Collation)
			case "to":
				cmp = compareStrings(firstAddress(a.To), firstAddress(b.To), comp.Collation)
			case "hasKeyword":
				cmp = compareBools(hasKeyword(a, comp.Keyword), hasKeyword(b, comp.Keyword))
			case "allInThreadHaveKeyword":
				cmp = compareBools(all[threadKeywordKey(a.ThreadID, comp.Keyword)], all[threadKeywordKey(b.ThreadID, comp.Keyword)])
			case "someInThreadHaveKeyword":
				cmp = compareBools(any[threadKeywordKey(a.ThreadID, comp.Keyword)], any[threadKeywordKey(b.ThreadID, comp.Keyword)])
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

// baseSubject returns the RFC 5256 Section 2.1 "base subject" that RFC 8621 Section 4.4.2
// uses for "subject" sorting: remove a trailing "(fwd)", then repeatedly strip any leading
// whitespace, bracketed list tag ("[tag]"), and reply/forward prefixes ("re:", "fwd:",
// "fw:" and their bracketed counter forms like "fwd[2]:"), case-insensitively.
func baseSubject(s string) string {
	t := strings.TrimSpace(s)
	if len(t) >= 5 && strings.EqualFold(t[len(t)-5:], "(fwd)") {
		t = strings.TrimSpace(t[:len(t)-5])
	}
	for {
		trimmed := strings.TrimLeft(t, " \t")
		lower := strings.ToLower(trimmed)
		stripped := false
		for _, tag := range []string{"re", "fwd", "fw"} {
			if strings.HasPrefix(lower, tag+":") {
				t = trimmed[len(tag)+1:]
				stripped = true
				break
			}
			counter := tag + "["
			if strings.HasPrefix(lower, counter) {
				if end := strings.IndexByte(lower[len(counter):], ']'); end >= 0 {
					contentStart := len(counter) + end + 1
					if contentStart < len(lower) && lower[contentStart] == ':' {
						t = trimmed[contentStart+1:]
						stripped = true
						break
					}
				}
			}
		}
		if !stripped && len(trimmed) > 0 && trimmed[0] == '[' {
			if end := strings.IndexByte(trimmed, ']'); end >= 0 {
				t = trimmed[end+1:]
				stripped = true
			}
		}
		if !stripped {
			return trimmed
		}
	}
}

// compareStrings compares two strings per the comparator's collation: "i;octet" is a
// case-sensitive binary comparison; any other (or default) collation is case-insensitive.
func compareStrings(a, b, collation string) int {
	if collation == "i;octet" {
		return strings.Compare(a, b)
	}
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

// compareBools orders false before true for ascending sorts (RFC 8620 Section 5.5).
func compareBools(a, b bool) int {
	if a == b {
		return 0
	}
	if !a {
		return -1
	}
	return 1
}

// firstAddress returns the "name" property of the first EmailAddress, or its "email"
// property when the name is null/empty, or the empty string when there is none (RFC 8621
// Section 4.4.2).
func firstAddress(addrs []EmailAddress) string {
	if len(addrs) == 0 {
		return ""
	}
	if addrs[0].Name != "" {
		return addrs[0].Name
	}
	return addrs[0].Email
}

// hasKeyword reports whether the Email carries the keyword.
func hasKeyword(em *Email, keyword string) bool {
	return em.Keywords != nil && em.Keywords[keyword]
}
