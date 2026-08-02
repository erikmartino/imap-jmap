package jmap

// SieveScript represents a SieveScript object per RFC 9661 Section 1.4.
type SieveScript struct {
	ID       Id     `json:"id"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	IsActive bool   `json:"isActive"`
	IsValid  bool   `json:"isValid"`
}
