package jmapcore

import (
	"encoding/json"
	"fmt"
)

// Invocation represents a JMAP Invocation array tuple: [name, args, clientCallId]
// as defined in RFC 8620 Section 3.2.
// @spec RFC8620#3.2-p1-MUST
type Invocation struct {
	Name         string
	Args         map[string]any
	ClientCallID string
}

// MarshalJSON implements json.Marshaler for Invocation per RFC 8620 Section 3.2.
// @spec RFC8620#3.2-p1-MUST
func (inv Invocation) MarshalJSON() ([]byte, error) {
	args := inv.Args
	if args == nil {
		args = make(map[string]any)
	}
	return json.Marshal([]any{inv.Name, args, inv.ClientCallID})
}

// UnmarshalJSON implements json.Unmarshaler for Invocation per RFC 8620 Section 3.2.
// @spec RFC8620#3.2-p1-MUST
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
// @spec RFC8620#3.1-p1-MUST
type Request struct {
	Using       []string          `json:"using"`
	MethodCalls []Invocation      `json:"methodCalls"`
	CreatedIds  map[string]string `json:"createdIds,omitempty"`
}

// Response represents a JMAP Response object per RFC 8620 Section 3.5.
// @spec RFC8620#3.5-p1-MUST
type Response struct {
	MethodResponses []Invocation      `json:"methodResponses"`
	CreatedIds      map[string]string `json:"createdIds,omitempty"`
	SessionState    string            `json:"sessionState"`
}

// RequestError represents a Problem Details object for JMAP Request errors per RFC 8620 Section 3.6.1.
// @spec RFC8620#3.6.1-p1-MUST
type RequestError struct {
	Type   string `json:"type"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	Limit  string `json:"limit,omitempty"`
}

const (
	ErrorNotJSON           = "urn:ietf:params:jmap:error:notJSON"
	ErrorInvalidJSON       = ErrorNotJSON
	ErrorUnknownCapability = "urn:ietf:params:jmap:error:unknownCapability"
	ErrorNotRequest        = "urn:ietf:params:jmap:error:notRequest"
	ErrorLimit             = "urn:ietf:params:jmap:error:limit"
)

// Standard JMAP method error types per RFC 8620 Section 3.6.2.
// @spec RFC8620#3.6.2-p1-MUST
const (
	MethodErrorUnknownMethod          = "unknownMethod"
	MethodErrorInvalidArguments       = "invalidArguments"
	MethodErrorInvalidResultReference = "invalidResultReference"
	MethodErrorUnknownDataType        = "unknownDataType"
	MethodErrorAnchorNotFound         = "anchorNotFound"
	MethodErrorAccountNotFound        = "accountNotFound"
	MethodErrorServerFail             = "serverFail"
	MethodErrorForbidden              = "forbidden"
	MethodErrorRequestTooLarge        = "requestTooLarge"
	MethodErrorCannotCalculateChanges = "cannotCalculateChanges"
)

// MethodErrorArgs returns argument map for a standard method error per RFC 8620 Section 3.6.2.
// @spec RFC8620#3.6.2-p1-MUST
func MethodErrorArgs(errType string, description string) map[string]any {
	args := map[string]any{
		"type": errType,
	}
	if description != "" {
		args["description"] = description
	}
	return args
}

// InvalidArgumentsErrorArgs returns an invalidArguments method error argument map with optional properties.
func InvalidArgumentsErrorArgs(properties []string, description string) map[string]any {
	args := MethodErrorArgs(MethodErrorInvalidArguments, description)
	if len(properties) > 0 {
		args["properties"] = properties
	}
	return args
}
