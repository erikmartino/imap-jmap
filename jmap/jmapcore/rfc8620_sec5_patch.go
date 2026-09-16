package jmapcore

import (
	"fmt"
	"strings"
)

// PatchObject represents a JSON Pointer patch map per RFC 8620 Section 5.3.
// Keys are JSON pointers (e.g. "keywords/$flag" or "parentId") and values are target replacement values.
// @spec RFC8620#5.3-p11-MUST
type PatchObject map[string]any

// ApplyPatch applies a PatchObject to a target map structure in-place / returning updated structure.
// Handles both top-level property replacements and JSON Pointer nested key paths.
// @spec RFC8620#5.3-p12-MUST
func ApplyPatch(target map[string]any, patch PatchObject) error {
	for path, val := range patch {
		if !strings.HasPrefix(path, "/") {
			// Top-level property replacement
			if val == nil {
				delete(target, path)
			} else {
				target[path] = val
			}
			continue
		}

		// Nested JSON Pointer path (e.g. "/keywords/$flag")
		tokens := strings.Split(path[1:], "/")
		for i := range tokens {
			tokens[i] = strings.ReplaceAll(tokens[i], "~1", "/")
			tokens[i] = strings.ReplaceAll(tokens[i], "~0", "~")
		}

		if err := setPointerToken(target, tokens, val); err != nil {
			return fmt.Errorf("failed to apply patch path %q: %w", path, err)
		}
	}
	return nil
}

func setPointerToken(current any, tokens []string, val any) error {
	if len(tokens) == 0 {
		return nil
	}
	token := tokens[0]

	m, ok := current.(map[string]any)
	if !ok {
		return fmt.Errorf("cannot step into non-object target for token %q", token)
	}

	if len(tokens) == 1 {
		if val == nil {
			delete(m, token)
		} else {
			m[token] = val
		}
		return nil
	}

	sub, exists := m[token]
	if !exists || sub == nil {
		subMap := make(map[string]any)
		m[token] = subMap
		sub = subMap
	}

	return setPointerToken(sub, tokens[1:], val)
}
