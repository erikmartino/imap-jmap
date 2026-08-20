package jmap

import (
	"os"
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
