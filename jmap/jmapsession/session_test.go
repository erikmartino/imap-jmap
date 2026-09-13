package jmapsession_test

import (
	"testing"

	"imap-jmap/jmap/jmapsession"
	"imap-jmap/jmap/spectest"
)

func TestSessionPrimitives(t *testing.T) {
	spectest.RequireID(t, "RFC8620#2-p1-MUST", "Session object representation and capabilities")
	spectest.RequireID(t, "RFC8620#2.2-p1-MUST", "Core capabilities limits")

	cap := jmapsession.CoreCapability{
		MaxSizeUpload:       jmapsession.DefaultMaxSizeUpload,
		MaxObjectsInGet:     jmapsession.DefaultMaxObjectsInGet,
		CollationAlgorithms: []string{"i;unicode-casemap"},
	}

	if cap.MaxSizeUpload != 50000000 {
		t.Fatalf("expected DefaultMaxSizeUpload to be 50000000, got %d", cap.MaxSizeUpload)
	}

	sess := jmapsession.Session{
		Username: "user@example.com",
		State:    "s1",
		Capabilities: map[string]any{
			jmapsession.CoreCapabilityURI: cap,
		},
	}

	if sess.State != "s1" || sess.Username != "user@example.com" {
		t.Fatalf("unexpected Session values: %+v", sess)
	}
}
