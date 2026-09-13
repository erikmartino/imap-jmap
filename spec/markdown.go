package spec

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// GenerateMarkdown renders all specification matrices into a comprehensive
// Markdown document including an executive summary and requirement tables grouped by RFC and Section.
func GenerateMarkdown(matrices []Matrix) string {
	var buf bytes.Buffer

	buf.WriteString("# Specification Conformance & Requirements Traceability\n\n")
	buf.WriteString("This document is automatically generated from the Go-native specification matrices in [`spec/`](../spec/).\n\n")
	buf.WriteString("To verify or regenerate this document, run:\n")
	buf.WriteString("```bash\n")
	buf.WriteString("UPDATE_DOCS=1 go test -run TestSpecMarkdownGolden ./spec\n")
	buf.WriteString("```\n\n")

	// Summary Table
	buf.WriteString("## Summary\n\n")
	buf.WriteString("| Matrix | Spec(s) | Covered | Gaps | Non-Goals | Total | Conformance |\n")
	buf.WriteString("| :--- | :--- | :---: | :---: | :---: | :---: | :---: |\n")

	var totalCovered, totalGaps, totalNonGoals, totalReqs int

	for _, m := range matrices {
		covered, gaps, nonGoals := 0, 0, 0
		specsMap := make(map[string]bool)

		for _, r := range m.Requirements {
			specsMap[r.Spec] = true
			switch r.Status {
			case Covered:
				covered++
			case Gap:
				gaps++
			case NonGoal:
				nonGoals++
			}
		}

		total := len(m.Requirements)
		totalCovered += covered
		totalGaps += gaps
		totalNonGoals += nonGoals
		totalReqs += total

		var specsList []string
		for s := range specsMap {
			specsList = append(specsList, s)
		}
		sort.Strings(specsList)
		specsStr := strings.Join(specsList, ", ")

		pct := 0.0
		if total > 0 {
			pct = (float64(covered) / float64(total)) * 100.0
		}

		anchor := strings.ToLower(strings.ReplaceAll(m.Name, " ", "-"))
		buf.WriteString(fmt.Sprintf("| [%s](#%s) | %s | %d | %d | %d | %d | %.1f%% |\n",
			m.Name, anchor, specsStr, covered, gaps, nonGoals, total, pct))
	}

	overallPct := 0.0
	if totalReqs > 0 {
		overallPct = (float64(totalCovered) / float64(totalReqs)) * 100.0
	}
	buf.WriteString(fmt.Sprintf("| **Total** | | **%d** | **%d** | **%d** | **%d** | **%.1f%%** |\n\n",
		totalCovered, totalGaps, totalNonGoals, totalReqs, overallPct))
	buf.WriteString("---\n\n")

	// Detailed matrices
	for _, m := range matrices {
		covered := 0
		hasNotes := false
		
		// Sort copy of requirements by (Spec, Section)
		reqs := make([]Requirement, len(m.Requirements))
		copy(reqs, m.Requirements)
		sort.SliceStable(reqs, func(i, j int) bool {
			return rowOrder(reqs[i], reqs[j]) < 0
		})

		for _, r := range reqs {
			if r.Status == Covered {
				covered++
			}
			if r.Note != "" {
				hasNotes = true
			}
		}
		total := len(reqs)
		pct := 0.0
		if total > 0 {
			pct = (float64(covered) / float64(total)) * 100.0
		}

		buf.WriteString(fmt.Sprintf("## %s\n\n", m.Name))
		buf.WriteString(fmt.Sprintf("* **Test Suite Directory**: [`%s/`](../%s/)\n", m.TestDir, m.TestDir))
		buf.WriteString(fmt.Sprintf("* **Conformance**: %d / %d (%.1f%%)\n\n", covered, total, pct))

		if hasNotes {
			buf.WriteString("| Spec | Section | Level | Requirement | Status | Tests | Note |\n")
			buf.WriteString("| :--- | :---: | :---: | :--- | :---: | :--- | :--- |\n")
		} else {
			buf.WriteString("| Spec | Section | Level | Requirement | Status | Tests |\n")
			buf.WriteString("| :--- | :---: | :---: | :--- | :---: | :--- |\n")
		}

		for _, r := range reqs {
			statusBadge := "✅ Covered"
			switch r.Status {
			case Gap:
				statusBadge = "⚠️ **Gap**"
			case NonGoal:
				statusBadge = "🛑 Non-Goal"
			}

			testsStr := "—"
			if len(r.Tests) > 0 {
				var formattedTests []string
				for _, t := range r.Tests {
					formattedTests = append(formattedTests, fmt.Sprintf("`%s`", t))
				}
				testsStr = strings.Join(formattedTests, "<br/>")
			}

			cleanText := escapeMarkdown(r.Text)
			specLink := formatSpecURL(r.Spec, r.Section)

			if hasNotes {
				cleanNote := escapeMarkdown(r.Note)
				buf.WriteString(fmt.Sprintf("| %s | [%s](%s) | `%s` | %s | %s | %s | %s |\n",
					r.Spec, r.Section, specLink, r.Level, cleanText, statusBadge, testsStr, cleanNote))
			} else {
				buf.WriteString(fmt.Sprintf("| %s | [%s](%s) | `%s` | %s | %s | %s |\n",
					r.Spec, r.Section, specLink, r.Level, cleanText, statusBadge, testsStr))
			}
		}

		buf.WriteString("\n---\n\n")
	}

	return buf.String()
}

func rowOrder(a, b Requirement) int {
	if a.Spec != b.Spec {
		return strings.Compare(a.Spec, b.Spec)
	}
	return sectionCompare(a.Section, b.Section)
}

func sectionCompare(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		var an, bn int
		aerr := parseNum(as[i], &an)
		berr := parseNum(bs[i], &bn)
		if aerr && berr {
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

func parseNum(s string, out *int) bool {
	n := 0
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		n = n*10 + int(s[i]-'0')
	}
	*out = n
	return true
}

// formatSpecURL returns the direct internet URL for a given spec and section.
func formatSpecURL(specName, section string) string {
	specNameUpper := strings.ToUpper(strings.TrimSpace(specName))
	sectionClean := strings.TrimSpace(section)

	if strings.HasPrefix(specNameUpper, "RFC") {
		num := strings.TrimPrefix(specNameUpper, "RFC")
		return fmt.Sprintf("https://www.rfc-editor.org/rfc/rfc%s.html#section-%s", num, sectionClean)
	}

	if strings.HasPrefix(specName, "draft-") {
		return fmt.Sprintf("https://datatracker.ietf.org/doc/html/%s#section-%s", specName, sectionClean)
	}

	return fmt.Sprintf("https://www.rfc-editor.org/rfc/%s.html#section-%s", strings.ToLower(specName), sectionClean)
}

func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.TrimSpace(s)
}
