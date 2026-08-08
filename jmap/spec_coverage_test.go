package jmap_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// requirementRow is one normative clause in a requirement-traceability matrix
// (docs/conformance/*.json), mapping an RFC/draft section + RFC 2119 level to the
// test(s) that cover it. See AGENTS.md "Requirement Traceability & RFC 2119 Coverage".
type requirementRow struct {
	Spec    string   `json:"spec"`
	Section string   `json:"section"`
	Level   string   `json:"level"`
	Text    string   `json:"text"`
	Tests   []string `json:"tests"`
	Status  string   `json:"status"`
	Note    string   `json:"note,omitempty"`
}

var validLevels = map[string]bool{
	"MUST": true, "MUST NOT": true, "SHOULD": true, "SHOULD NOT": true,
	"MAY": true, "RECOMMENDED": true, "OPTIONAL": true,
}

var validStatuses = map[string]bool{"covered": true, "gap": true, "non-goal": true}

// conformanceMatrices are the requirement-traceability files checked by TestSpecCoverage,
// each paired with the package test directory whose Test* functions its rows may reference.
var conformanceMatrices = []struct {
	Path    string
	TestDir string
}{
	{"../docs/conformance/jmap-calendars.json", "."},
	{"../docs/conformance/jmap-mail.json", "."},
	{"../docs/conformance/smtp.json", "../smtp"},
}

// TestSpecCoverage gates the requirement-traceability matrices: it fails on dangling
// test references, "covered" rows without tests, unsorted or malformed rows, and
// duplicate clauses, and it reports the outstanding gaps. It cannot judge whether a
// listed test exercises the clause *correctly* or across every input representation —
// that is enforced by the AGENTS.md coverage rules and the spectest.Require() citations,
// not by this structural check.
func TestSpecCoverage(t *testing.T) {
	for _, m := range conformanceMatrices {
		testNames := collectTestNames(t, m.TestDir)
		data, err := os.ReadFile(m.Path)
		if err != nil {
			t.Fatalf("read matrix %s: %v", m.Path, err)
		}
		var rows []requirementRow
		if err := json.Unmarshal(data, &rows); err != nil {
			t.Fatalf("parse matrix %s: %v", m.Path, err)
		}

		seen := map[string]bool{}
		var covered, gaps int
		var mustGaps []string

		for i, r := range rows {
			where := m.Path + " [" + strconv.Itoa(i) + "] " + r.Spec + " §" + r.Section

			if !validLevels[r.Level] {
				t.Errorf("%s: invalid RFC 2119 level %q", where, r.Level)
			}
			if !validStatuses[r.Status] {
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
			case "covered":
				if len(r.Tests) == 0 {
					t.Errorf("%s: status \"covered\" but no tests listed", where)
				}
				for _, name := range r.Tests {
					if !testNames[name] {
						t.Errorf("%s: references test %q which does not exist", where, name)
					}
				}
				covered++
			case "gap", "non-goal":
				if len(r.Tests) != 0 {
					t.Errorf("%s: status %q must not list tests (found %v)", where, r.Status, r.Tests)
				}
				if r.Status == "gap" {
					gaps++
					if r.Level == "MUST" || r.Level == "MUST NOT" {
						mustGaps = append(mustGaps, r.Spec+" §"+r.Section+" — "+r.Text)
					}
				}
			}

			if i > 0 && rowOrder(rows[i-1], r) > 0 {
				t.Errorf("%s: matrix not sorted by (spec, section); %s §%s precedes %s §%s",
					where, rows[i-1].Spec, rows[i-1].Section, r.Spec, r.Section)
			}
		}

		t.Logf("%s: %d covered, %d gap(s)", filepath.Base(m.Path), covered, gaps)
		for _, g := range mustGaps {
			t.Logf("  OUTSTANDING MUST gap: %s", g)
		}
	}
}

// rowOrder orders rows by spec (string) then section (numeric-aware), so the matrix
// stays readable and sections don't sort lexically (5.4 before 5.11).
func rowOrder(a, b requirementRow) int {
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
