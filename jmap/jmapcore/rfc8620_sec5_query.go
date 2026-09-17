package jmapcore

import (
	"fmt"
	"math"
	"strings"
)

// Comparator defines sorting rules per RFC 8620 Section 5.5.
// @spec RFC8620#5.5-p1-MUST
type Comparator struct {
	Property    string `json:"property"`
	IsAscending bool   `json:"isAscending"`
	Collation   string `json:"collation,omitempty"`
	Keyword     string `json:"keyword,omitempty"`
}

// ParseQueryPosition extracts the "position" argument per RFC 8620 Section 5.5.
// @spec RFC8620#5.5-p1-MUST
func ParseQueryPosition(args map[string]any) (position int, errMsg string) {
	posVal, ok := args["position"].(float64)
	if !ok {
		return 0, ""
	}
	return int(posVal), ""
}

// NormalizePosition applies RFC 8620 Section 5.5 position semantics: negative is offset from end.
// @spec RFC8620#5.5-p1-MUST
func NormalizePosition(position, total int) int {
	if position < 0 {
		position += total
		if position < 0 {
			position = 0
		}
	}
	return position
}

// ParseQueryAnchor extracts the "anchor" and "anchorOffset" arguments per RFC 8620 Section 5.5.
// @spec RFC8620#5.5-p1-MUST
func ParseQueryAnchor(args map[string]any) (anchor string, offset int, errMsg string) {
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

// ApplyQueryAnchor positions a fully filtered/sorted id list per RFC 8620 Section 5.5.
// @spec RFC8620#5.5-p1-MUST
func ApplyQueryAnchor(anchor string, offset int, ids []Id, limit *uint64) (position int, out []Id, found bool) {
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

// AdvertisedCollations are the collation algorithms supported per RFC 8620 Section 5.5:
// "i;ascii-casemap" (case-insensitive, default) and "i;octet" (binary comparison).
// @spec RFC8620#5.5-p2-MUST
var AdvertisedCollations = map[string]bool{"i;ascii-casemap": true, "i;octet": true}

// ParseComparators parses the "sort" argument per RFC 8620 Section 5.5.
// @spec RFC8620#5.5-p2-MUST
func ParseComparators(args map[string]any) []Comparator {
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

// ValidateComparators enforces RFC 8620 Section 5.5 sort validation: unsupported property or
// collation must be rejected with "unsupportedSort", and keyword sorts require a "keyword".
// @spec RFC8620#5.5-p2-MUST
func ValidateComparators(comparators []Comparator, supported map[string]bool) (errType, errMsg string) {
	for _, c := range comparators {
		if !supported[c.Property] {
			return "unsupportedSort", fmt.Sprintf("sort property %q is not supported", c.Property)
		}
		if c.Collation != "" && !AdvertisedCollations[c.Collation] {
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

// ComputeQueryChanges derives the added/removed deltas for /queryChanges per RFC 8620 Section 5.6.
// @spec RFC8620#5.6-p1-MUST
func ComputeQueryChanges(created, updated, destroyed, currentIDs []Id, upToId string) (added []map[string]any, removed []Id) {
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

// EvalFilterOperator evaluates an RFC 8620 Section 5.5 FilterOperator object (contains
// "operator" and "conditions" properties) using the provided matchCondition function for each
// condition. It returns (matched, true) if filter is a FilterOperator, or (false, false) if
// filter is not an operator (i.e. it is a FilterCondition).
// @spec RFC8620#5.5-p3-MUST
func EvalFilterOperator(filter map[string]any, matchCondition func(map[string]any) bool) (bool, bool) {
	if filter == nil {
		return true, false
	}
	opRaw, ok := filter["operator"].(string)
	if !ok {
		return false, false
	}
	var conds []map[string]any
	if rawConds, ok := filter["conditions"].([]any); ok {
		for _, c := range rawConds {
			if cm, ok := c.(map[string]any); ok {
				conds = append(conds, cm)
			}
		}
	} else if rawConds, ok := filter["conditions"].([]map[string]any); ok {
		conds = rawConds
	}

	switch strings.ToUpper(opRaw) {
	case "AND":
		for _, condMap := range conds {
			if !matchCondition(condMap) {
				return false, true
			}
		}
		return true, true
	case "OR":
		for _, condMap := range conds {
			if matchCondition(condMap) {
				return true, true
			}
		}
		return false, true
	case "NOT":
		for _, condMap := range conds {
			if matchCondition(condMap) {
				return false, true
			}
		}
		return true, true
	default:
		return false, true
	}
}
