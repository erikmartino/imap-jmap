package jmap

import (
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
)

// parseQueryPosition extracts the "position" argument per RFC 8620 Section 5.5: an integer
// defaulting to 0. A negative value is an offset from the end of the results.
func parseQueryPosition(args map[string]any) (position int, errMsg string) {
	return jmapcore.ParseQueryPosition(args)
}

// NormalizePosition applies RFC 8620 Section 5.5 position semantics: a negative position is
// an offset from the end of the results — it is added to the total, and if still negative,
// clamped to 0.
func NormalizePosition(position, total int) int {
	return jmapcore.NormalizePosition(position, total)
}

// parseQueryAnchor extracts the "anchor" and "anchorOffset" arguments per RFC 8620
// Section 5.5. "anchor" must be an Id (string); "anchorOffset" defaults to 0, may be
// negative, and must be an integer. If no anchor is supplied, any anchorOffset argument
// is ignored. An empty anchor string is treated as absent.
func parseQueryAnchor(args map[string]any) (anchor string, offset int, errMsg string) {
	return jmapcore.ParseQueryAnchor(args)
}

// applyQueryAnchor positions a fully filtered and sorted id list per RFC 8620 Section 5.5:
// the index of the anchor within the results plus anchorOffset is used exactly as though it
// were the "position" argument (so a negative result is an offset from the end, clamped to
// 0), then the limit slices from there. It returns false when the anchor is not in the
// results; the caller MUST then reject the call with an anchorNotFound error.
func applyQueryAnchor(anchor string, offset int, ids []Id, limit *uint64) (position int, out []Id, found bool) {
	return jmapcore.ApplyQueryAnchor(anchor, offset, ids, limit)
}

// FilterCondition represents Email/query filter condition properties per RFC 8621 Section 4.5.1.
type FilterCondition = jmapmail.FilterCondition

// Comparator defines sorting rules per RFC 8620 Section 5.5.
// The canonical definition lives in jmapcore; this is a type alias for backward compatibility.
type Comparator = jmapcore.Comparator

// parseComparators parses the "sort" argument per RFC 8621 Section 4.5.2.
var (
	parseComparators     = jmapcore.ParseComparators
	ParseComparators     = jmapcore.ParseComparators
	advertisedCollations = jmapcore.AdvertisedCollations
	AdvertisedCollations = jmapcore.AdvertisedCollations
	validateComparators  = jmapcore.ValidateComparators
	ValidateComparators  = jmapcore.ValidateComparators
	computeQueryChanges  = jmapcore.ComputeQueryChanges
	ComputeQueryChanges  = jmapcore.ComputeQueryChanges
)

// ThreadFilterContext provides thread-level keyword counts for query filters.
type ThreadFilterContext = jmapmail.ThreadFilterContext

// EvalFilterOperator evaluates an RFC 8620 Section 5.5 FilterOperator object.
var (
	EvalFilterOperator = jmapcore.EvalFilterOperator
	ValidateFilter     = jmapcore.ValidateFilter
)

// MatchesFilter checks if an email matches a filter object per RFC 8621 Section 4.5.
func MatchesFilter(em *Email, filter map[string]any) bool {
	return jmapmail.MatchesFilter(em, filter)
}

// BuildThreadFilterContext constructs a ThreadFilterContext from an email slice.
func BuildThreadFilterContext(emails []*Email) *ThreadFilterContext {
	return jmapmail.BuildThreadFilterContext(emails)
}

// MatchesFilterWithThreadContext checks if an email matches a filter object with thread context.
func MatchesFilterWithThreadContext(em *Email, filter map[string]any, tc *ThreadFilterContext) bool {
	return jmapmail.MatchesFilterWithThreadContext(em, filter, tc)
}

// CleanQueryTerm strips leading/trailing whitespace, wildcards (*), and quotes from a search term.
func CleanQueryTerm(q string) string {
	return jmapmail.CleanQueryTerm(q)
}

func cleanQueryTerm(q string) string {
	return jmapmail.CleanQueryTerm(q)
}

var (
	emailSortableProperties      = jmapmail.EmailSortableProperties
	EmailSortableProperties      = jmapmail.EmailSortableProperties
	emailMutableFilterProperties = jmapmail.EmailMutableFilterProperties
	EmailMutableFilterProperties = jmapmail.EmailMutableFilterProperties
	emailMutableSortProperties   = jmapmail.EmailMutableSortProperties
	EmailMutableSortProperties   = jmapmail.EmailMutableSortProperties
)

func upToIdTruncationApplicable(filter map[string]any, comparators []Comparator, mutableFilter, mutableSort map[string]bool) bool {
	return jmapmail.UpToIDTruncationApplicable(filter, comparators, mutableFilter, mutableSort)
}

// SortEmails sorts emails in-place using RFC 8621 Section 4.4.2 comparators.
func SortEmails(emails []*Email, comparators []Comparator) {
	jmapmail.SortEmails(emails, comparators)
}

// SortEmailsWithContext sorts emails per the comparators, using precomputed per-thread
// keyword answers.
func SortEmailsWithContext(emails []*Email, comparators []Comparator, all, any map[string]bool) {
	jmapmail.SortEmailsWithContext(emails, comparators, all, any)
}

// BaseSubject returns the RFC 5256 Section 2.1 "base subject" that RFC 8621 Section 4.4.2
// uses for "subject" sorting: remove a trailing "(fwd)", then repeatedly strip any leading
// whitespace, bracketed list tag ("[tag]"), and reply/forward prefixes ("re:", "fwd:",
// "fw:" and their bracketed counter forms like "fwd[2]:"), case-insensitively.
func BaseSubject(s string) string {
	return jmapmail.BaseSubject(s)
}

func baseSubject(s string) string {
	return jmapmail.BaseSubject(s)
}
