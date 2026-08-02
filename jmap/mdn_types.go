package jmap

// MDNDisposition represents disposition details of an MDN object per RFC 9007 Section 2.
type MDNDisposition struct {
	ActionMode  string `json:"actionMode"`
	SendingMode string `json:"sendingMode"`
	Type        string `json:"type"`
}

// MDN represents a JMAP Message Disposition Notification object per RFC 9007 Section 2.
type MDN struct {
	ID                     Id                `json:"id,omitempty"`
	ForEmailID             Id                `json:"forEmailId"`
	Subject                string            `json:"subject,omitempty"`
	Recipient              string            `json:"recipient,omitempty"`
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
