// Package spectest provides RFC 2119 requirement-coverage annotations for tests.
//
// A test cites the exact normative clause it exercises with Require() or Cover();
// the citation is logged (so it shows in `go test -v`) and recorded so coverage can be
// reported and audited against the requirement matrices defined in package spec/.
//
// The spec matrices are the machine-checked source of truth (see the SpecCoverage checker);
// Require() and Cover() make each test self-documenting and, per AGENTS.md, are
// mandatory for new requirement tests.
package spectest

import (
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

// Cover cites a strongly-typed Requirement from the spec package.
func Cover(t testing.TB, req spec.Requirement) {
	t.Helper()
	Require(t, req.Spec, req.Section, req.Level, req.Text)
}

// Registered returns a copy of every citation recorded so far.
func Registered() []Citation {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Citation, len(citations))
	copy(out, citations)
	return out
}
