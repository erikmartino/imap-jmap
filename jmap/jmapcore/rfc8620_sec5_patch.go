package jmapcore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// PatchObject represents a JSON Pointer patch map per RFC 8620 Section 5.3.
// Keys are JSON pointers (e.g. "keywords/$flag" or "parentId") with an implicit leading slash,
// and values are target replacement values.
// @spec RFC8620#5.3-p11-MUST
type PatchObject map[string]any

// ParseJSONPointerTokens parses a JSON Pointer string with an implicit leading slash
// per RFC 8620 Section 5.3.
func ParseJSONPointerTokens(path string) []string {
	p := path
	if strings.HasPrefix(p, "/") {
		p = p[1:]
	}
	parts := strings.Split(p, "/")
	tokens := make([]string, len(parts))
	for i, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		tokens[i] = part
	}
	return tokens
}

// ValidatePatch validates a PatchObject against an existing object map per RFC 8620 Section 5.3.
// If any RFC 8620 Section 5.3 restrictions are violated, a *SetError is returned.
// Returns nil if the patch is valid.
func ValidatePatch(target map[string]any, patch map[string]any, serverSetProperties []string) *SetError {
	if len(patch) == 0 {
		return nil
	}

	// 1. Parse tokens for each patch path
	parsed := make(map[string][]string, len(patch))
	for path := range patch {
		parsed[path] = ParseJSONPointerTokens(path)
	}

	// 2. Check for prefix conflicts among patches (RFC 8620 Section 5.3 [p11])
	// "There MUST NOT be two patches in the PatchObject where the pointer of one
	// is the prefix of the pointer of the other, e.g., "alerts/1/offset" and "alerts"."
	for pathA, tokensA := range parsed {
		for pathB, tokensB := range parsed {
			if pathA == pathB {
				continue
			}
			if len(tokensA) < len(tokensB) {
				isPrefix := true
				for i := range tokensA {
					if tokensA[i] != tokensB[i] {
						isPrefix = false
						break
					}
				}
				if isPrefix {
					return &SetError{
						Type:        "invalidPatch",
						Description: fmt.Sprintf("prefix conflict: %q is a prefix of %q", pathA, pathB),
					}
				}
			}
		}
	}

	// 3. Check path restrictions against target (RFC 8620 Section 5.3 [p8-p10])
	// - "The pointer MUST NOT reference inside an array (i.e., you MUST NOT insert/delete from an array;
	//   the array MUST be replaced in its entirety instead)."
	// - "All parts prior to the last (i.e., the value after the final slash) MUST already exist on the object being patched."
	for rawPath, tokens := range parsed {
		var cur any = target
		for i, tok := range tokens {
			isLast := (i == len(tokens) - 1)
			if _, isSlice := cur.([]any); isSlice {
				return &SetError{
					Type:        "invalidPatch",
					Description: fmt.Sprintf("pointer %q references inside an array at token %q", rawPath, tok),
				}
			}
			m, isMap := cur.(map[string]any)
			if !isMap {
				return &SetError{
					Type:        "invalidPatch",
					Description: fmt.Sprintf("pointer %q part %q does not exist on non-object", rawPath, tok),
				}
			}
			val, exists := m[tok]
			if !isLast {
				if !exists || val == nil {
					return &SetError{
						Type:        "invalidPatch",
						Description: fmt.Sprintf("all parts prior to the last must exist; token %q not found for %q", tok, rawPath),
					}
				}
				if _, isSlice := val.([]any); isSlice {
					return &SetError{
						Type:        "invalidPatch",
						Description: fmt.Sprintf("pointer %q references inside an array at token %q", rawPath, tok),
					}
				}
				cur = val
			}
		}
	}

	// 4. Server-set properties check (RFC 8620 Section 5.3 [p14])
	// "Any server-set properties MAY be included in the patch if their value is identical
	// to the current server value (before applying the patches to the object). Otherwise,
	// the update MUST be rejected with an "invalidProperties" SetError."
	for _, prop := range serverSetProperties {
		for rawPath, patchVal := range patch {
			tokens := parsed[rawPath]
			if len(tokens) == 1 && tokens[0] == prop {
				targetVal, hasTarget := target[prop]
				if !hasTarget || !areEqualValues(patchVal, targetVal) {
					return &SetError{
						Type:        "invalidProperties",
						Properties:  []string{prop},
						Description: fmt.Sprintf("cannot modify server-set property %q", prop),
					}
				}
			}
		}
	}

	return nil
}

func areEqualValues(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	if fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b) {
		return true
	}
	aJSON, errA := json.Marshal(a)
	bJSON, errB := json.Marshal(b)
	if errA == nil && errB == nil && bytes.Equal(aJSON, bJSON) {
		return true
	}
	return false
}

// ApplyPatch applies a PatchObject to a target map structure in-place / returning updated structure.
// Handles both top-level property replacements and JSON Pointer nested key paths.
// @spec RFC8620#5.3-p12-MUST
func ApplyPatch(target map[string]any, patch PatchObject) error {
	for path, val := range patch {
		tokens := ParseJSONPointerTokens(path)
		if len(tokens) == 1 {
			if val == nil {
				delete(target, tokens[0])
			} else {
				target[tokens[0]] = val
			}
			continue
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
