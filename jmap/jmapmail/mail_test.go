package jmapmail_test

import (
	"encoding/json"
	"testing"

	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/spectest"
)

func TestEmailAddressJSON(t *testing.T) {
	spectest.RequireID(t, "RFC8621#4.1.2-p1-MUST", "EmailAddress structure")

	addr := jmapmail.EmailAddress{Name: "Alice", Email: "alice@example.com"}
	b, err := json.Marshal(addr)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed jmapmail.EmailAddress
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if parsed.Name != "Alice" || parsed.Email != "alice@example.com" {
		t.Fatalf("unexpected unmarshaled: %v", parsed)
	}

	// Address without name
	addrNoName := jmapmail.EmailAddress{Email: "bob@example.com"}
	b2, err := json.Marshal(addrNoName)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var parsed2 jmapmail.EmailAddress
	if err := json.Unmarshal(b2, &parsed2); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if parsed2.Name != "" || parsed2.Email != "bob@example.com" {
		t.Fatalf("unexpected unmarshaled: %v", parsed2)
	}
}

func TestQuotaDataTypes(t *testing.T) {
	spectest.RequireID(t, "RFC9425#4-p1-MUST", "Quota object")

	q := jmapmail.Quota{
		ID:           "quota-1",
		ResourceType: "octets",
		Used:         1000,
		HardLimit:    10000,
		Scope:        "account",
		Name:         "Storage",
		DataTypes:    []string{"Mail", "Calendar"},
	}

	b, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed jmapmail.Quota
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if parsed.Name != "Storage" || len(parsed.DataTypes) != 2 || parsed.DataTypes[0] != "Mail" {
		t.Fatalf("unexpected unmarshaled: %v", parsed)
	}
}
