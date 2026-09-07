package spec

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// GenerateMarkdown renders all specification matrices into a comprehensive
// Markdown document including an executive summary and requirement tables.
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
		for _, r := range m.Requirements {
			if r.Status == Covered {
				covered++
			}
			if r.Note != "" {
				hasNotes = true
			}
		}
		total := len(m.Requirements)
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

		for _, r := range m.Requirements {
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

			if hasNotes {
				cleanNote := escapeMarkdown(r.Note)
				buf.WriteString(fmt.Sprintf("| %s | %s | `%s` | %s | %s | %s | %s |\n",
					r.Spec, r.Section, r.Level, cleanText, statusBadge, testsStr, cleanNote))
			} else {
				buf.WriteString(fmt.Sprintf("| %s | %s | `%s` | %s | %s | %s |\n",
					r.Spec, r.Section, r.Level, cleanText, statusBadge, testsStr))
			}
		}

		buf.WriteString("\n---\n\n")
	}

	return buf.String()
}

func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.TrimSpace(s)
}
