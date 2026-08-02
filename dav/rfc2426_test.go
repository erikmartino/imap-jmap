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
