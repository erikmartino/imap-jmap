package jmap

// IMAPAccount represents an IMAP server access configuration per RFC 9698 Section 3.
type IMAPAccount struct {
	ID        Id     `json:"id"`
	Host      string `json:"host"`
	Port      uint32 `json:"port"`
	TLS       string `json:"tls"` // "always", "starttls", "never"
	Username  string `json:"username"`
	State     string `json:"state"`               // "connected", "disabled", "error"
	LastError string `json:"lastError,omitempty"` // Error description if state is "error"
}
