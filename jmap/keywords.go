package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

// Standard RFC 9979 JMAP Keyword Constants per RFC 9979 Section 3.
const (
	KeywordSeen      = jmapmail.KeywordSeen
	KeywordAnswered  = jmapmail.KeywordAnswered
	KeywordFlagged   = jmapmail.KeywordFlagged
	KeywordDraft     = jmapmail.KeywordDraft
	KeywordDeleted   = jmapmail.KeywordDeleted
	KeywordJunk      = jmapmail.KeywordJunk
	KeywordNotJunk   = jmapmail.KeywordNotJunk
	KeywordPhishing  = jmapmail.KeywordPhishing
	KeywordForwarded = jmapmail.KeywordForwarded
	KeywordMDNSent   = jmapmail.KeywordMDNSent
)

// Standard RFC 9979 and RFC 8621 JMAP Mailbox Role Constants.
const (
	RoleInbox      = jmapmail.RoleInbox
	RoleAll        = jmapmail.RoleAll
	RoleArchive    = jmapmail.RoleArchive
	RoleDrafts     = jmapmail.RoleDrafts
	RoleFlagged    = jmapmail.RoleFlagged
	RoleJunk       = jmapmail.RoleJunk
	RoleSent       = jmapmail.RoleSent
	RoleTrash      = jmapmail.RoleTrash
	RoleImportant  = jmapmail.RoleImportant
	RoleSubscribed = jmapmail.RoleSubscribed
)

var (
	IMAPFlagToJMAPKeyword    = jmapmail.IMAPFlagToJMAPKeyword
	JMAPKeywordToIMAPFlag    = jmapmail.JMAPKeywordToIMAPFlag
	IMAPAttributeToJMAPRole  = jmapmail.IMAPAttributeToJMAPRole
	JMAPRoleToIMAPAttribute  = jmapmail.JMAPRoleToIMAPAttribute
	IsValidJMAPKeyword       = jmapmail.IsValidJMAPKeyword
	NormalizeJMAPKeyword     = jmapmail.NormalizeJMAPKeyword
	MapIMAPFlagsToJMAPKeywords = jmapmail.MapIMAPFlagsToJMAPKeywords
	MapJMAPKeywordsToIMAPFlags = jmapmail.MapJMAPKeywordsToIMAPFlags
)
