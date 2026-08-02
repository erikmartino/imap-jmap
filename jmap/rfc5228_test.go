package jmap_test

import (
	"context"
	"testing"

	"imap-jmap/jmap/memory"
)

// TestRFC5228_SieveLanguageValidation tests Sieve filtering language syntax validation per RFC 5228.
func TestRFC5228_SieveLanguageValidation(t *testing.T) {
	sieveBackend := memory.NewMemorySieveBackend()

	// Valid RFC 5228 Sieve script with fileinto and keep
	validScript := `require ["fileinto", "reject"];
if header :contains "subject" "urgent" {
    fileinto "INBOX.Urgent";
} else {
    keep;
}`

	isValid, errDetail := sieveBackend.ValidateSieveScript(context.Background(), validScript)
	if !isValid {
		t.Errorf("Expected valid RFC 5228 Sieve script, got error: %s", errDetail)
	}

	// Invalid RFC 5228 Sieve script (syntax error)
	invalidScript := `if header :contains { invalid syntax }`
	isValidBad, _ := sieveBackend.ValidateSieveScript(context.Background(), invalidScript)
	if isValidBad {
		t.Errorf("Expected invalid Sieve script to fail syntax validation")
	}
}
