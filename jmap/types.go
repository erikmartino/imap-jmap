package jmap

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrNotFound is returned by backend update methods when the referenced id does not
// exist. Handlers MUST map it to a "notFound" SetError per RFC 8620 Section 5.3.
var ErrNotFound = errors.New("not found")

// Id represents a JMAP Id as defined in RFC 8620 Section 1.6 & 1.7.5.
type Id string

var idRegexp = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)

// Validate checks if the Id matches RFC 8620 Section 1.6 rules.
func (id Id) Validate() bool {
	return idRegexp.MatchString(string(id))
}

// Invocation represents a JMAP Invocation array tuple: [name, args, clientCallId]
// as defined in RFC 8620 Section 3.2.
type Invocation struct {
	Name         string
	Args         map[string]any
	ClientCallID string
}

// MarshalJSON implements json.Marshaler for Invocation.
func (inv Invocation) MarshalJSON() ([]byte, error) {
	args := inv.Args
	if args == nil {
		args = make(map[string]any)
	}
	return json.Marshal([]any{inv.Name, args, inv.ClientCallID})
}

// UnmarshalJSON implements json.Unmarshaler for Invocation.
func (inv *Invocation) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) != 3 {
		return fmt.Errorf("invocation must be a 3-element array, got %d elements", len(raw))
	}

	if err := json.Unmarshal(raw[0], &inv.Name); err != nil {
		return fmt.Errorf("invalid method name: %w", err)
	}

	if err := json.Unmarshal(raw[1], &inv.Args); err != nil {
		return fmt.Errorf("invalid method args: %w", err)
	}

	if err := json.Unmarshal(raw[2], &inv.ClientCallID); err != nil {
		return fmt.Errorf("invalid client call ID: %w", err)
	}

	return nil
}

// Request represents a JMAP Request object per RFC 8620 Section 3.1.
type Request struct {
	Using       []string          `json:"using"`
	MethodCalls []Invocation      `json:"methodCalls"`
	CreatedIds  map[string]string `json:"createdIds,omitempty"`
}

// Response represents a JMAP Response object per RFC 8620 Section 3.5.
type Response struct {
	MethodResponses []Invocation      `json:"methodResponses"`
	CreatedIds      map[string]string `json:"createdIds,omitempty"`
	SessionState    string            `json:"sessionState"`
}

// RequestError represents a Problem Details object for JMAP Request errors per RFC 8620 Section 3.6.1.
type RequestError struct {
	Type   string `json:"type"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

const (
	ErrorInvalidJSON       = "urn:ietf:params:jmap:error:invalidJSON"
	ErrorUnknownCapability = "urn:ietf:params:jmap:error:unknownCapability"
	ErrorNotRequest        = "urn:ietf:params:jmap:error:notRequest"
	ErrorLimit             = "urn:ietf:params:jmap:error:limit"
)

// MethodErrorArgs returns argument map for a standard method error per RFC 8620 Section 3.6.2.
func MethodErrorArgs(errType string, description string) map[string]any {
	args := map[string]any{
		"type": errType,
	}
	if description != "" {
		args["description"] = description
	}
	return args
}

// SetError defines error object for /set methods per RFC 8620 Section 5.3.
type SetError struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

func (e SetError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", e.Type, e.Description)
	}
	return e.Type
}

const (
	MethodErrorUnknownMethod          = "unknownMethod"
	MethodErrorInvalidArguments       = "invalidArguments"
	MethodErrorInvalidResultReference = "invalidResultReference"
	MethodErrorUnknownDataType        = "unknownDataType"
	MethodErrorAnchorNotFound         = "anchorNotFound"
	MethodErrorAccountNotFound        = "accountNotFound"
)

// ResultReference represents a result reference object per RFC 8620 Section 3.7.
type ResultReference struct {
	ResultOf string `json:"resultOf"`
	Name     string `json:"name"`
	Path     string `json:"path"`
}

// IsResultReference checks if a value map represents a ResultReference.
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
