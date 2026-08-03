package jmap

import "encoding/json"

// mergeCopyOverrides builds the property map for a Foo/copy creation (RFC 8620 Section 5.4).
// It starts from the source object's properties and applies the client-supplied overrides,
// dropping the "id" so the target account assigns a fresh one. Server-set fields the backend
// re-derives (timestamps, ids) are left for the backend to populate on create.
func mergeCopyOverrides(source any, overrides map[string]any) map[string]any {
	merged := make(map[string]any)
	if b, err := json.Marshal(source); err == nil {
		_ = json.Unmarshal(b, &merged)
	}
	for k, v := range overrides {
		if k == "id" {
			continue
		}
		merged[k] = v
	}
	delete(merged, "id")
	return merged
}
