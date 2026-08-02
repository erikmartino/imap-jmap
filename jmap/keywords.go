package jmap

import (
	"regexp"
	"sort"
	"strings"
)

// Standard RFC 9979 JMAP Keyword Constants per RFC 9979 Section 3.
const (
	KeywordSeen      = "$seen"
	KeywordAnswered  = "$answered"
	KeywordFlagged   = "$flagged"
	KeywordDraft     = "$draft"
	KeywordDeleted   = "$deleted"
	KeywordJunk      = "$junk"
	KeywordNotJunk   = "$notjunk"
	KeywordPhishing  = "$phishing"
	KeywordForwarded = "$forwarded"
	KeywordMDNSent   = "$mdnsent"
)

// Standard RFC 9979 JMAP Mailbox Role Constants per RFC 9979 Section 4.
const (
	RoleAll       = "all"
	RoleArchive   = "archive"
	RoleDrafts    = "drafts"
	RoleFlagged   = "flagged"
	RoleJunk      = "junk"
	RoleSent      = "sent"
	RoleTrash     = "trash"
	RoleImportant = "important"
)

var (
	// imapFlagToKeyword maps uppercase IMAP system flags to standard JMAP keywords per RFC 9979.
	imapFlagToKeyword = map[string]string{
		"\\SEEN":      KeywordSeen,
		"\\ANSWERED":  KeywordAnswered,
		"\\FLAGGED":   KeywordFlagged,
		"\\DRAFT":     KeywordDraft,
		"\\DELETED":   KeywordDeleted,
		"\\JUNK":      KeywordJunk,
		"\\SPAM":      KeywordJunk,
		"\\NOTJUNK":   KeywordNotJunk,
		"\\PHISHING":  KeywordPhishing,
		"\\FORWARDED": KeywordForwarded,
		"\\MDNSENT":   KeywordMDNSent,
	}

	// keywordToIMAPFlag maps standard JMAP keywords to canonical uppercase IMAP system flags per RFC 9979.
	keywordToIMAPFlag = map[string]string{
		KeywordSeen:      "\\Seen",
		KeywordAnswered:  "\\Answered",
		KeywordFlagged:   "\\Flagged",
		KeywordDraft:     "\\Draft",
		KeywordDeleted:   "\\Deleted",
		KeywordJunk:      "\\Junk",
		KeywordNotJunk:   "\\NotJunk",
		KeywordPhishing:  "\\Phishing",
		KeywordForwarded: "\\Forwarded",
		KeywordMDNSent:   "\\MDNSent",
	}

	// imapAttributeToRole maps uppercase IMAP special-use attributes to JMAP mailbox roles per RFC 9979.
	imapAttributeToRole = map[string]string{
		"\\ALL":       RoleAll,
		"\\ARCHIVE":   RoleArchive,
		"\\DRAFTS":    RoleDrafts,
		"\\FLAGGED":   RoleFlagged,
		"\\JUNK":      RoleJunk,
		"\\SENT":      RoleSent,
		"\\TRASH":     RoleTrash,
		"\\IMPORTANT": RoleImportant,
	}

	// roleToIMAPAttribute maps JMAP mailbox roles to canonical uppercase IMAP special-use attributes per RFC 9979.
	roleToIMAPAttribute = map[string]string{
		RoleAll:       "\\All",
		RoleArchive:   "\\Archive",
		RoleDrafts:    "\\Drafts",
		RoleFlagged:   "\\Flagged",
		RoleJunk:      "\\Junk",
		RoleSent:      "\\Sent",
		RoleTrash:     "\\Trash",
		RoleImportant: "\\Important",
	}

	validJMAPKeywordRegex = regexp.MustCompile(`^[$a-zA-Z0-9_-]+$`)
)

// IMAPFlagToJMAPKeyword converts an IMAP system flag (case-insensitive) to its standard JMAP keyword per RFC 9979.
// If the flag is not a standard IMAP system flag, it is normalized to lowercase (removing leading backslash if present).
func IMAPFlagToJMAPKeyword(flag string) string {
	upper := strings.ToUpper(strings.TrimSpace(flag))
	if kw, ok := imapFlagToKeyword[upper]; ok {
		return kw
	}
	cleaned := strings.TrimPrefix(flag, "\\")
	return strings.ToLower(cleaned)
}

// JMAPKeywordToIMAPFlag converts a JMAP keyword (case-insensitive) to its canonical IMAP system flag per RFC 9979.
// If the keyword is a custom keyword, it returns the custom flag name.
func JMAPKeywordToIMAPFlag(keyword string) string {
	lower := strings.ToLower(strings.TrimSpace(keyword))
	if flag, ok := keywordToIMAPFlag[lower]; ok {
		return flag
	}
	if strings.HasPrefix(lower, "$") {
		return keyword
	}
	return "\\" + keyword
}

// IMAPAttributeToJMAPRole converts an IMAP special-use attribute (case-insensitive) to a JMAP mailbox role per RFC 9979.
func IMAPAttributeToJMAPRole(attribute string) string {
	upper := strings.ToUpper(strings.TrimSpace(attribute))
	if role, ok := imapAttributeToRole[upper]; ok {
		return role
	}
	cleaned := strings.TrimPrefix(attribute, "\\")
	return strings.ToLower(cleaned)
}

// JMAPRoleToIMAPAttribute converts a JMAP mailbox role (case-insensitive) to a canonical IMAP special-use attribute per RFC 9979.
func JMAPRoleToIMAPAttribute(role string) string {
	lower := strings.ToLower(strings.TrimSpace(role))
	if attr, ok := roleToIMAPAttribute[lower]; ok {
		return attr
	}
	return "\\" + strings.Title(lower)
}

// IsValidJMAPKeyword checks if a string is a valid JMAP keyword per RFC 8621 / RFC 9979.
// Valid keywords must be non-empty, contain only ASCII alphanumeric characters, '$', '_', or '-', and not contain whitespace.
func IsValidJMAPKeyword(keyword string) bool {
	if keyword == "" || len(keyword) > 255 {
		return false
	}
	return validJMAPKeywordRegex.MatchString(keyword)
}

// NormalizeJMAPKeyword normalizes a keyword to lowercase per RFC 9979 guidelines.
func NormalizeJMAPKeyword(keyword string) string {
	return strings.ToLower(strings.TrimSpace(keyword))
}

// MapIMAPFlagsToJMAPKeywords converts a list of IMAP flags to a JMAP keywords map per RFC 9979.
func MapIMAPFlagsToJMAPKeywords(flags []string) map[string]bool {
	keywords := make(map[string]bool)
	for _, f := range flags {
		kw := IMAPFlagToJMAPKeyword(f)
		if IsValidJMAPKeyword(kw) {
			keywords[kw] = true
		}
	}
	return keywords
}

// MapJMAPKeywordsToIMAPFlags converts a JMAP keywords map to a sorted slice of IMAP flags per RFC 9979.
func MapJMAPKeywordsToIMAPFlags(keywords map[string]bool) []string {
	var flags []string
	for kw, val := range keywords {
		if val {
			flag := JMAPKeywordToIMAPFlag(kw)
			flags = append(flags, flag)
		}
	}
	sort.Strings(flags)
	return flags
}
