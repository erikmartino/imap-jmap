// specextract is a CLI tool to fetch and parse IETF RFC documents,
// extract normative RFC 2119 keyword clauses, and generate Go-native
// specification matrices for use with the spec package.
//
// Usage:
//   go run ./tools/specextract [flags] <RFC-NUMBER> [RFC-NUMBER...]
//
// Examples:
//   go run ./tools/specextract RFC8620
//   go run ./tools/specextract -output rfc8620.go RFC8620
//   go run ./tools/specextract -format json RFC8620 RFC8621
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"unicode"
)

// RFC2119 keywords as defined in RFC 2119 and RFC 8174
var rfc2119Keywords = []string{
	"MUST", "MUST NOT", "SHOULD", "SHOULD NOT", "MAY", "RECOMMENDED", "OPTIONAL",
	"REQUIRED", "SHALL", "SHALL NOT", "NOT RECOMMENDED",
}

// RFCClause represents a single normative clause extracted from an RFC
type RFCClause struct {
	Spec    string `json:"spec"`
	Section string `json:"section"`
	Level   string `json:"level"`
	Text    string `json:"text"`
	ParaNum int    `json:"para_num"`
}

// RFCDocument represents a parsed RFC with extracted clauses
type RFCDocument struct {
	RFCNumber string       `json:"rfc_number"`
	Title     string       `json:"title"`
	Clauses   []RFCClause  `json:"clauses"`
}

// Config holds the CLI configuration
type Config struct {
	OutputFile string
	Format     string // "go" or "json"
	Verbose    bool
	Debug      bool
}

func main() {
	config := parseFlags()

	if len(flag.Args()) == 0 {
		fmt.Fprintf(os.Stderr, "Error: No RFC numbers specified\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <RFC-NUMBER> [RFC-NUMBER...]\n", os.Args[0])
		os.Exit(1)
	}

	rfcNumbers := flag.Args()
	var documents []RFCDocument

	for _, rfcNum := range rfcNumbers {
		doc, err := fetchAndParseRFC(rfcNum, config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", rfcNum, err)
			continue
		}
		documents = append(documents, *doc)
	}

	if len(documents) == 0 {
		fmt.Fprintf(os.Stderr, "Error: No RFCs were successfully processed\n")
		os.Exit(1)
	}

	// Output results
	var output io.Writer = os.Stdout
	if config.OutputFile != "" {
		file, err := os.Create(config.OutputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()
		output = file
	}

	switch config.Format {
	case "json":
		outputJSON(output, documents)
	case "go":
		outputGo(output, documents)
	default:
		fmt.Fprintf(os.Stderr, "Error: Unsupported format %q\n", config.Format)
		os.Exit(1)
	}
}

func parseFlags() Config {
	config := Config{}

	flag.StringVar(&config.OutputFile, "output", "", "Output file path (default: stdout)")
	flag.StringVar(&config.Format, "format", "go", "Output format: 'go' or 'json'")
	flag.BoolVar(&config.Verbose, "verbose", false, "Verbose output")
	flag.BoolVar(&config.Debug, "debug", false, "Debug output (very verbose)")

	flag.Parse()

	return config
}

func fetchAndParseRFC(rfcNum string, config Config) (*RFCDocument, error) {
	// Normalize RFC number
	rfcNum = strings.ToUpper(strings.TrimPrefix(rfcNum, "RFC"))
	if !strings.HasPrefix(rfcNum, "RFC") {
		rfcNum = "RFC" + rfcNum
	}

	if config.Verbose {
		fmt.Printf("Processing %s...\n", rfcNum)
	}

	// Fetch RFC text
	text, err := fetchRFCText(rfcNum)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch RFC %s: %w", rfcNum, err)
	}

	// Parse the RFC text
	doc, err := parseRFCText(rfcNum, text, config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RFC %s: %w", rfcNum, err)
	}

	return doc, nil
}

func fetchRFCText(rfcNum string) (string, error) {
	// Normalize RFC number by removing RFC prefix
	rfcNumber := strings.TrimPrefix(rfcNum, "RFC")
	
	// Try to fetch from IETF datatracker
	url := fmt.Sprintf("https://www.rfc-editor.org/rfc/rfc%s.txt", strings.ToLower(rfcNumber))
	
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Try alternative URL format
		url = fmt.Sprintf("https://www.rfc-editor.org/rfc/%s.txt", strings.ToLower(rfcNumber))
		resp, err = http.Get(url)
		if err != nil {
			return "", fmt.Errorf("HTTP request failed: %w", err)
		}
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(body), nil
}

func parseRFCText(rfcNum, text string, config Config) (*RFCDocument, error) {
	// Clean the text by removing excessive whitespace and form feeds
	text = cleanRFCText(text)
	
	doc := &RFCDocument{
		RFCNumber: rfcNum,
		Title:     extractTitle(text),
	}

	// Find the start of the actual content (after the header and TOC)
	contentStart := findContentStart(text)
	if contentStart >= len(text) {
		return doc, nil
	}
	
	content := text[contentStart:]

	// Extract clauses directly from the content
	clauses := extractRFC2119ClausesFromText(content, rfcNum, config)
	doc.Clauses = clauses

	return doc, nil
}

func cleanRFCText(text string) string {
	// Remove form feed characters and excessive newlines
	text = strings.ReplaceAll(text, "\f", "")
	text = strings.ReplaceAll(text, "\r", "")
	
	// Collapse multiple newlines into single newlines
	lines := strings.Split(text, "\n")
	var cleanedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleanedLines = append(cleanedLines, trimmed)
		}
	}
	
	return strings.Join(cleanedLines, "\n")
}

func extractTitle(text string) string {
	// RFC titles are typically after "Request for Comments: XXX" and before "Status of this Memo"
	lines := strings.Split(text, "\n")
	
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Request for Comments:") {
			// Look for the title in the next few lines
			for j := i + 1; j < len(lines) && j < i+10; j++ {
				nextLine := strings.TrimSpace(lines[j])
				if nextLine != "" && !strings.Contains(nextLine, ":") && 
				   !strings.Contains(nextLine, "Status") && !strings.Contains(nextLine, "Abstract") &&
				   !strings.Contains(nextLine, "Category") && !strings.Contains(nextLine, "BCP") &&
				   len(nextLine) > 10 && len(nextLine) < 200 {
					return nextLine
				}
			}
		}
	}
	
	return "Untitled"
}

func findContentStart(text string) int {
	// Find where the actual RFC content starts by looking for the first
	// section that doesn't look like a TOC entry
	lines := strings.Split(text, "\n")
	
	// Skip lines until we find the first real section header
	for i, line := range lines {
		line = strings.TrimSpace(line)
		
		// Skip empty lines
		if line == "" {
			continue
		}
		
		// Skip header lines
		if strings.Contains(line, "Status of this Memo") || 
		   strings.Contains(line, "Copyright Notice") ||
		   strings.Contains(line, "Table of Contents") ||
		   strings.Contains(line, "RFC") && strings.Contains(line, "For the Internet Community") {
			continue
		}
		
		// Skip TOC entries (lines with many dots or page numbers)
		if isTOCEntry(line) {
			continue
		}
		
		// If this line looks like a section header (starts with a number)
		// and doesn't look like a TOC entry, we've found the start
		if (strings.HasPrefix(line, "1.") || strings.HasPrefix(line, "1 ") || 
		    strings.HasPrefix(line, "1)") || strings.HasPrefix(line, "2.") || 
		    strings.HasPrefix(line, "2 ") || strings.HasPrefix(line, "2)")) &&
		   !isTOCEntry(line) {
			return i
		}
		
		// If this line doesn't look like a TOC entry and contains actual text,
		// it might be the start of content
		if !isTOCEntry(line) && len(line) > 20 && !strings.Contains(line, "...") {
			return i
		}
	}
	
	// If we didn't find a clear start, start after the first 50 lines
	if len(lines) > 50 {
		return 50
	}
	
	return 0
}

func isTOCEntry(line string) bool {
	line = strings.TrimSpace(line)
	
	// Check for the pattern of dots used in TOC
	if strings.Contains(line, " . . . ") || strings.Contains(line, "... ") {
		return true
	}
	
	// Check for many consecutive dots (more than 5)
	if strings.Count(line, ".") > 5 {
		return true
	}
	
	// Check for page numbers at the end
	if len(line) > 0 {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 {
			lastChar := rune(trimmed[len(trimmed)-1])
			if unicode.IsDigit(lastChar) {
				// Check if there are dots in the line
				if strings.Contains(trimmed, ".") {
					return true
				}
			}
		}
	}
	
	// Check for lines that start with a number and contain many dots
	if len(line) > 0 && unicode.IsDigit(rune(line[0])) {
		if strings.Count(line, ".") > 3 {
			return true
		}
	}
	
	return false
}

func extractRFC2119ClausesFromText(text, rfcNum string, config Config) []RFCClause {
	var clauses []RFCClause
	
	// Split into lines
	lines := strings.Split(text, "\n")
	
	// Track current section
	currentSection := ""
	paraNum := 1
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		
		// Check if this is a section header
		if isSectionHeader(line) {
			currentSection = extractSectionNumber(line)
			paraNum = 1
			continue
		}
		
		// Skip TOC entries
		if isTOCEntry(line) {
			continue
		}
		
		// Check for RFC 2119 keywords in this line
		for _, keyword := range rfc2119Keywords {
			clause := extractClauseFromLine(line, keyword, currentSection, paraNum, rfcNum, config)
			if clause != nil {
				clauses = append(clauses, *clause)
				paraNum++
				break // Only extract one clause per line
			}
		}
	}
	
	return clauses
}

func isSectionHeader(line string) bool {
	// Section headers typically start with a number followed by a dot or space
	re := regexp.MustCompile(`^\d+(\.\d+)*[\. )]+.*$`)
	return re.MatchString(line)
}

func extractSectionNumber(line string) string {
	// Extract the section number from the header
	re := regexp.MustCompile(`^(\d+(\.\d+)*)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

func extractClauseFromLine(line, keyword, currentSection string, paraNum int, rfcNum string, config Config) *RFCClause {
	// Case-insensitive search for the keyword
	lowerLine := strings.ToLower(line)
	lowerKeyword := strings.ToLower(keyword)
	
	// Find the keyword in the line
	keywordIndex := strings.Index(lowerLine, lowerKeyword)
	if keywordIndex == -1 {
		return nil
	}

	// Extract the sentence containing the keyword
	sentence := extractSentence(line, keywordIndex, keyword)
	if sentence == "" {
		return nil
	}

	// Clean up the sentence
	sentence = strings.TrimSpace(sentence)
	sentence = cleanSentence(sentence)

	if sentence == "" {
		return nil
	}

	// Map keyword to standard RFC 2119 level
	level := mapKeywordToLevel(keyword)

	if config.Debug {
		fmt.Printf("Found %s clause in %s §%s [p%d]: %s\n", keyword, rfcNum, currentSection, paraNum, sentence)
	}

	return &RFCClause{
		Spec:    rfcNum,
		Section: currentSection,
		Level:   level,
		Text:    sentence,
		ParaNum: paraNum,
	}
}

func extractSentence(line string, keywordIndex int, keyword string) string {
	// Find the start of the sentence (beginning of line or after a period)
	start := keywordIndex
	for i := keywordIndex - 1; i >= 0; i-- {
		if line[i] == '.' || line[i] == '!' || line[i] == '?' {
			// Skip spaces after punctuation
			start = i + 1
			for start < len(line) && unicode.IsSpace(rune(line[start])) {
				start++
			}
			break
		}
		if i == 0 {
			start = 0
			break
		}
	}

	// Find the end of the sentence (next period, exclamation, or question mark)
	end := len(line)
	for i := keywordIndex + len(keyword); i < len(line); i++ {
		if line[i] == '.' || line[i] == '!' || line[i] == '?' {
			end = i + 1
			break
		}
	}

	if start >= end {
		return ""
	}

	return line[start:end]
}

func cleanSentence(sentence string) string {
	// Remove extra whitespace
	sentence = strings.Join(strings.Fields(sentence), " ")
	
	// Remove trailing punctuation that might have been included
	sentence = strings.TrimRight(sentence, ". ")
	
	// Remove common RFC formatting artifacts
	sentence = strings.ReplaceAll(sentence, "  ", " ")
	
	return sentence
}

func mapKeywordToLevel(keyword string) string {
	// Map various keyword forms to standard RFC 2119 levels
	switch strings.ToUpper(keyword) {
	case "MUST", "REQUIRED", "SHALL":
		return "MUST"
	case "MUST NOT", "SHALL NOT":
		return "MUST NOT"
	case "SHOULD", "RECOMMENDED":
		return "SHOULD"
	case "SHOULD NOT", "NOT RECOMMENDED":
		return "SHOULD NOT"
	case "MAY", "OPTIONAL":
		return "MAY"
	default:
		return keyword
	}
}

func outputJSON(w io.Writer, documents []RFCDocument) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	
	if err := encoder.Encode(documents); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}

func outputGo(w io.Writer, documents []RFCDocument) {
	// Generate Go code for spec package
	
	fmt.Fprintf(w, "// Code generated by specextract. DO NOT EDIT DIRECTLY.\n")
	fmt.Fprintf(w, "package spec\n\n")
	
	// Generate a variable for each RFC
	for _, doc := range documents {
		varName := fmt.Sprintf("%sRequirements", doc.RFCNumber)
		
		fmt.Fprintf(w, "var %s = []Requirement{\n", varName)
		
		for i, clause := range doc.Clauses {
			// Escape quotes in text
			text := strings.ReplaceAll(clause.Text, `"`, `\"`)
			text = strings.ReplaceAll(text, "\n", " ")
			text = strings.ReplaceAll(text, "\r", "")
			
			fmt.Fprintf(w, "\t{\n")
			fmt.Fprintf(w, "\t\tSpec:    \"%s\",\n", clause.Spec)
			fmt.Fprintf(w, "\t\tSection: \"%s\",\n", clause.Section)
			fmt.Fprintf(w, "\t\tLevel:   Level(\"%s\"),\n", clause.Level)
			fmt.Fprintf(w, "\t\tText:    \"%s\",\n", text)
			fmt.Fprintf(w, "\t\tTests:   []string{}, // TODO: Add test references\n")
			fmt.Fprintf(w, "\t\tStatus:  Gap, // TODO: Update status\n")
			fmt.Fprintf(w, "\t\tNote:    \"Extracted by specextract from %s §%s [p%d]\",\n", 
				clause.Spec, clause.Section, clause.ParaNum)
			
			if i < len(doc.Clauses)-1 {
				fmt.Fprintf(w, "\t},\n")
			} else {
				fmt.Fprintf(w, "\t}\n")
			}
		}
		
		fmt.Fprintf(w, "}\n\n")
	}
}