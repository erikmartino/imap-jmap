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
type Id = jmapcore.Id

// Invocation represents a JMAP Invocation array tuple: [name, args, clientCallId]
type Invocation = jmapcore.Invocation

// Request represents a JMAP Request object per RFC 8620 Section 3.1.
type Request = jmapcore.Request

// Response represents a JMAP Response object per RFC 8620 Section 3.5.
type Response = jmapcore.Response

// RequestError represents a Problem Details object for JMAP Request errors per RFC 8620 Section 3.6.1.
type RequestError = jmapcore.RequestError

// SetError defines error object for /set methods per RFC 8620 Section 5.3 and RFC 8621 Section 4.6.
type SetError = jmapcore.SetError

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
