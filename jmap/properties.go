package jmap

import "encoding/json"

// parseProperties extracts the optional "properties" argument per RFC 8620 Section 5.1.
func parseProperties(args map[string]any) []string {
	raw, _ := args["properties"].([]any)
	if len(raw) == 0 {
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

// filterProperties reduces a marshaled object to the requested property names. The "id"
// property is always included per RFC 8620 Section 5.1, even when not requested. The
// original object is returned unchanged when no properties are requested.
func filterProperties(obj any, properties []string) any {
	if len(properties) == 0 {
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
		}
	}
	return out
}

// filterList applies filterProperties to every element of a typed list, preserving nil.
func filterList[T any](list []*T, properties []string) []any {
	if len(properties) == 0 {
		out := make([]any, 0, len(list))
		for _, item := range list {
			out = append(out, item)
		}
		return out
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		out = append(out, filterProperties(item, properties))
	}
	return out
}
