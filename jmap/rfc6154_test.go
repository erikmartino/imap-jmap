package jmap_test

import (
	"testing"

	"imap-jmap/jmap"
)

// TestRFC6154_IMAPSpecialUseAttributes tests RFC 6154 IMAP LIST Extension for Special-Use Mailboxes mapping.
func TestRFC6154_IMAPSpecialUseAttributes(t *testing.T) {
	specialUseRoles := []string{"all", "archive", "drafts", "flagged", "sent", "trash", "junk"}

	for _, role := range specialUseRoles {
		attr := jmap.JMAPRoleToIMAPAttribute(role)
		if attr == "" {
			t.Errorf("Expected non-empty IMAP special-use attribute for role %q per RFC 6154", role)
		}
	}
}
