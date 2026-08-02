package jmap_test

import (
	"reflect"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC9979_IMAPFlagToJMAPKeyword tests translation of IMAP system flags to JMAP keywords per RFC 9979.
func TestRFC9979_IMAPFlagToJMAPKeyword(t *testing.T) {
	tests := []struct {
		imapFlag string
		expected string
	}{
		{"\\Seen", jmap.KeywordSeen},
		{"\\SEEN", jmap.KeywordSeen},
		{"\\seen", jmap.KeywordSeen},
		{"\\Answered", jmap.KeywordAnswered},
		{"\\Flagged", jmap.KeywordFlagged},
		{"\\Draft", jmap.KeywordDraft},
		{"\\Deleted", jmap.KeywordDeleted},
		{"\\Junk", jmap.KeywordJunk},
		{"\\Spam", jmap.KeywordJunk},
		{"\\NotJunk", jmap.KeywordNotJunk},
		{"\\Phishing", jmap.KeywordPhishing},
		{"\\Forwarded", jmap.KeywordForwarded},
		{"\\MDNSent", jmap.KeywordMDNSent},
		{"$label1", "$label1"},
		{"custom_flag", "custom_flag"},
	}

	for _, tt := range tests {
		got := jmap.IMAPFlagToJMAPKeyword(tt.imapFlag)
		if got != tt.expected {
			t.Errorf("IMAPFlagToJMAPKeyword(%q) = %q; want %q", tt.imapFlag, got, tt.expected)
		}
	}
}

// TestRFC9979_JMAPKeywordToIMAPFlag tests translation of JMAP keywords to IMAP system flags per RFC 9979.
func TestRFC9979_JMAPKeywordToIMAPFlag(t *testing.T) {
	tests := []struct {
		keyword  string
		expected string
	}{
		{jmap.KeywordSeen, "\\Seen"},
		{jmap.KeywordAnswered, "\\Answered"},
		{jmap.KeywordFlagged, "\\Flagged"},
		{jmap.KeywordDraft, "\\Draft"},
		{jmap.KeywordDeleted, "\\Deleted"},
		{jmap.KeywordJunk, "\\Junk"},
		{jmap.KeywordNotJunk, "\\NotJunk"},
		{jmap.KeywordPhishing, "\\Phishing"},
		{jmap.KeywordForwarded, "\\Forwarded"},
		{jmap.KeywordMDNSent, "\\MDNSent"},
		{"$custom", "$custom"},
		{"work", "\\work"},
	}

	for _, tt := range tests {
		got := jmap.JMAPKeywordToIMAPFlag(tt.keyword)
		if got != tt.expected {
			t.Errorf("JMAPKeywordToIMAPFlag(%q) = %q; want %q", tt.keyword, got, tt.expected)
		}
	}
}

// TestRFC9979_IMAPAttributeToJMAPRole tests translation of IMAP special-use attributes to JMAP mailbox roles per RFC 9979.
func TestRFC9979_IMAPAttributeToJMAPRole(t *testing.T) {
	tests := []struct {
		attr     string
		expected string
	}{
		{"\\All", jmap.RoleAll},
		{"\\Archive", jmap.RoleArchive},
		{"\\Drafts", jmap.RoleDrafts},
		{"\\Flagged", jmap.RoleFlagged},
		{"\\Junk", jmap.RoleJunk},
		{"\\Sent", jmap.RoleSent},
		{"\\Trash", jmap.RoleTrash},
		{"\\Important", jmap.RoleImportant},
	}

	for _, tt := range tests {
		got := jmap.IMAPAttributeToJMAPRole(tt.attr)
		if got != tt.expected {
			t.Errorf("IMAPAttributeToJMAPRole(%q) = %q; want %q", tt.attr, got, tt.expected)
		}

		gotRoleToAttr := jmap.JMAPRoleToIMAPAttribute(tt.expected)
		if gotRoleToAttr != tt.attr {
			t.Errorf("JMAPRoleToIMAPAttribute(%q) = %q; want %q", tt.expected, gotRoleToAttr, tt.attr)
		}
	}
}

// TestRFC9979_KeywordValidationAndBatchMapping tests keyword validation and bulk batch flag/keyword mapping.
func TestRFC9979_KeywordValidationAndBatchMapping(t *testing.T) {
	// 1. Keyword validation
	if !jmap.IsValidJMAPKeyword("$seen") {
		t.Errorf("Expected '$seen' to be valid JMAP keyword")
	}
	if !jmap.IsValidJMAPKeyword("custom-kw_1") {
		t.Errorf("Expected 'custom-kw_1' to be valid JMAP keyword")
	}
	if jmap.IsValidJMAPKeyword("invalid keyword space") {
		t.Errorf("Expected whitespace keyword to be invalid")
	}

	// 2. Batch mapping IMAP flags -> JMAP keywords map
	flags := []string{"\\Seen", "\\Flagged", "$label1"}
	kwMap := jmap.MapIMAPFlagsToJMAPKeywords(flags)
	expectedKwMap := map[string]bool{
		"$seen":   true,
		"$flagged": true,
		"$label1": true,
	}
	if !reflect.DeepEqual(kwMap, expectedKwMap) {
		t.Errorf("MapIMAPFlagsToJMAPKeywords(%v) = %v; want %v", flags, kwMap, expectedKwMap)
	}

	// 3. Batch mapping JMAP keywords map -> IMAP flags slice
	flagsBack := jmap.MapJMAPKeywordsToIMAPFlags(expectedKwMap)
	expectedFlagsBack := []string{"$label1", "\\Flagged", "\\Seen"}
	if !reflect.DeepEqual(flagsBack, expectedFlagsBack) {
		t.Errorf("MapJMAPKeywordsToIMAPFlags(%v) = %v; want %v", expectedKwMap, flagsBack, expectedFlagsBack)
	}
}
