package jmaphandler

import "encoding/json"

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
