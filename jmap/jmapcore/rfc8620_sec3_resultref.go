package jmapcore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ResultReference represents a result reference object per RFC 8620 Section 3.7.
// @spec RFC8620#3.7-p1-MUST
type ResultReference struct {
	ResultOf string `json:"resultOf"`
	Name     string `json:"name"`
	Path     string `json:"path"`
}

// IsResultReference checks if a value map represents a ResultReference.
// @spec RFC8620#3.7-p1-MUST
func IsResultReference(m map[string]any) bool {
	if m == nil {
		return false
	}
	_, hasResultOf := m["resultOf"]
	_, hasName := m["name"]
	_, hasPath := m["path"]
	return hasResultOf && hasName && hasPath
}

// EvaluateJSONPointer resolves an RFC 6901 JSON pointer against a data structure, extended per
// RFC 8620 Section 3.7: the token "*" maps the rest of the pointer across every element of an
// array, flattening any nested arrays into the output.
// @spec RFC8620#3.7-p2-MUST
func EvaluateJSONPointer(data any, pointer string) (any, error) {
	if pointer == "" {
		return data, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("invalid json pointer: must start with /")
	}

	// Normalize through JSON so Go-typed values (e.g. []*Email, []Id) are addressable as the
	// JSON objects/arrays the JMAP responses expose to clients.
	var normalized any
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize value: %w", err)
	}
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, fmt.Errorf("cannot parse serialized value: %w", err)
	}

	tokens := strings.Split(pointer[1:], "/")
	for i := range tokens {
		// Unescape JSON pointer tokens: ~1 -> /, ~0 -> ~
		tokens[i] = strings.ReplaceAll(tokens[i], "~1", "/")
		tokens[i] = strings.ReplaceAll(tokens[i], "~0", "~")
	}
	return evalPointerTokens(normalized, tokens)
}

func evalPointerTokens(data any, tokens []string) (any, error) {
	if len(tokens) == 0 {
		return data, nil
	}
	token := tokens[0]
	rest := tokens[1:]

	switch v := data.(type) {
	case map[string]any:
		val, ok := v[token]
		if !ok {
			return nil, fmt.Errorf("key %q not found in object", token)
		}
		return evalPointerTokens(val, rest)
	case []any:
		if token == "*" {
			var out []any
			for _, item := range v {
				r, err := evalPointerTokens(item, rest)
				if err != nil {
					return nil, err
				}
				if arr, ok := r.([]any); ok {
					out = append(out, arr...)
				} else {
					out = append(out, r)
				}
			}
			return out, nil
		}
		idx, err := strconv.Atoi(token)
		if err != nil || idx < 0 || idx >= len(v) {
			return nil, fmt.Errorf("array index %q out of bounds", token)
		}
		return evalPointerTokens(v[idx], rest)
	default:
		return nil, fmt.Errorf("cannot evaluate pointer token %q on type %T", token, data)
	}
}
