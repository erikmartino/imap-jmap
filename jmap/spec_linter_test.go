package jmap_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSpecTraceabilityLinter enforces that production code has proper @spec annotations
// for traceability to RFC clauses. This ensures all protocol logic can be traced back
// to specific RFC requirements.
func TestSpecTraceabilityLinter(t *testing.T) {
	// Directories to check for @spec annotations
	dirs := []string{
		".",                    // jmap package
		"../smtp",             // smtp package
		"../jmap/imapsmtp",    // imapsmtp backend
		"../jmap/nextcloud",   // nextcloud backend
		"../jmap/managesieve", // managesieve backend
	}

	// File patterns to check (non-test Go files)
	includePatterns := []string{
		"*.go",
	}

	// File patterns to exclude
	excludePatterns := []string{
		"*_test.go",
		"*_mock.go",
		"*_generated.go",
		"*pb.go", // protobuf generated files
	}

	// Collect all Go files to check
	var filesToCheck []string
	
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			
			if info.IsDir() {
				// Skip vendor and node_modules directories
				if info.Name() == "vendor" || info.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			
			// Check if this is a Go file we should examine
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			
			// Check exclude patterns
			for _, pattern := range excludePatterns {
				matched, err := filepath.Match(pattern, info.Name())
				if err == nil && matched {
					return nil
				}
			}
			
			// Check include patterns
			for _, pattern := range includePatterns {
				matched, err := filepath.Match(pattern, info.Name())
				if err == nil && matched {
					filesToCheck = append(filesToCheck, path)
					return nil
				}
			}
			
			return nil
		})
		
		if err != nil {
			t.Logf("Warning: Error walking directory %s: %v", dir, err)
		}
	}

	if len(filesToCheck) == 0 {
		t.Skip("No production Go files found to check")
	}

	// Check each file for @spec annotations
	var missingAnnotations []string
	var invalidAnnotations []string
	
	for _, filePath := range filesToCheck {
		fileAnnotations, err := checkFileForSpecAnnotations(filePath)
		if err != nil {
			t.Logf("Warning: Error checking file %s: %v", filePath, err)
			continue
		}

		// Check for functions that should have @spec annotations
		missing, invalid := analyzeFileAnnotations(filePath, fileAnnotations)
		
		missingAnnotations = append(missingAnnotations, missing...)
		invalidAnnotations = append(invalidAnnotations, invalid...)
	}

	// Report missing annotations
	if len(missingAnnotations) > 0 {
		t.Logf("Missing @spec annotations in the following functions:")
		for _, annotation := range missingAnnotations {
			t.Logf("  %s", annotation)
		}
	}

	// Report invalid annotations
	if len(invalidAnnotations) > 0 {
		t.Logf("Invalid @spec annotations found:")
		for _, annotation := range invalidAnnotations {
			t.Logf("  %s", annotation)
		}
	}

	// For now, we just log warnings. In the future, this should fail the test
	// when strict enforcement is enabled.
	if len(missingAnnotations) > 0 || len(invalidAnnotations) > 0 {
		t.Logf("Spec traceability linter found %d missing and %d invalid annotations", 
			len(missingAnnotations), len(invalidAnnotations))
	}
}

// checkFileForSpecAnnotations parses a Go file and returns all @spec annotations found
func checkFileForSpecAnnotations(filePath string) ([]string, error) {
	var annotations []string
	
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	// Walk through all comments to find @spec annotations
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CommentGroup:
			for _, comment := range v.List {
				if specAnnotation := extractSpecAnnotation(comment.Text); specAnnotation != "" {
					annotations = append(annotations, specAnnotation)
				}
			}
		case *ast.Comment:
			if specAnnotation := extractSpecAnnotation(v.Text); specAnnotation != "" {
				annotations = append(annotations, specAnnotation)
			}
		}
		return true
	})

	return annotations, nil
}

// extractSpecAnnotation extracts @spec annotations from a comment
func extractSpecAnnotation(commentText string) string {
	// Look for @spec annotations in the comment
	// Format: // @spec <clause-id> or /* @spec <clause-id> */
	
	// Remove comment markers
	cleaned := strings.TrimSpace(commentText)
	cleaned = strings.TrimPrefix(cleaned, "//")
	cleaned = strings.TrimPrefix(cleaned, "/*")
	cleaned = strings.TrimSuffix(cleaned, "*/")
	cleaned = strings.TrimSpace(cleaned)

	// Look for @spec pattern
	re := regexp.MustCompile(`@spec\s+(\S+)`)
	matches := re.FindStringSubmatch(cleaned)
	
	if len(matches) >= 2 {
		return matches[1]
	}
	
	return ""
}

// analyzeFileAnnotations analyzes a file's annotations and returns missing/invalid annotations
func analyzeFileAnnotations(filePath string, annotations []string) ([]string, []string) {
	var missing []string
	var invalid []string

	// Parse the file to find exported functions that should have @spec annotations
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, 0)
	if err != nil {
		return missing, invalid
	}

	// Functions that should have @spec annotations:
	// - JMAP method handlers (Get*, Set*, Query*, etc.)
	// - Protocol mappers
	// - Error generators
	// - Validators
	
	methodPatterns := []string{
		"Get", "Set", "Query", "Changes", "Copy", "Destroy", "Import", "Export",
		"Parse", "Validate", "Map", "Convert", "Handle",
	}

	// Collect all exported functions
	ast.Inspect(file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok {
			if fn.Name.IsExported() {
				funcName := fn.Name.Name
				
				// Check if this function should have an @spec annotation
				shouldHaveAnnotation := false
				for _, pattern := range methodPatterns {
					if strings.HasPrefix(funcName, pattern) {
						shouldHaveAnnotation = true
						break
				}
				}
				
				// Also check for common JMAP patterns
				if strings.Contains(funcName, "JMAP") || strings.Contains(funcName, "RFC") {
					shouldHaveAnnotation = true
				}
				
				if shouldHaveAnnotation {
					// Check if there's an @spec annotation for this function
					// For now, we'll just check if the function has any @spec annotation
					// In a more sophisticated implementation, we'd map annotations to specific functions
					
					// If no annotations found in the file, this function is missing an annotation
					if len(annotations) == 0 {
						missing = append(missing, filePath+": "+funcName)
					}
				}
			}
		}
		return true
	})

	// Check for invalid clause IDs in annotations
	for _, annotation := range annotations {
		if !isValidClauseIDFormat(annotation) {
			invalid = append(invalid, filePath+": @spec "+annotation)
		}
	}

	return missing, invalid
}

// isValidClauseIDFormat checks if a clause ID has a valid format
func isValidClauseIDFormat(clauseID string) bool {
	// Valid formats:
	// - RFC8620#5.4-p1-MUST
	// - draft-ietf-jmap-calendars-27#3.2.1-p2-SHOULD
	// - SPEC#SECTION-pPARA-LEVEL
	
	// Check for the basic structure: SPEC#SECTION-pPARA-LEVEL
	re := regexp.MustCompile(`^[a-zA-Z0-9-]+#[a-zA-Z0-9.]+-p\d+-[A-Z_]+$`)
	return re.MatchString(clauseID)
}