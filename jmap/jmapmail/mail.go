package jmapmail

import (
	"context"
	"encoding/json"

	"imap-jmap/jmap/jmapcore"
)

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
	ID            jmapcore.Id   `json:"id"`
	Name          string        `json:"name"`
	ParentID      *jmapcore.Id  `json:"parentId"`
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
	ID       jmapcore.Id   `json:"id"`
	EmailIDs []jmapcore.Id `json:"emailIds"`
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
	BlobID      *jmapcore.Id    `json:"blobId"`
	Size        uint64          `json:"size"`
	Headers     []EmailHeader   `json:"headers,omitempty"`
	Name        *string         `json:"name"`
	Type        string          `json:"type"`
	Charset     *string         `json:"charset"`
	Disposition *string         `json:"disposition"`
	CID         *string         `json:"cid"`
	Language    []string        `json:"language,omitempty"`
	Location    *string         `json:"location"`
	SubParts    []EmailBodyPart `json:"subParts,omitempty"`
}

// EmailBodyValue represents the decoded body contents for a part per RFC 8621 Section 4.1.4.
type EmailBodyValue struct {
	Value             string `json:"value"`
	IsEncodingProblem bool   `json:"isEncodingProblem"`
	IsTruncated       bool   `json:"isTruncated"`
}

// SmimeVerificationResult holds the verification outcome for an email per RFC 9219 Section 4.
type SmimeVerificationResult struct {
	SmimeStatus       string   `json:"smimeStatus"`
	SmimeStatusAt     string   `json:"smimeStatusAt"`
	SmimeErrors       []string `json:"smimeErrors,omitempty"`
	SmimeVerifiedWith *string  `json:"smimeVerifiedWith,omitempty"`
}

// Email represents a JMAP Email object per RFC 8621 & RFC 9219.
type Email struct {
	ID            jmapcore.Id               `json:"id"`
	BlobID        jmapcore.Id               `json:"blobId"`
	ThreadID      jmapcore.Id               `json:"threadId"`
	MailboxIDs    map[jmapcore.Id]bool      `json:"mailboxIds"`
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
	ID            jmapcore.Id    `json:"id"`
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
	ID             jmapcore.Id               `json:"id"`
	IdentityID     jmapcore.Id               `json:"identityId"`
	EmailID        jmapcore.Id               `json:"emailId"`
	ThreadID       jmapcore.Id               `json:"threadId"`
	Envelope       *SubmissionEnvelope       `json:"envelope,omitempty"`
	SendAt         string                    `json:"sendAt"`
	UndoStatus     string                    `json:"undoStatus"`
	DeliveryStatus map[string]DeliveryStatus `json:"deliveryStatus,omitempty"`
	DSN            *DSNParameters            `json:"dsn,omitempty"`
	MDN            *MDNParameters            `json:"mdn,omitempty"`
}

// SearchSnippet represents a JMAP SearchSnippet object per RFC 8621 Section 5.
type SearchSnippet struct {
	AccountID string      `json:"accountId"`
	EmailID   jmapcore.Id `json:"emailId"`
	Subject   *string     `json:"subject"`
	Preview   *string     `json:"preview"`
}

// VacationResponse is the per-account auto-reply singleton per RFC 8621 Section 8.
type VacationResponse struct {
	ID        jmapcore.Id `json:"id"`
	IsEnabled bool        `json:"isEnabled"`
	FromDate  *string     `json:"fromDate"`
	ToDate    *string     `json:"toDate"`
	Subject   *string     `json:"subject"`
	TextBody  *string     `json:"textBody"`
	HTMLBody  *string     `json:"htmlBody"`
}

// PushSubscription represents a JMAP Web Push subscription per RFC 8620 Section 7.2.
type PushSubscription struct {
	ID               jmapcore.Id           `json:"id,omitempty"`
	DeviceClientID   string                `json:"deviceClientId"`
	URL              string                `json:"url"`
	Keys             *PushSubscriptionKeys `json:"keys,omitempty"`
	VerificationCode *string               `json:"verificationCode,omitempty"`
	Expires          *string               `json:"expires,omitempty"`
	Types            []string              `json:"types,omitempty"`
}

// PushSubscriptionKeys holds client-provided encryption keys for push message encryption per RFC 8620 Section 7.2.
type PushSubscriptionKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// Quota represents a JMAP Quota object per RFC 9425 Section 4.
type Quota struct {
	ID           jmapcore.Id   `json:"id"`
	ResourceType string        `json:"resourceType"` // "count" or "octets"
	Used         uint64        `json:"used"`
	HardLimit    uint64        `json:"hardLimit"`
	WarnLimit    *uint64       `json:"warnLimit,omitempty"`
	SoftLimit    *uint64       `json:"softLimit,omitempty"`
	Scope        string        `json:"scope"` // "global" or "account"
	Name         string        `json:"name"`
	Description  *string       `json:"description,omitempty"`
	AccountIDs   []jmapcore.Id `json:"accountIds,omitempty"`
	DataTypes    []string      `json:"dataTypes,omitempty"`
}

// MDNDisposition represents disposition details of an MDN object per RFC 9007 Section 2.
type MDNDisposition struct {
	ActionMode  string `json:"actionMode"`
	SendingMode string `json:"sendingMode"`
	Type        string `json:"type"`
}

// MDN represents a JMAP Message Disposition Notification object per RFC 9007 Section 2.
type MDN struct {
	ID                     jmapcore.Id       `json:"id,omitempty"`
	ForEmailID             jmapcore.Id       `json:"forEmailId"`
	Subject                string            `json:"subject,omitempty"`
	Recipient              string            `json:"recipient,omitempty"`
	FinalRecipient         string            `json:"finalRecipient,omitempty"`
	ReportingUA            string            `json:"reportingUA,omitempty"`
	Disposition            MDNDisposition    `json:"disposition"`
	TextBody               string            `json:"textBody,omitempty"`
	IncludeOriginalMessage bool              `json:"includeOriginalMessage,omitempty"`
	MDNGateway             string            `json:"mdnGateway,omitempty"`
	OriginalRecipient      string            `json:"originalRecipient,omitempty"`
	OriginalMessageID      string            `json:"originalMessageId,omitempty"`
	Error                  []string          `json:"error,omitempty"`
	ExtensionFields        map[string]string `json:"extensionFields,omitempty"`
}

// OutboundDeliveryResult is the outcome of delivering a raw message to one external
// recipient via the outbound relay.
type OutboundDeliveryResult struct {
	Delivered bool
	SmtpReply string
}

// OutboundMailSender delivers a raw RFC 5322 message to external recipients by
// relaying it to the recipient domain's SMTP servers (RFC 5321 Section 5.1).
type OutboundMailSender interface {
	SendMail(ctx context.Context, from string, recipients []string, rawMessage []byte) map[string]OutboundDeliveryResult
}

// SMTPAvailableBackend is an optional interface that MailBackend implementations can fulfill
// to indicate whether an outer SMTP server is available for outbound message dispatch.
type SMTPAvailableBackend interface {
	HasSMTPServer() bool
}

// MailBackend defines the storage interface for JMAP Mail & Quota resources per RFC 8621, RFC 9219, & RFC 9425.
type MailBackend interface {
	// State returns the current change state token for mail data.
	State(ctx context.Context) string

	// Mailboxes (RFC 8621 Section 2)
	MailboxState(ctx context.Context) string
	MailboxChanges(ctx context.Context, sinceState string, maxChanges *uint64) (created, updated, destroyed []jmapcore.Id, updatedProperties []string, newState string, hasMoreChanges bool)
	GetMailboxes(ctx context.Context, ids []jmapcore.Id) (list []*Mailbox, notFound []jmapcore.Id, err error)
	GetAllMailboxes(ctx context.Context) ([]*Mailbox, error)
	CreateMailbox(ctx context.Context, mb *Mailbox) (*Mailbox, error)
	UpdateMailbox(ctx context.Context, id jmapcore.Id, patch map[string]any) (*Mailbox, error)
	DeleteMailbox(ctx context.Context, id jmapcore.Id, onDestroyRemoveMessages bool) (bool, error)

	// Threads (RFC 8621 Section 3)
	ThreadState(ctx context.Context) string
	ThreadChanges(ctx context.Context, sinceState string, maxChanges *uint64) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetThreads(ctx context.Context, ids []jmapcore.Id) (list []*Thread, notFound []jmapcore.Id, err error)
	GetAllThreads(ctx context.Context) ([]*Thread, error)

	// Emails (RFC 8621 Section 4)
	EmailState(ctx context.Context) string
	EmailChanges(ctx context.Context, sinceState string, maxChanges *uint64) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetEmails(ctx context.Context, ids []jmapcore.Id) (list []*Email, notFound []jmapcore.Id, err error)
	GetAllEmails(ctx context.Context) ([]*Email, error)
	CreateEmail(ctx context.Context, em *Email) (*Email, error)
	UpdateEmail(ctx context.Context, id jmapcore.Id, patch map[string]any) (*Email, error)
	DeleteEmail(ctx context.Context, id jmapcore.Id) (bool, error)
	QueryEmails(ctx context.Context, filter map[string]any, comparators []jmapcore.Comparator, position int, limit *uint64) (ids []jmapcore.Id, total int, err error)

	// S/MIME Verification (RFC 9219 Section 4)
	VerifySmime(ctx context.Context, ids []jmapcore.Id) (verified map[jmapcore.Id]*SmimeVerificationResult, notFound []jmapcore.Id, err error)

	// Quotas (RFC 9425 Section 4)
	QuotaState(ctx context.Context) string
	QuotaChanges(ctx context.Context, sinceState string, maxChanges *uint64) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetQuotas(ctx context.Context, ids []jmapcore.Id) (list []*Quota, notFound []jmapcore.Id, err error)
	GetAllQuotas(ctx context.Context) ([]*Quota, error)

	// Identities (RFC 8621 Section 6)
	IdentityState(ctx context.Context) string
	IdentityChanges(ctx context.Context, sinceState string, maxChanges *uint64) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetIdentities(ctx context.Context) ([]*Identity, error)
	CreateIdentity(ctx context.Context, identity *Identity) (*Identity, error)
	UpdateIdentity(ctx context.Context, id jmapcore.Id, patch map[string]any) (*Identity, error)
	DeleteIdentity(ctx context.Context, id jmapcore.Id) (bool, error)

	// VacationResponse is a per-account singleton (id "singleton") per RFC 8621 Section 8.
	VacationResponseState(ctx context.Context) string
	GetVacationResponse(ctx context.Context) (*VacationResponse, error)
	UpdateVacationResponse(ctx context.Context, patch map[string]any) (*VacationResponse, error)

	// Submissions (RFC 8621 Section 7)
	SubmissionState(ctx context.Context) string
	SubmissionChanges(ctx context.Context, sinceState string, maxChanges *uint64) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	CreateSubmission(ctx context.Context, sub *EmailSubmission) (*EmailSubmission, error)
	UpdateSubmission(ctx context.Context, id jmapcore.Id, patch map[string]any) (*EmailSubmission, error)
	DeleteSubmission(ctx context.Context, id jmapcore.Id) (bool, error)
	GetSubmissions(ctx context.Context, ids []jmapcore.Id) (list []*EmailSubmission, notFound []jmapcore.Id, err error)
	GetAllSubmissions(ctx context.Context) ([]*EmailSubmission, error)
	QuerySubmissions(ctx context.Context, filter map[string]any, comparators []jmapcore.Comparator, position int, limit *uint64) ([]jmapcore.Id, int, error)

	// MDN (RFC 9007 Section 3)
	SendMDN(ctx context.Context, mdn *MDN) (*MDN, error)
	ParseMDN(ctx context.Context, blobID jmapcore.Id) (*MDN, error)

	// PushSubscription (RFC 8620 Section 7.2)
	GetPushSubscriptions(ctx context.Context, ids []jmapcore.Id) (list []*PushSubscription, notFound []jmapcore.Id, err error)
	GetAllPushSubscriptions(ctx context.Context) ([]*PushSubscription, error)
	CreatePushSubscription(ctx context.Context, sub *PushSubscription) (*PushSubscription, error)
	UpdatePushSubscription(ctx context.Context, id jmapcore.Id, patch map[string]any) (*PushSubscription, error)
	DeletePushSubscription(ctx context.Context, id jmapcore.Id) (bool, error)
}
