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
