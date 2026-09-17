package jmap

import (
	"imap-jmap/jmap/jmaphandler"
)

// MethodHandler defines the function signature for a JMAP method handler.
type MethodHandler = jmaphandler.MethodHandler

// MethodRegistry manages registered JMAP methods per RFC 8620 Section 3.4.
type MethodRegistry = jmaphandler.MethodRegistry

// NewMethodRegistry creates a new MethodRegistry with standard RFC 8620 methods registered.
var NewMethodRegistry = jmaphandler.NewMethodRegistry

// aliasMethod wraps a handler so it can be registered under an additional method name.
var aliasMethod = jmaphandler.AliasMethod

// AliasMethod wraps a handler so it can be registered under an additional method name.
var AliasMethod = jmaphandler.AliasMethod
