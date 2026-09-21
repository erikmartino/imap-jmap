package jmap

import (
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

// Creation references (RFC 8620 Section 5.3) helper wrappers mapping to jmapcore/jmaphandler primitives.
// @spec RFC8620#5.3-p1-MUST

type CreationRefs = jmapcore.CreationRefs

var (
	isIdProperty             = jmapcore.IsIdProperty
	nilIfEmpty               = jmaphandler.NilIfEmpty
	NilIfEmpty               = jmaphandler.NilIfEmpty
	resolveCreationRef       = jmapcore.ResolveCreationRef
	resolveNodeCreationRefs  = jmapcore.ResolveNodeCreationRefs
	resolveIdBooleanMapRefs  = jmapcore.ResolveIdBooleanMapRefs
	resolveCreationID        = jmaphandler.ResolveCreationID
	ResolveCreationID        = jmaphandler.ResolveCreationID
	runCreateLoop            = jmaphandler.RunCreateLoop
	RunCreateLoop            = jmaphandler.RunCreateLoop
	resolvePatchCreationRefs = jmaphandler.ResolvePatchCreationRefs
	ResolvePatchCreationRefs = jmaphandler.ResolvePatchCreationRefs
	NewCreationRefs          = jmaphandler.NewCreationRefs
	WithCreationRefs         = jmaphandler.WithCreationRefs
	CreationRefsFrom         = jmaphandler.CreationRefsFrom
	newSetCreationRefs       = jmaphandler.NewSetCreationRefs
	NewSetCreationRefs       = jmaphandler.NewSetCreationRefs
	recordCreationRefs       = jmaphandler.RecordCreationRefs
	NewRequestCache          = jmaphandler.NewRequestCache
	NewDummyRequestCache     = jmaphandler.NewDummyRequestCache
	WithRequestCache         = jmaphandler.WithRequestCache
	RequestCacheFrom         = jmaphandler.RequestCacheFrom
)

type RequestCache = jmapcore.RequestCache
