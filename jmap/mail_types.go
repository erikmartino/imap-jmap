package jmap

import "encoding/json"

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
	MayAdmin       bool `json:"mayAdmin"`
}

// Mailbox represents a JMAP Mailbox object per RFC 8621 Section 2.
type Mailbox struct {
	ID            Id            `json:"id"`
	Name          string        `json:"name"`
	ParentID      *Id           `json:"parentId"`
	Role          *string       `json:"role"`
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
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (a EmailAddress) MarshalJSON() ([]byte, error) {
	type Alias struct {
		Name  *string `json:"name"`
		Email string  `json:"email"`
	}
	var namePtr *string
	if a.Name != "" {
		namePtr = &a.Name
	}
	return json.Marshal(Alias{
		Name:  namePtr,
		Email: a.Email,
	})
}

func (a *EmailAddress) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name  *string `json:"name"`
		Email string  `json:"email"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Name != nil {
		a.Name = *raw.Name
	} else {
		a.Name = ""
	}
	a.Email = raw.Email
	return nil
}

// EmailHeader represents a raw RFC 5322 email header field.
type EmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// EmailBodyPart represents a body part structure in an Email per RFC 8621 Section 4.1.4.
type EmailBodyPart struct {
	PartID      *string         `json:"partId"`
	BlobID      *Id             `json:"blobId"`
	Size        uint64          `json:"size"`
	Headers     []EmailHeader   `json:"headers,omitempty"`
	Name        *string         `json:"name"`
	Type        string          `json:"type"`
	Charset     *string         `json:"charset"`
	Disposition *string         `json:"disposition"`
	CID         *string         `json:"cid"`
	Language    []string        `json:"language"`
	Location    *string         `json:"location"`
	SubParts    []EmailBodyPart `json:"subParts,omitempty"`
}

// EmailBodyValue represents decoded body value details per RFC 8621 Section 4.1.4.
type EmailBodyValue struct {
	Value             string `json:"value"`
	IsEncodingProblem bool   `json:"isEncodingProblem"`
	IsTruncated       bool   `json:"isTruncated"`
}

// SmimeVerificationResult represents the result of Email/verifySmime per RFC 9219 Section 4.
type SmimeVerificationResult struct {
	SmimeStatus       string   `json:"smimeStatus"`
	SmimeStatusAt     string   `json:"smimeStatusAt"`
	SmimeErrors       []string `json:"smimeErrors,omitempty"`
	SmimeVerifiedWith *string  `json:"smimeVerifiedWith,omitempty"`
}

// Email represents a JMAP Email object per RFC 8621 & RFC 9219.
type Email struct {
	ID            Id                        `json:"id"`
	BlobID        Id                        `json:"blobId"`
	ThreadID      Id                        `json:"threadId"`
	MailboxIDs    map[Id]bool               `json:"mailboxIds"`
	Keywords      map[string]bool           `json:"keywords"`
	Size          uint64                    `json:"size"`
	ReceivedAt    string                    `json:"receivedAt"`
	MessageID     []string                  `json:"messageId"`
	InReplyTo     []string                  `json:"inReplyTo"`
	References    []string                  `json:"references"`
	Sender        []EmailAddress            `json:"sender"`
	From          []EmailAddress            `json:"from"`
	To            []EmailAddress            `json:"to"`
	CC            []EmailAddress            `json:"cc"`
	BCC           []EmailAddress            `json:"bcc"`
	ReplyTo       []EmailAddress            `json:"replyTo"`
	Subject       string                    `json:"subject"`
	SentAt        *string                   `json:"sentAt"`
	Headers       []EmailHeader             `json:"headers"`
	BodyStructure EmailBodyPart             `json:"bodyStructure"`
	BodyValues    map[string]EmailBodyValue `json:"bodyValues,omitempty"`
	TextBody      []EmailBodyPart           `json:"textBody"`
	HTMLBody      []EmailBodyPart           `json:"htmlBody"`
	Attachments   []EmailBodyPart           `json:"attachments"`
	HasAttachment bool                      `json:"hasAttachment"`
	Preview       string                    `json:"preview"`

	// S/MIME Verification Extensions (RFC 9219 Section 3)
	SMIMEStatus       *string  `json:"smimeStatus,omitempty"`
	SMIMEStatusAt     *string  `json:"smimeStatusAt,omitempty"`
	SMIMEErrors       []string `json:"smimeErrors,omitempty"`
	SMIMEVerifiedWith *string  `json:"smimeVerifiedWith,omitempty"`
}

// Identity represents a JMAP Identity object per RFC 8621 Section 6.
type Identity struct {
	ID            Id             `json:"id"`
	Name          string         `json:"name"`
	Email         string         `json:"email"`
	ReplyTo       []EmailAddress `json:"replyTo"`
	BCC           []EmailAddress `json:"bcc"`
	TextSignature string         `json:"textSignature"`
	HTMLSignature string         `json:"htmlSignature"`
	MayDelete     bool           `json:"mayDelete"`
}

// SubmissionAddress represents a mail address in an EmailSubmission envelope per RFC 8621 Section 7.1.
type SubmissionAddress struct {
	Email      string         `json:"email"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// SubmissionEnvelope represents the SMTP envelope for EmailSubmission per RFC 8621 Section 7.1.
type SubmissionEnvelope struct {
	MailFrom SubmissionAddress   `json:"mailFrom"`
	RcptTo   []SubmissionAddress `json:"rcptTo"`
}

// DeliveryStatus represents recipient delivery status in EmailSubmission per RFC 8621 Section 7.1.
type DeliveryStatus struct {
	SmtpReply string `json:"smtpReply,omitempty"`
	Delivered string `json:"delivered"` // "queued", "yes", "no", "failed"
	Displayed string `json:"displayed,omitempty"`
}

// DSNParameters represents Delivery Status Notification parameters per RFC 8621 Section 7.1.
type DSNParameters struct {
	Ret   string `json:"ret,omitempty"`
	Envid string `json:"envid,omitempty"`
}

// MDNParameters represents Message Disposition Notification parameters per RFC 8621 Section 7.1.
type MDNParameters struct {
	Disposition       string `json:"disposition,omitempty"`
	FinalRecipient    string `json:"finalRecipient,omitempty"`
	OriginalMessageID string `json:"originalMessageId,omitempty"`
}

// EmailSubmission represents a JMAP EmailSubmission object per RFC 8621 Section 7.
type EmailSubmission struct {
	ID             Id                        `json:"id"`
	IdentityID     Id                        `json:"identityId"`
	EmailID        Id                        `json:"emailId"`
	ThreadID       Id                        `json:"threadId"`
	Envelope       *SubmissionEnvelope       `json:"envelope,omitempty"`
	SendAt         string                    `json:"sendAt"`
	UndoStatus     string                    `json:"undoStatus"`
	DeliveryStatus map[string]DeliveryStatus `json:"deliveryStatus,omitempty"`
	DSN            *DSNParameters            `json:"dsn,omitempty"`
	MDN            *MDNParameters            `json:"mdn,omitempty"`
}

// SearchSnippet represents a JMAP SearchSnippet object per RFC 8621 Section 5.
type SearchSnippet struct {
	AccountID string  `json:"accountId"`
	EmailID   Id      `json:"emailId"`
	Subject   *string `json:"subject"`
	Preview   *string `json:"preview"`
}

// VacationResponse is the per-account auto-reply singleton per RFC 8621 Section 8.
// Its id is always "singleton".
type VacationResponse struct {
	ID        Id      `json:"id"`
	IsEnabled bool    `json:"isEnabled"`
	FromDate  *string `json:"fromDate"`
	ToDate    *string `json:"toDate"`
	Subject   *string `json:"subject"`
	TextBody  *string `json:"textBody"`
	HTMLBody  *string `json:"htmlBody"`
}
