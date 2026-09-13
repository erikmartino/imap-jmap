package jmapauth_test

import (
	"testing"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/spectest"
)

func TestAuthPrimitives(t *testing.T) {
	spectest.RequireID(t, "RFC8620#8.2-p1-MUST", "Authentication header parsing")

	b, err := jmapauth.DecodeBase64OrRaw("dXNlcjpwYXNz")
	if err != nil || string(b) != "user:pass" {
		t.Fatalf("expected user:pass, got %s, err=%v", string(b), err)
	}

	bBasic, err := jmapauth.DecodeBase64OrRaw("Basic dXNlcjpwYXNz")
	if err != nil || string(bBasic) != "user:pass" {
		t.Fatalf("expected user:pass with Basic prefix, got %s, err=%v", string(bBasic), err)
	}
}
