package jmaphandler

import (
	"encoding/json"

	"imap-jmap/jmap/jmapcore"
)

// ParseProperties extracts the optional "properties" argument per RFC 8620 Section 5.1.
// If absent or nil, it returns nil (meaning "all properties").
// If present as an array (even if empty), it returns a non-nil slice.
func ParseProperties(args map[string]any) []string {
	rawVal, ok := args["properties"]
	if !ok || rawVal == nil {
		return nil
	}
	raw, ok := rawVal.([]any)
	if !ok {
		return nil
	}
	props := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			props = append(props, s)
		}
	}
	return props
}

// ValidateProperties checks that every requested property in props is in allowed or matches custom.
// If an invalid property is found, it returns (false, invalidPropName).
func ValidateProperties(props []string, allowed map[string]bool, custom func(string) bool) (bool, string) {
	if props == nil {
		return true, ""
	}
	for _, p := range props {
		if allowed != nil && allowed[p] {
			continue
		}
		if custom != nil && custom(p) {
			continue
		}
		return false, p
	}
	return true, ""
}

// ParseIDs extracts the optional "ids" argument per RFC 8620 Section 5.1.
// If absent or nil, it returns (nil, false) meaning "all records".
// If present as an array, it returns deduplicated IDs preserving first-seen order, and true.
// Duplicate IDs in the request are returned at most once per RFC 8620 Section 5.1 [p5].
func ParseIDs(args map[string]any) ([]jmapcore.Id, bool) {
	rawVal, ok := args["ids"]
	if !ok || rawVal == nil {
		return nil, false
	}
	raw, ok := rawVal.([]any)
	if !ok {
		return nil, false
	}
	seen := make(map[jmapcore.Id]struct{}, len(raw))
	ids := make([]jmapcore.Id, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			id := jmapcore.Id(s)
			if _, exists := seen[id]; !exists {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	return ids, true
}

// ParseMaxChanges extracts and validates the optional "maxChanges" argument per RFC 8620 Section 5.2.
// If absent or nil, it returns (nil, nil) allowing the server default.
// If present, it MUST be a positive integer greater than 0. Otherwise it returns an invalidArguments error map.
func ParseMaxChanges(args map[string]any) (*uint64, map[string]any) {
	rawMC, ok := args["maxChanges"]
	if !ok || rawMC == nil {
		return nil, nil
	}
	mc, ok := rawMC.(float64)
	if !ok || mc <= 0 || mc != float64(uint64(mc)) {
		return nil, jmapcore.InvalidArgumentsErrorArgs([]string{"maxChanges"}, "maxChanges must be a positive integer greater than 0")
	}
	m := uint64(mc)
	return &m, nil
}

// FilterProperties reduces a marshaled object to the requested property names. The "id"
// property is always included per RFC 8620 Section 5.1, even when not requested. The
// original object is returned unchanged when properties is nil.
func FilterProperties(obj any, properties []string) any {
	if properties == nil {
		return obj
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return obj
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return obj
	}
	out := make(map[string]any, len(properties)+1)
	if id, ok := m["id"]; ok {
		out["id"] = id
	}
	for _, p := range properties {
		if v, ok := m[p]; ok {
			out[p] = v
		} else {
			out[p] = nil
		}
	}
	return out
}

// FilterList applies FilterProperties to every element of a typed list, preserving nil.
func FilterList[T any](list []*T, properties []string) []any {
	if properties == nil {
		out := make([]any, 0, len(list))
		for _, item := range list {
			out = append(out, item)
		}
		return out
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		out = append(out, FilterProperties(item, properties))
	}
	return out
}
