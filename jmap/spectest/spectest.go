// Package spectest provides RFC 2119 requirement-coverage annotations for tests.
//
// A test cites the exact normative clause it exercises with Require(); the citation
// is logged (so it shows in `go test -v`) and recorded so coverage can be reported
// and audited against the requirement-traceability matrix under docs/conformance/.
//
// The matrix is the machine-checked source of truth (see the SpecCoverage checker in
// the jmap tests); Require() is the in-test citation that makes each test
// self-documenting and, per AGENTS.md, is mandatory for new requirement tests.
package spectest

import (
	"sync"
	"testing"
)

// Level is an RFC 2119 / RFC 8174 requirement level.
type Level string

const (
	MUST        Level = "MUST"
	MUSTNOT     Level = "MUST NOT"
	SHOULD      Level = "SHOULD"
	SHOULDNOT   Level = "SHOULD NOT"
	MAY         Level = "MAY"
	RECOMMENDED Level = "RECOMMENDED"
	OPTIONAL    Level = "OPTIONAL"
)

// Citation records that a test exercised a specific normative clause.
type Citation struct {
	Test    string
	Spec    string
	Section string
	Level   Level
	Text    string
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

// Registered returns a copy of every citation recorded so far.
func Registered() []Citation {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Citation, len(citations))
	copy(out, citations)
	return out
}
