// Package spectest provides RFC 2119 requirement-coverage annotations for tests.
//
// A test cites the exact normative clause it exercises with Require() or Cover();
// the citation is logged (so it shows in `go test -v`) and recorded so coverage can be
// reported and audited against the requirement matrices defined in package spec/.
//
// The spec matrices are the machine-checked source of truth (see the SpecCoverage checker);
// Require() and Cover() make each test self-documenting and, per AGENTS.md, are
// mandatory for new requirement tests.
//
// For clause-level traceability, use RequireID() with deterministic clause IDs
// in the format: `<SPEC>#<section>-p<para>-<level>`
package spectest

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"imap-jmap/spec"
)

// Level is an RFC 2119 / RFC 8174 requirement level.
type Level = spec.Level

const (
	MUST        = spec.MUST
	MUSTNOT     = spec.MUSTNOT
	SHOULD      = spec.SHOULD
	SHOULDNOT   = spec.SHOULDNOT
	MAY         = spec.MAY
	RECOMMENDED = spec.RECOMMENDED
	OPTIONAL    = spec.OPTIONAL
)

// Citation records that a test exercised a specific normative clause.
type Citation struct {
	Test    string
	Spec    string
	Section string
	Level   Level
	Text    string
	ClauseID string // Optional: deterministic clause ID for traceability
}

var (
	mu        sync.Mutex
	citations []Citation
)

// Require cites the normative clause a test covers: the spec id (e.g.
// "draft-ietf-jmap-calendars-27" or "RFC8984"), the section (e.g. "5.11.1"), the
// RFC 2119 level, and the requirement text. It is documentation that also
// self-registers for coverage reporting. It never fails the test.
func Require(t testing.TB, spec, section string, level Level, text string) {
	t.Helper()
	mu.Lock()
	citations = append(citations, Citation{Test: t.Name(), Spec: spec, Section: section, Level: level, Text: text})
	mu.Unlock()
	t.Logf("[spec] %s §%s %s — %s", spec, section, level, text)
}

// RequireID cites a normative clause by its deterministic clause ID.
// The clause ID format is: `<SPEC>#<section>-p<para>-<level>`
// For example: "RFC8620#5.4-p1-MUST"
// This provides precise traceability to specific paragraphs in the specification.
func RequireID(t testing.TB, clauseID, text string) {
	t.Helper()
	
	// Parse the clause ID to extract spec, section, level
	spec, section, level := parseClauseID(clauseID)
	
	mu.Lock()
	citations = append(citations, Citation{
		Test:    t.Name(),
		Spec:    spec,
		Section: section,
		Level:   Level(level),
		Text:    text,
		ClauseID: clauseID,
	})
	mu.Unlock()
	t.Logf("[spec] %s — %s", clauseID, text)
}

// Cover cites a strongly-typed Requirement from the spec package.
func Cover(t testing.TB, req spec.Requirement) {
	t.Helper()
	Require(t, req.Spec, req.Section, req.Level, req.Text)
}

// CoverID cites a strongly-typed Requirement from the spec package by its clause ID.
func CoverID(t testing.TB, req spec.Requirement, clauseID string) {
	t.Helper()
	RequireID(t, clauseID, req.Text)
}

// GenerateClauseID generates a deterministic clause ID from spec, section, paragraph, and level.
// Format: `<SPEC>#<section>-p<para>-<level>`
func GenerateClauseID(spec, section string, paraNum int, level Level) string {
	// Normalize the level to uppercase
	levelStr := strings.ToUpper(string(level))
	// Replace spaces with underscores for consistency
	levelStr = strings.ReplaceAll(levelStr, " ", "_")
	
	return fmt.Sprintf("%s#%s-p%d-%s", spec, section, paraNum, levelStr)
}

// parseClauseID parses a clause ID and returns spec, section, and level.
// Expected format: `<SPEC>#<section>-p<para>-<level>`
func parseClauseID(clauseID string) (spec, section, level string) {
	// Split by #
	parts := strings.Split(clauseID, "#")
	if len(parts) != 2 {
		return clauseID, "", ""
	}
	
	spec = parts[0]
	
	// Split the rest by -
	restParts := strings.Split(parts[1], "-")
	if len(restParts) < 3 {
		return spec, "", ""
	}
	
	// Find the section (before -p), paragraph (after -p), and level (after -p<para>)
	sectionPart := restParts[0]
	
	// Look for -p<para> pattern
	var paraNum int
	var levelStr string
	
	for i := 1; i < len(restParts); i++ {
		if strings.HasPrefix(restParts[i], "p") {
			// This should be the paragraph number
			paraStr := strings.TrimPrefix(restParts[i], "p")
			fmt.Sscanf(paraStr, "%d", &paraNum)
			
			// The remaining parts should be the level
			if i+1 < len(restParts) {
				levelStr = restParts[i+1]
				// Join any remaining parts
				for j := i + 2; j < len(restParts); j++ {
					levelStr += "-" + restParts[j]
				}
			}
			break
		}
	}
	
	// Normalize level string
	levelStr = strings.ReplaceAll(levelStr, "_", " ")
	level = strings.ToUpper(levelStr)
	
	return spec, sectionPart, level
}

// Registered returns a copy of every citation recorded so far.
func Registered() []Citation {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Citation, len(citations))
	copy(out, citations)
	return out
}

// RegisteredByClauseID returns a map of clause IDs to citations.
// This is used for bi-directional conformance enforcement.
func RegisteredByClauseID() map[string][]Citation {
	mu.Lock()
	defer mu.Unlock()
	
	result := make(map[string][]Citation)
	for _, citation := range citations {
		if citation.ClauseID != "" {
			result[citation.ClauseID] = append(result[citation.ClauseID], citation)
		}
	}
	return result
}

// RegisteredByTest returns a map of test names to citations.
func RegisteredByTest() map[string][]Citation {
	mu.Lock()
	defer mu.Unlock()
	
	result := make(map[string][]Citation)
	for _, citation := range citations {
		result[citation.Test] = append(result[citation.Test], citation)
	}
	return result
}
