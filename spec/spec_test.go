package spec_test

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

// TestSpecCoverage ensures all specification requirement matrices are valid,
// sorted, refer to real tests, and report outstanding gaps.
func TestSpecCoverage(t *testing.T) {
	for _, m := range spec.Matrices {
		testDir := filepath.Join("..", m.TestDir)
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

// TestSpecMarkdownGolden ensures docs/SPEC_COVERAGE.md is up-to-date with spec.Matrices.
// When UPDATE_DOCS=1 is set, it regenerates docs/SPEC_COVERAGE.md.
// In normal test runs, it fails if the file on disk differs from the generated markdown.
func TestSpecMarkdownGolden(t *testing.T) {
	docPath := filepath.Join("..", "docs", "SPEC_COVERAGE.md")
	generated := spec.GenerateMarkdown(spec.Matrices)

	if os.Getenv("UPDATE_DOCS") == "1" {
		if err := os.WriteFile(docPath, []byte(generated), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", docPath, err)
		}
		t.Logf("Successfully updated %s", docPath)
		return
	}

	existing, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("%s does not exist. Run with UPDATE_DOCS=1 go test -run TestSpecMarkdownGolden ./spec to create it: %v", docPath, err)
	}

	if string(existing) != generated {
		t.Fatalf("%s is out of date with spec.Matrices. Run with UPDATE_DOCS=1 go test -run TestSpecMarkdownGolden ./spec to regenerate.", docPath)
	}
}
