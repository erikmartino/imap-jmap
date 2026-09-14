package imap

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/emersion/go-imap/v2"
)

// EmailIDFor constructs a composite Email ID string from a Mailbox ID and an IMAP UID.
func EmailIDFor(mbID string, uid uint32) string {
	return fmt.Sprintf("%s-%d", mbID, uid)
}

// ParseEmailID deconstructs an Email ID into its Mailbox ID and IMAP UID.
func ParseEmailID(id string) (string, uint32, error) {
	lastIdx := strings.LastIndex(id, "-")
	if lastIdx == -1 {
		lastIdx = strings.LastIndex(id, ":")
	}
	if lastIdx == -1 {
		return "", 0, fmt.Errorf("invalid email id format: %s", id)
	}
	uidStr := id[lastIdx+1:]
	uid, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("invalid uid in email id: %w", err)
	}
	return id[:lastIdx], uint32(uid), nil
}

// ThreadIDFor generates an ID for a thread from a Message-ID.
func ThreadIDFor(messageID string, fallback string) string {
	cleaned := strings.Trim(messageID, "<>")
	if cleaned == "" {
		return fallback
	}
	valid := true
	for _, r := range cleaned {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			valid = false
			break
		}
	}
	if valid && len(cleaned) <= 255 {
		return cleaned
	}
	h := sha256.Sum256([]byte(cleaned))
	return "t-" + base64.RawURLEncoding.EncodeToString(h[:16])
}

// MapIMAPFlagsToKeywords converts IMAP flags to standard keywords map.
func MapIMAPFlagsToKeywords(flags []imap.Flag) map[string]bool {
	keywords := make(map[string]bool)
	for _, flag := range flags {
		switch flag {
		case imap.FlagSeen:
			keywords["$seen"] = true
		case imap.FlagFlagged:
			keywords["$flagged"] = true
		case imap.FlagDraft:
			keywords["$draft"] = true
		case imap.FlagAnswered:
			keywords["$answered"] = true
		default:
			s := strings.ToLower(string(flag))
			if s != "" && !strings.HasPrefix(s, "\\") {
				keywords[s] = true
			}
		}
	}
	return keywords
}

// MapFlagsToKeywords converts string flags to standard keywords map.
func MapFlagsToKeywords(flagStrs []string) map[string]bool {
	flags := make([]imap.Flag, 0, len(flagStrs))
	for _, s := range flagStrs {
		flags = append(flags, imap.Flag(s))
	}
	return MapIMAPFlagsToKeywords(flags)
}

// MapKeywordsToIMAPFlags converts keywords map to a slice of IMAP flags.
func MapKeywordsToIMAPFlags(keywords map[string]bool) []imap.Flag {
	var flags []imap.Flag
	for kw, val := range keywords {
		if !val {
			continue
		}
		switch kw {
		case "$seen":
			flags = append(flags, imap.FlagSeen)
		case "$flagged":
			flags = append(flags, imap.FlagFlagged)
		case "$draft":
			flags = append(flags, imap.FlagDraft)
		case "$answered":
			flags = append(flags, imap.FlagAnswered)
		default:
			flags = append(flags, imap.Flag(kw))
		}
	}
	return flags
}

// MapJMAPKeywordToIMAPFlag converts a JMAP keyword to its canonical string flag representation.
func MapJMAPKeywordToIMAPFlag(kw string) string {
	lower := strings.ToLower(strings.TrimSpace(kw))
	switch lower {
	case "$seen":
		return "\\Seen"
	case "$flagged":
		return "\\Flagged"
	case "$draft":
		return "\\Draft"
	case "$answered":
		return "\\Answered"
	default:
		if strings.HasPrefix(lower, "$") {
			return lower
		}
		return "$" + lower
	}
}

// MapKeywordsToFlags converts keywords map to a slice of string flags.
func MapKeywordsToFlags(keywords map[string]bool) []string {
	flags := MapKeywordsToIMAPFlags(keywords)
	res := make([]string, 0, len(flags))
	for _, f := range flags {
		res = append(res, string(f))
	}
	return res
}

// MailboxIDForName converts an IMAP folder name to a Mailbox ID.
func MailboxIDForName(name string) string {
	switch strings.ToLower(name) {
	case "inbox":
		return "mb-inbox"
	case "drafts":
		return "mb-drafts"
	case "sent":
		return "mb-sent"
	case "trash":
		return "mb-trash"
	case "junk":
		return "mb-junk"
	case "archive":
		return "mb-archive"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

// NameForMailboxID converts a Mailbox ID back to an IMAP folder name.
func NameForMailboxID(id string) (string, error) {
	switch id {
	case "mb-inbox":
		return "INBOX", nil
	case "mb-drafts":
		return "Drafts", nil
	case "mb-sent":
		return "Sent", nil
	case "mb-trash":
		return "Trash", nil
	case "mb-junk":
		return "Junk", nil
	case "mb-archive":
		return "Archive", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", fmt.Errorf("invalid mailbox id: %w", err)
	}
	return string(b), nil
}

// DetectRole determines the mailbox role for an IMAP folder from its name and string attributes.
func DetectRole(name string, attrs []string) string {
	for _, attr := range attrs {
		switch strings.ToLower(attr) {
		case "\\drafts", string(imap.MailboxAttrDrafts):
			return "drafts"
		case "\\sent", string(imap.MailboxAttrSent):
			return "sent"
		case "\\trash", string(imap.MailboxAttrTrash):
			return "trash"
		case "\\junk", string(imap.MailboxAttrJunk):
			return "junk"
		case "\\archive", string(imap.MailboxAttrArchive):
			return "archive"
		}
	}

	lower := strings.ToLower(name)
	switch {
	case strings.EqualFold(name, "INBOX"):
		return "inbox"
	case strings.Contains(lower, "draft"):
		return "drafts"
	case strings.Contains(lower, "sent"):
		return "sent"
	case strings.Contains(lower, "trash") || strings.Contains(lower, "bin") || strings.Contains(lower, "deleted"):
		return "trash"
	case strings.Contains(lower, "junk") || strings.Contains(lower, "spam"):
		return "junk"
	case strings.Contains(lower, "archive"):
		return "archive"
	case strings.Contains(lower, "outbox"):
		return "outbox"
	case strings.Contains(lower, "template"):
		return "templates"
	default:
		return ""
	}
}

// ParentMailboxID returns the parent Mailbox ID for a hierarchical IMAP folder path.
func ParentMailboxID(name string, delimiter rune) *string {
	if delimiter == 0 {
		delimiter = '/'
	}
	parts := strings.Split(name, string(delimiter))
	if len(parts) > 1 {
		parentName := strings.Join(parts[:len(parts)-1], string(delimiter))
		pID := MailboxIDForName(parentName)
		return &pID
	}
	return nil
}
