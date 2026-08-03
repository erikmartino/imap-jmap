package dav_test

import (
	"strings"
	"testing"

	"github.com/emersion/go-vcard"
)

// TestRFC2426_vCard3Format tests RFC 2426 vCard 3.0 specification parsing.
func TestRFC2426_vCard3Format(t *testing.T) {
	vcard3Str := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Jane Smith\r\nEMAIL:jane@example.com\r\nEND:VCARD\r\n"
	dec := vcard.NewDecoder(strings.NewReader(vcard3Str))
	card, err := dec.Decode()
	if err != nil {
		t.Fatalf("vcard.Decode failed for RFC 2426 vCard 3.0: %v", err)
	}

	if card.Value(vcard.FieldFormattedName) != "Jane Smith" {
		t.Errorf("Expected FN 'Jane Smith', got %q", card.Value(vcard.FieldFormattedName))
	}
	if card.Value(vcard.FieldVersion) != "3.0" {
		t.Errorf("Expected VERSION '3.0', got %q", card.Value(vcard.FieldVersion))
	}
}

// TestRFC2426_vCard3Properties tests RFC 2426 vCard 3.0 multi-property decoding.
func TestRFC2426_vCard3Properties(t *testing.T) {
	vcard3Str := "BEGIN:VCARD\r\nVERSION:3.0\r\nN:Smith;Jane;M.;Dr.;\r\nFN:Dr. Jane M. Smith\r\nTEL;TYPE=WORK,VOICE:+15550001111\r\nEMAIL;TYPE=INTERNET:jane.smith@example.com\r\nORG:Tech Solutions Inc.\r\nTITLE:CTO\r\nEND:VCARD\r\n"

	dec := vcard.NewDecoder(strings.NewReader(vcard3Str))
	card, err := dec.Decode()
	if err != nil {
		t.Fatalf("Decode vCard 3.0 failed: %v", err)
	}

	if card.Value(vcard.FieldFormattedName) != "Dr. Jane M. Smith" {
		t.Errorf("Expected FN 'Dr. Jane M. Smith', got %q", card.Value(vcard.FieldFormattedName))
	}
	if card.Value(vcard.FieldTelephone) != "+15550001111" {
		t.Errorf("Expected TEL '+15550001111', got %q", card.Value(vcard.FieldTelephone))
	}
	if card.Value(vcard.FieldEmail) != "jane.smith@example.com" {
		t.Errorf("Expected EMAIL 'jane.smith@example.com', got %q", card.Value(vcard.FieldEmail))
	}
}
