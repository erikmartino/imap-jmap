package jmap_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"imap-jmap/spec"
)
// TestSpecCoverage gates the requirement-traceability matrices: it fails on dangling
// test references, "covered" rows without tests, unsorted or malformed rows, and
// duplicate clauses, and it reports the outstanding gaps. It cannot judge whether a
// listed test exercises the clause *correctly* or across every input representation —
// that is enforced by the AGENTS.md coverage rules and the spectest.Require() citations,
// not by this structural check.
func TestSpecCoverage(t *testing.T) {
	for _, m := range spec.Matrices {
		testDir := m.TestDir
		switch testDir {
		case "jmap":
			testDir = "."
		case "jmap/vcardconv":
			testDir = "./vcardconv"
		case "smtp":
			testDir = "../smtp"
		default:
			testDir = filepath.Join("..", testDir)
		}

		testNames := collectTestNames(t, testDir)
		rows := m.Requirements

		seen := map[string]bool{}
		var covered, gaps int
		var mustGaps []string

		for i, r := range rows {
			where := m.Name + " [" + strconv.Itoa(i) + "] " + r.Spec + " §" + r.Section

			if !spec.ValidLevels[r.Level] {
				t.Errorf("%s: invalid RFC 2119 level %q", where, r.Level)
			}
			if !spec.ValidStatuses[r.Status] {
				t.Errorf("%s: invalid status %q (want covered|gap|non-goal)", where, r.Status)
			}

			key := r.Spec + "|" + r.Section + "|" + r.Text
			if seen[key] {
				t.Errorf("%s: duplicate clause: %q", where, r.Text)
			}
			seen[key] = true

			if !sort.StringsAreSorted(r.Tests) {
				t.Errorf("%s: tests list must be sorted, got %v", where, r.Tests)
			}

			switch r.Status {
			case spec.Covered:
				if len(r.Tests) == 0 {
					t.Errorf("%s: status \"covered\" but no tests listed", where)
				}
				for _, name := range r.Tests {
					if !testNames[name] {
						t.Errorf("%s: references test %q which does not exist", where, name)
					}
				}
				covered++
			case spec.Gap, spec.NonGoal:
				if len(r.Tests) != 0 {
					t.Errorf("%s: status %q must not list tests (found %v)", where, r.Status, r.Tests)
				}
				if r.Status == spec.Gap {
					gaps++
					if r.Level == spec.MUST || r.Level == spec.MUSTNOT {
						mustGaps = append(mustGaps, r.Spec+" §"+r.Section+" — "+r.Text)
					}
				}
			}

			if i > 0 && rowOrder(rows[i-1], r) > 0 {
				t.Errorf("%s: matrix not sorted by (spec, section); %s §%s precedes %s §%s",
					where, rows[i-1].Spec, rows[i-1].Section, r.Spec, r.Section)
			}
		}

		t.Logf("%s: %d covered, %d gap(s)", m.Name, covered, gaps)
		for _, g := range mustGaps {
			t.Logf("  OUTSTANDING MUST gap: %s", g)
		}
	}
}

// TestSpecCoverageBidirectional enforces bi-directional conformance between spec matrices and test citations.
// This is a placeholder for the full implementation which will:
// 1. Spec -> Test: Every "covered" clause in the matrix must be referenced by an existing test function containing spectest.RequireID.
// 2. Test -> Spec: Every spectest.RequireID call in test code must exist in the canonical matrix (flagging typos and stale IDs).
// 3. No Untracked Tests: Any test matching TestRFC* or feature suites must cite at least one valid spec clause ID.
func TestSpecCoverageBidirectional(t *testing.T) {
	// For now, this is a placeholder that logs the current state
	t.Log("Bi-directional conformance enforcement: Not yet fully implemented")
	
	// Collect all clause IDs from the spec matrices
	matrixClauseIDs := collectMatrixClauseIDs()
	
	// Log the number of clauses found
	clauseCount := 0
	for range matrixClauseIDs {
		clauseCount++
	}
	t.Logf("Found %d clause IDs in spec matrices", clauseCount)
}

// collectMatrixClauseIDs collects all clause IDs from the spec matrices
func collectMatrixClauseIDs() map[string]bool {
	clauseIDs := make(map[string]bool)
	
	for _, m := range spec.Matrices {
		for _, req := range m.Requirements {
			// Generate clause ID for this requirement
			clauseID := generateClauseIDFromRequirement(req)
			clauseIDs[clauseID] = true
			
			// Also add variations that might be used in tests
			// For example, with different paragraph numbers
			baseID := req.Spec + "#" + req.Section + "-"
			clauseIDs[baseID] = true
		}
	}
	
	return clauseIDs
}

// collectTestClauseIDs scans all test files for spectest.RequireID calls
func collectTestClauseIDs(t *testing.T) map[string]map[string]bool {
	// This is a simplified version - in a real implementation, we would parse the AST
	// to find all calls to spectest.RequireID and extract the clauseID argument.
	// For now, we'll return an empty map as this would require more complex AST parsing.
	
	// For the initial implementation, we'll just return an empty map
	// The actual implementation would use AST parsing to find RequireID calls
	return make(map[string]map[string]bool)
}

// generateClauseIDFromRequirement generates a clause ID from a requirement
func generateClauseIDFromRequirement(req spec.Requirement) string {
	// Simple format: SPEC#SECTION-LEVEL
	// In the future, this should include paragraph numbers
	levelStr := strings.ReplaceAll(string(req.Level), " ", "_")
	return req.Spec + "#" + req.Section + "-" + levelStr
}

// isValidClauseID checks if a clause ID is valid based on the matrices
func isValidClauseID(clauseID string, matrixClauseIDs map[string]bool) bool {
	// Check for exact match
	if matrixClauseIDs[clauseID] {
		return true
	}
	
	// Check if the clause ID matches any requirement by spec/section
	// Parse the clause ID
	parts := strings.Split(clauseID, "#")
	if len(parts) != 2 {
		return false
	}
	
	specPart := parts[0]
	sectionPart := parts[1]
	
	// Look for any requirement with matching spec and section
	for _, m := range spec.Matrices {
		for _, req := range m.Requirements {
			if req.Spec == specPart && req.Section == sectionPart {
				return true
			}
		}
	}
	
	return false
}

// getTestNamesFromCitations extracts test names from citations
func getTestNamesFromCitations(citations map[string]bool) []string {
	var names []string
	for name := range citations {
		names = append(names, name)
	}
	return names
}

// rowOrder orders rows by spec (string) then section (numeric-aware), so the matrix
// stays readable and sections don't sort lexically (5.4 before 5.11).
func rowOrder(a, b spec.Requirement) int {
	if a.Spec != b.Spec {
		return strings.Compare(a.Spec, b.Spec)
	}
	return sectionCompare(a.Section, b.Section)
}

func sectionCompare(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		if aerr == nil && berr == nil {
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
			continue
		}
		if c := strings.Compare(as[i], bs[i]); c != 0 {
			return c
		}
	}
	return len(as) - len(bs)
}

// collectTestNames parses every *_test.go in the package directory and returns the set
// of top-level Test function names, so the matrix's test references can be verified.
func collectTestNames(t *testing.T, dir string) map[string]bool {
	names := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read test dir %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "Test") {
				names[fn.Name.Name] = true
			}
		}
	}
	return names
}
