package jmapcore

import (
	"fmt"
	"math"
)

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
