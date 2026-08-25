package jmap

import (
	"os"
	"strings"
	"testing"
)

func TestStructuredEMLParsing(t *testing.T) {
	raw, err := os.ReadFile("/home/martino/git/fastmail/JMAP-TestSuite/t/corpus/emails/structured.eml")
	if err != nil {
		t.Skipf("structured.eml not found: %v", err)
	}

	em, err := parseRFC822(raw)
	if err != nil {
		t.Fatalf("parseRFC822 failed: %v", err)
	}

	t.Logf("TextBody parts (%d):", len(em.TextBody))
	for i, p := range em.TextBody {
		pID := ""
		if p.PartID != nil {
			pID = *p.PartID
		}
		t.Logf("  [%d] partId=%s type=%s name=%v size=%d val=%q", i, pID, p.Type, p.Name, p.Size, em.BodyValues[pID].Value)
	}

	t.Logf("HTMLBody parts (%d):", len(em.HTMLBody))
	for i, p := range em.HTMLBody {
		pID := ""
		if p.PartID != nil {
			pID = *p.PartID
		}
		t.Logf("  [%d] partId=%s type=%s name=%v size=%d val=%q", i, pID, p.Type, p.Name, p.Size, em.BodyValues[pID].Value)
	}

	t.Logf("Attachments (%d):", len(em.Attachments))
	for i, p := range em.Attachments {
		pID := ""
		if p.PartID != nil {
			pID = *p.PartID
		}
		t.Logf("  [%d] partId=%s type=%s name=%v size=%d", i, pID, p.Type, p.Name, p.Size)
	}
}

func TestRichTextEmailFormatAndParseRoundTrip(t *testing.T) {
	part1 := "1"
	part2 := "2"
	em := &Email{
		Subject: "Rich Text Test",
		From:    []EmailAddress{{Name: "Sender", Email: "sender@example.com"}},
		To:      []EmailAddress{{Name: "Recipient", Email: "recipient@example.com"}},
		TextBody: []EmailBodyPart{{
			PartID: &part1,
			Type:   "text/plain",
		}},
		HTMLBody: []EmailBodyPart{{
			PartID: &part2,
			Type:   "text/html",
		}},
		BodyValues: map[string]EmailBodyValue{
			"1": {Value: "Plain text fallback"},
			"2": {Value: "<p>Rich text <b>bold</b> and <i>italic</i></p>"},
		},
	}

	raw := FormatEmailRFC822(em)
	if len(raw) == 0 {
		t.Fatalf("FormatEmailRFC822 produced empty bytes")
	}

	// Verify MIME contains multipart/alternative
	rawStr := string(raw)
	if !strings.Contains(rawStr, "multipart/alternative") {
		t.Fatalf("expected raw RFC 822 to contain multipart/alternative, got:\n%s", rawStr)
	}
	if !strings.Contains(rawStr, "text/plain") || !strings.Contains(rawStr, "text/html") {
		t.Fatalf("expected raw RFC 822 to contain both text/plain and text/html, got:\n%s", rawStr)
	}

	// Parse back
	parsed, err := ParseRFC822(raw)
	if err != nil {
		t.Fatalf("ParseRFC822 failed: %v", err)
	}

	if len(parsed.TextBody) != 1 {
		t.Fatalf("expected 1 textBody part, got %d", len(parsed.TextBody))
	}
	if len(parsed.HTMLBody) != 1 {
		t.Fatalf("expected 1 htmlBody part, got %d", len(parsed.HTMLBody))
	}

	textVal := parsed.BodyValues[*parsed.TextBody[0].PartID].Value
	htmlVal := parsed.BodyValues[*parsed.HTMLBody[0].PartID].Value

	if textVal != "Plain text fallback" {
		t.Errorf("expected textBody value 'Plain text fallback', got %q", textVal)
	}
	if htmlVal != "<p>Rich text <b>bold</b> and <i>italic</i></p>" {
		t.Errorf("expected htmlBody value '<p>Rich text <b>bold</b> and <i>italic</i></p>', got %q", htmlVal)
	}
}

func TestPlainTextEmailFormatAndParse(t *testing.T) {
	part1 := "1"
	em := &Email{
		Subject: "Plain Only Test",
		From:    []EmailAddress{{Name: "Sender", Email: "sender@example.com"}},
		To:      []EmailAddress{{Name: "Recipient", Email: "recipient@example.com"}},
		TextBody: []EmailBodyPart{{
			PartID: &part1,
			Type:   "text/plain",
		}},
		HTMLBody: []EmailBodyPart{},
		BodyValues: map[string]EmailBodyValue{
			"1": {Value: "Just plain text\nwith newlines"},
		},
	}

	raw := FormatEmailRFC822(em)
	parsed, err := ParseRFC822(raw)
	if err != nil {
		t.Fatalf("ParseRFC822 failed: %v", err)
	}

	if len(parsed.TextBody) != 1 {
		t.Fatalf("expected 1 textBody part, got %d", len(parsed.TextBody))
	}
	if len(parsed.HTMLBody) != 1 {
		t.Fatalf("expected 1 htmlBody part for plain-only email (RFC 8621 Section 4.1.4 fallback), got %d", len(parsed.HTMLBody))
	}

	textVal := parsed.BodyValues[*parsed.TextBody[0].PartID].Value
	if textVal != "Just plain text\nwith newlines" {
		t.Errorf("expected text value 'Just plain text\\nwith newlines', got %q", textVal)
	}
}

func TestHTMLOnlyEmailFormatAndParse(t *testing.T) {
	part1 := "1"
	em := &Email{
		Subject: "HTML Only Test",
		From:    []EmailAddress{{Name: "Sender", Email: "sender@example.com"}},
		To:      []EmailAddress{{Name: "Recipient", Email: "recipient@example.com"}},
		TextBody: []EmailBodyPart{},
		HTMLBody: []EmailBodyPart{{
			PartID: &part1,
			Type:   "text/html",
		}},
		BodyValues: map[string]EmailBodyValue{
			"1": {Value: "<h1>Hello HTML</h1>"},
		},
	}

	raw := FormatEmailRFC822(em)
	parsed, err := ParseRFC822(raw)
	if err != nil {
		t.Fatalf("ParseRFC822 failed: %v", err)
	}

	if len(parsed.HTMLBody) != 1 {
		t.Fatalf("expected 1 htmlBody part, got %d", len(parsed.HTMLBody))
	}
	if len(parsed.TextBody) != 1 {
		t.Fatalf("expected 1 textBody part for HTML-only email (RFC 8621 Section 4.1.4 fallback), got %d", len(parsed.TextBody))
	}

	htmlVal := parsed.BodyValues[*parsed.HTMLBody[0].PartID].Value
	if htmlVal != "<h1>Hello HTML</h1>" {
		t.Errorf("expected html value '<h1>Hello HTML</h1>', got %q", htmlVal)
	}
	if parsed.Preview != "Hello HTML" {
		t.Errorf("expected stripped preview 'Hello HTML', got %q", parsed.Preview)
	}
}

func TestInlineMediaAndAttachmentDistinction(t *testing.T) {
	part1 := "1"
	part2 := "2"
	part3 := "3"
	dispAtt := "attachment"

	root := &EmailBodyPart{
		Type: "multipart/mixed",
		SubParts: []EmailBodyPart{
			{
				PartID: &part1,
				Type:   "text/plain",
			},
			{
				PartID: &part2,
				Type:   "image/png", // inline media
			},
			{
				PartID:      &part3,
				Type:        "application/pdf",
				Disposition: &dispAtt, // attachment
			},
		},
	}

	textParts, htmlParts, attParts := extractBodyStructureParts(root)

	// Both textBody and htmlBody include inline media (image/png) and text part
	if len(textParts) != 2 {
		t.Errorf("expected 2 text parts (text/plain + image/png), got %d", len(textParts))
	}
	if len(htmlParts) != 2 {
		t.Errorf("expected 2 html parts (text/plain + image/png), got %d", len(htmlParts))
	}

	// Attachments MUST include application/pdf
	if len(attParts) != 1 || *attParts[0].PartID != "3" {
		t.Errorf("expected 1 attachment part (partId 3), got %v", attParts)
	}
}
