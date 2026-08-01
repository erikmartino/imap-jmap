package jmap

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

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
	ErrorInvalidJSON        = "urn:ietf:params:jmap:error:invalidJSON"
	ErrorUnknownCapability  = "urn:ietf:params:jmap:error:unknownCapability"
	ErrorNotRequest         = "urn:ietf:params:jmap:error:notRequest"
	ErrorLimit              = "urn:ietf:params:jmap:error:limit"
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

const (
	MethodErrorUnknownMethod          = "unknownMethod"
	MethodErrorInvalidArguments       = "invalidArguments"
	MethodErrorInvalidResultReference = "invalidResultReference"
)

// ResultReference represents a result reference object per RFC 8620 Section 3.3.
type ResultReference struct {
	ResultOf string `json:"#resultOf"`
	Name     string `json:"#name"`
	Path     string `json:"#path"`
}

// IsResultReference checks if a value map represents a ResultReference.
func IsResultReference(m map[string]any) bool {
	_, hasResultOf := m["#resultOf"]
	_, hasPath := m["#path"]
	return hasResultOf && hasPath
}

// EvaluateJSONPointer resolves a RFC 6901 JSON pointer against a data structure.
func EvaluateJSONPointer(data any, pointer string) (any, error) {
	if pointer == "" {
		return data, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("invalid json pointer: must start with /")
	}

	tokens := strings.Split(pointer[1:], "/")
	curr := data

	for _, token := range tokens {
		// Unescape JSON pointer tokens: ~1 -> /, ~0 -> ~
		token = strings.ReplaceAll(token, "~1", "/")
		token = strings.ReplaceAll(token, "~0", "~")

		switch v := curr.(type) {
		case map[string]any:
			val, ok := v[token]
			if !ok {
				return nil, fmt.Errorf("key %q not found in object", token)
			}
			curr = val
		case []any:
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("array index %q out of bounds", token)
			}
			curr = v[idx]
		default:
			return nil, fmt.Errorf("cannot evaluate pointer token %q on type %T", token, curr)
		}
	}
	return curr, nil
}
