package jmapfilenode

import (
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

var (
	ErrNotFound              = jmapcore.ErrNotFound
	MethodErrorArgs          = jmapcore.MethodErrorArgs
	NewMethodRegistry        = jmaphandler.NewMethodRegistry
	ParseProperties          = jmaphandler.ParseProperties
	parseProperties          = jmaphandler.ParseProperties
	FilterProperties         = jmaphandler.FilterProperties
	filterProperties         = jmaphandler.FilterProperties
	NilIfEmpty               = jmaphandler.NilIfEmpty
	nilIfEmpty               = jmaphandler.NilIfEmpty
	NewSetCreationRefs       = jmaphandler.NewSetCreationRefs
	newSetCreationRefs       = jmaphandler.NewSetCreationRefs
	RecordCreationRefs       = jmaphandler.RecordCreationRefs
	recordCreationRefs       = jmaphandler.RecordCreationRefs
	ResolvePatchCreationRefs = jmaphandler.ResolvePatchCreationRefs
	resolvePatchCreationRefs = jmaphandler.ResolvePatchCreationRefs
	ResolveCreationID        = jmaphandler.ResolveCreationID
	resolveCreationID        = jmaphandler.ResolveCreationID
	RunCreateLoop            = jmaphandler.RunCreateLoop
	runCreateLoop            = jmaphandler.RunCreateLoop

	parseQueryPosition  = jmapcore.ParseQueryPosition
	normalizePosition   = jmapcore.NormalizePosition
	NormalizePosition   = jmapcore.NormalizePosition
	parseQueryAnchor    = jmapcore.ParseQueryAnchor
	applyQueryAnchor    = jmapcore.ApplyQueryAnchor
	computeQueryChanges = jmapcore.ComputeQueryChanges
)

const (
	MethodErrorUnknownMethod          = jmapcore.MethodErrorUnknownMethod
	MethodErrorInvalidArguments       = jmapcore.MethodErrorInvalidArguments
	MethodErrorInvalidResultReference = jmapcore.MethodErrorInvalidResultReference
	MethodErrorUnknownDataType        = jmapcore.MethodErrorUnknownDataType
	MethodErrorAnchorNotFound         = jmapcore.MethodErrorAnchorNotFound
	MethodErrorAccountNotFound        = jmapcore.MethodErrorAccountNotFound
	MethodErrorServerFail             = jmapcore.MethodErrorServerFail
	MethodErrorForbidden              = jmapcore.MethodErrorForbidden
	MethodErrorRequestTooLarge        = jmapcore.MethodErrorRequestTooLarge
	MethodErrorCannotCalculateChanges = jmapcore.MethodErrorCannotCalculateChanges
)

func filterList[T any](list []*T, properties []string) []any {
	return jmaphandler.FilterList(list, properties)
}

func FilterList[T any](list []*T, properties []string) []any {
	return jmaphandler.FilterList(list, properties)
}
