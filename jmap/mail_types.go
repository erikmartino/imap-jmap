package jmap

// MailboxRights defines rights on a mailbox per RFC 8621 Section 2.
type MailboxRights struct {
	MayReadItems   bool `json:"mayReadItems"`
	MayAddItems    bool `json:"mayAddItems"`
	MayRemoveItems bool `json:"mayRemoveItems"`
	MaySetSeen     bool `json:"maySetSeen"`
	MaySetKeywords bool `json:"maySetKeywords"`
	MayCreateChild bool `json:"mayCreateChild"`
	MayRename      bool `json:"mayRename"`
	MayDelete      bool `json:"mayDelete"`
	MaySubmit      bool `json:"maySubmit"`
}

// Mailbox represents a JMAP Mailbox object per RFC 8621 Section 2.
type Mailbox struct {
	ID            Id            `json:"id"`
	Name          string        `json:"name"`
	ParentID      *Id           `json:"parentId,omitempty"`
	Role          *string       `json:"role,omitempty"`
	SortOrder     uint64        `json:"sortOrder"`
	TotalEmails   uint64        `json:"totalEmails"`
	UnreadEmails  uint64        `json:"unreadEmails"`
	TotalThreads  uint64        `json:"totalThreads"`
	UnreadThreads uint64        `json:"unreadThreads"`
	MyRights      MailboxRights `json:"myRights"`
	IsSubscribed  bool          `json:"isSubscribed"`
}

// Thread represents a JMAP Thread object per RFC 8621 Section 3.
type Thread struct {
	ID       Id   `json:"id"`
	EmailIDs []Id `json:"emailIds"`
}

// EmailAddress represents an address structure in Email headers per RFC 8621 Section 4.1.2.
type EmailAddress struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// EmailHeader represents a raw RFC 5322 email header field.
type EmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// EmailBodyPart represents a body part structure in an Email per RFC 8621 Section 4.1.4.
type EmailBodyPart struct {
	PartID      string        `json:"partId,omitempty"`
	BlobID      Id            `json:"blobId,omitempty"`
	Size        uint64        `json:"size"`
	Type        string        `json:"type"`
	Subtype     string        `json:"subtype,omitempty"`
	Name        string        `json:"name,omitempty"`
	Disposition string        `json:"disposition,omitempty"`
	Language    []string      `json:"language,omitempty"`
	CID         string        `json:"cid,omitempty"`
	Location    string        `json:"location,omitempty"`
	Headers     []EmailHeader `json:"headers,omitempty"`
}

// EmailBodyValue represents decoded body value details per RFC 8621 Section 4.1.4.
type EmailBodyValue struct {
	Value             string `json:"value"`
	IsEncodingProblem bool   `json:"isEncodingProblem,omitempty"`
	IsTruncated       bool   `json:"isTruncated,omitempty"`
}

// Email represents a JMAP Email object per RFC 8621 Section 4.
type Email struct {
	ID            Id                        `json:"id"`
	BlobID        Id                        `json:"blobId"`
	ThreadID      Id                        `json:"threadId"`
	MailboxIDs    map[Id]bool               `json:"mailboxIds"`
	Keywords      map[string]bool           `json:"keywords"`
	Size          uint64                    `json:"size"`
	ReceivedAt    string                    `json:"receivedAt"`
	MessageID     []string                  `json:"messageId,omitempty"`
	InReplyTo     []string                  `json:"inReplyTo,omitempty"`
	References    []string                  `json:"references,omitempty"`
	Sender        []EmailAddress            `json:"sender,omitempty"`
	From          []EmailAddress            `json:"from,omitempty"`
	To            []EmailAddress            `json:"to,omitempty"`
	CC            []EmailAddress            `json:"cc,omitempty"`
	BCC           []EmailAddress            `json:"bcc,omitempty"`
	ReplyTo       []EmailAddress            `json:"replyTo,omitempty"`
	Subject       string                    `json:"subject"`
	SentAt        string                    `json:"sentAt,omitempty"`
	BodyStructure EmailBodyPart             `json:"bodyStructure"`
	BodyValues    map[string]EmailBodyValue `json:"bodyValues,omitempty"`
	TextBody      []EmailBodyPart           `json:"textBody,omitempty"`
	HTMLBody      []EmailBodyPart           `json:"htmlBody,omitempty"`
	Attachments   []EmailBodyPart           `json:"attachments,omitempty"`
	HasAttachment bool                      `json:"hasAttachment"`
	Preview       string                    `json:"preview"`
}

// Identity represents a JMAP Identity object per RFC 8621 Section 6.
type Identity struct {
	ID            Id             `json:"id"`
	Name          string         `json:"name"`
	Email         string         `json:"email"`
	ReplyTo       []EmailAddress `json:"replyTo,omitempty"`
	BCC           []EmailAddress `json:"bcc,omitempty"`
	TextSignature string         `json:"textSignature,omitempty"`
	HTMLSignature string         `json:"htmlSignature,omitempty"`
}

// EmailSubmission represents a JMAP EmailSubmission object per RFC 8621 Section 7.
type EmailSubmission struct {
	ID             Id             `json:"id"`
	IdentityID     Id             `json:"identityId"`
	EmailID        Id             `json:"emailId"`
	ThreadID       Id             `json:"threadId"`
	SendAt         string         `json:"sendAt"`
	UndoStatus     string         `json:"undoStatus"`
	DeliveryStatus map[string]any `json:"deliveryStatus,omitempty"`
}

// SearchSnippet represents a JMAP SearchSnippet object per RFC 8621 Section 5.
type SearchSnippet struct {
	AccountID string  `json:"accountId"`
	EmailID   Id      `json:"emailId"`
	Subject   *string `json:"subject,omitempty"`
	Preview   *string `json:"preview,omitempty"`
}
