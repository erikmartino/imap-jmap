package dav_test

import (
	"strings"
	"testing"

	"github.com/emersion/go-vcard"
)

// TestRFC6350_vCard4Format tests RFC 6350 vCard 4.0 specification formatting.
func TestRFC6350_vCard4Format(t *testing.T) {
	card := make(vcard.Card)
	card.SetValue(vcard.FieldVersion, "4.0")
	card.SetValue(vcard.FieldFormattedName, "John Doe")
	card.SetValue(vcard.FieldEmail, "john.doe@example.com")

	var sb strings.Builder
	enc := vcard.NewEncoder(&sb)
	if err := enc.Encode(card); err != nil {
		t.Fatalf("vcard.Encode failed: %v", err)
	}

	vcardStr := sb.String()
	if !strings.Contains(vcardStr, "BEGIN:VCARD") {
		t.Errorf("Expected BEGIN:VCARD in vCard output")
	}
	if !strings.Contains(vcardStr, "VERSION:4.0") {
		t.Errorf("Expected VERSION:4.0 in vCard output per RFC 6350")
	}
	if !strings.Contains(vcardStr, "FN:John Doe") {
		t.Errorf("Expected FN:John Doe in vCard output per RFC 6350")
	}
}

// TestRFC6350_vCard4Properties tests encoding/decoding of vCard 4.0 properties per RFC 6350.
func TestRFC6350_vCard4Properties(t *testing.T) {
	vcardStr := "BEGIN:VCARD\r\nVERSION:4.0\r\nKIND:individual\r\nFN:Dr. Jane Smith\r\nTEL;VALUE=uri;TYPE=work,voice;PREF=1:tel:+15551234567\r\nORG:Acme Corp;Engineering\r\nTITLE:Software Architect\r\nNOTE:VCard 4.0 Test Note\r\nEND:VCARD\r\n"

	dec := vcard.NewDecoder(strings.NewReader(vcardStr))
	card, err := dec.Decode()
	if err != nil {
		t.Fatalf("Decode vCard 4.0 failed: %v", err)
	}

	if card.Value(vcard.FieldVersion) != "4.0" {
		t.Errorf("Expected VERSION 4.0, got %q", card.Value(vcard.FieldVersion))
	}
	if card.Value(vcard.FieldFormattedName) != "Dr. Jane Smith" {
		t.Errorf("Expected FN 'Dr. Jane Smith', got %q", card.Value(vcard.FieldFormattedName))
	}
	if card.Value(vcard.FieldOrganization) != "Acme Corp;Engineering" {
		t.Errorf("Expected ORG 'Acme Corp;Engineering', got %q", card.Value(vcard.FieldOrganization))
	}
	if card.Value(vcard.FieldTitle) != "Software Architect" {
		t.Errorf("Expected TITLE 'Software Architect', got %q", card.Value(vcard.FieldTitle))
	}
	if card.Value(vcard.FieldNote) != "VCard 4.0 Test Note" {
		t.Errorf("Expected NOTE 'VCard 4.0 Test Note', got %q", card.Value(vcard.FieldNote))
	}
}
