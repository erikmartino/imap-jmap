package jmap

import (
	"errors"
	"fmt"
	"regexp"

	"imap-jmap/jmap/jmapcore"
)

// ErrNotFound is returned by backend update methods when the referenced id does not
// exist. Handlers MUST map it to a "notFound" SetError per RFC 8620 Section 5.3.
var ErrNotFound = errors.New("not found")

// Id represents a JMAP Id as defined in RFC 8620 Section 1.6 & 1.7.5.
type Id string

var idRegexp = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)

// Validate checks if the Id matches RFC 8620 Section 1.6 rules.
func (id Id) Validate() bool {
	return jmapcore.Id(id).Validate()
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
	cInv := jmapcore.Invocation{Name: inv.Name, Args: inv.Args, ClientCallID: inv.ClientCallID}
	return cInv.MarshalJSON()
}

// UnmarshalJSON implements json.Unmarshaler for Invocation.
func (inv *Invocation) UnmarshalJSON(data []byte) error {
	var cInv jmapcore.Invocation
	if err := cInv.UnmarshalJSON(data); err != nil {
		return err
	}
	inv.Name = cInv.Name
	inv.Args = cInv.Args
	inv.ClientCallID = cInv.ClientCallID
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
	Limit  string `json:"limit,omitempty"`
}

const (
	ErrorNotJSON           = jmapcore.ErrorNotJSON
	ErrorInvalidJSON       = jmapcore.ErrorInvalidJSON
	ErrorUnknownCapability = jmapcore.ErrorUnknownCapability
	ErrorNotRequest        = jmapcore.ErrorNotRequest
	ErrorLimit             = jmapcore.ErrorLimit
)

// MethodErrorArgs returns argument map for a standard method error per RFC 8620 Section 3.6.2.
func MethodErrorArgs(errType string, description string) map[string]any {
	return jmapcore.MethodErrorArgs(errType, description)
}

// InvalidArgumentsErrorArgs returns argument map for invalidArguments per RFC 8620 Section 3.6.2.
func InvalidArgumentsErrorArgs(invalidArgs []string, description string) map[string]any {
	args := map[string]any{
		"type":      MethodErrorInvalidArguments,
		"arguments": invalidArgs,
	}
	if description != "" {
		args["description"] = description
	}
	return args
}

// SetError defines error object for /set methods per RFC 8620 Section 5.3 and RFC 8621 Section 4.6.
type SetError struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Properties  []string `json:"properties,omitempty"`
	NotFound    []string `json:"notFound,omitempty"`
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
	MethodErrorServerFail             = "serverFail"
	MethodErrorForbidden              = "forbidden"
	MethodErrorRequestTooLarge        = "requestTooLarge"
	MethodErrorCannotCalculateChanges = "cannotCalculateChanges"
)

// ResultReference represents a result reference object per RFC 8620 Section 3.7.
type ResultReference = jmapcore.ResultReference

// IsResultReference checks if a value map represents a ResultReference.
func IsResultReference(m map[string]any) bool {
	return jmapcore.IsResultReference(m)
}

// EvaluateJSONPointer resolves an RFC 6901 JSON pointer against a data structure, extended per RFC 8620 Section 3.7.
func EvaluateJSONPointer(data any, pointer string) (any, error) {
	return jmapcore.EvaluateJSONPointer(data, pointer)
}
