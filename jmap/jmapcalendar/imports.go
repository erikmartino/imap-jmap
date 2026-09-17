package jmapcalendar

import (
	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcopy"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)


var (
	MethodErrorArgs          = jmapcore.MethodErrorArgs
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
	ValidateGetLimits        = jmaphandler.ValidateGetLimits
	ValidateSetLimits        = jmaphandler.ValidateSetLimits
	SourceAccountContext     = jmapcopy.SourceAccountContext
	sourceAccountContext     = jmapcopy.SourceAccountContext
	ResolveCopyAccountIDs    = jmapcopy.ResolveCopyAccountIDs
	ValidateCopyStates       = jmapcopy.ValidateCopyStates
	MergeCopyOverrides       = jmapcopy.MergeCopyOverrides
	mergeCopyOverrides       = jmapcopy.MergeCopyOverrides
	AccountIDFromContext     = jmapauth.AccountIDFromContext
	ContextWithAccountID     = jmapauth.ContextWithAccountID
	AccountIDForSubject           = jmapauth.AccountIDForSubject
	SubjectForAccountID           = jmapauth.SubjectForAccountID
	SubjectFromContext            = jmapauth.SubjectFromContext
	PrincipalAccountIDFromContext = jmapauth.PrincipalAccountIDFromContext
	parseQueryPosition            = jmapcore.ParseQueryPosition
	normalizePosition             = jmapcore.NormalizePosition
	NormalizePosition             = jmapcore.NormalizePosition
	parseQueryAnchor              = jmapcore.ParseQueryAnchor
	applyQueryAnchor              = jmapcore.ApplyQueryAnchor
	parseComparators              = jmapcore.ParseComparators
	validateComparators           = jmapcore.ValidateComparators
	computeQueryChanges           = jmapcore.ComputeQueryChanges
	ContextWithSubject            = jmapauth.ContextWithSubject
	ContextWithCredentials        = jmapauth.ContextWithCredentials
	CredentialsFromContext        = jmapauth.CredentialsFromContext
	EvalFilterOperator            = jmapcore.EvalFilterOperator
	expandGroupRecipients         = ExpandGroupRecipients
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
