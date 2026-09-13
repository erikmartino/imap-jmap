package jmapauth_test

import (
	"encoding/base64"
	"testing"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/spectest"
)

func TestAuthPrimitives(t *testing.T) {
	spectest.RequireID(t, "RFC8620#8.2-p1-MUST", "Authentication header parsing")

	// 1. Standard padded Base64
	stdPadded := base64.StdEncoding.EncodeToString([]byte("user:pass123"))
	if b, err := jmapauth.DecodeBase64OrRaw(stdPadded); err != nil || string(b) != "user:pass123" {
		t.Fatalf("DecodeBase64OrRaw failed on std padded base64: %s, err=%v", string(b), err)
	}

	// 2. Standard unpadded (RawStd) Base64
	rawStd := base64.RawStdEncoding.EncodeToString([]byte("user:pass12"))
	if b, err := jmapauth.DecodeBase64OrRaw(rawStd); err != nil || string(b) != "user:pass12" {
		t.Fatalf("DecodeBase64OrRaw failed on raw std base64: %s, err=%v", string(b), err)
	}

	// 3. URL-safe Base64 with URL character '_' (translates to / in Std)
	urlPadded := base64.URLEncoding.EncodeToString([]byte{0xfb, 0xff, 0xfe})
	if b, err := jmapauth.DecodeBase64OrRaw(urlPadded); err != nil || len(b) != 3 {
		t.Fatalf("DecodeBase64OrRaw failed on URL base64: %v, err=%v", b, err)
	}

	// 4. RawURL Base64 unpadded
	rawURL := base64.RawURLEncoding.EncodeToString([]byte{0xfb, 0xff, 0xfe})
	if b, err := jmapauth.DecodeBase64OrRaw(rawURL); err != nil || len(b) != 3 {
		t.Fatalf("DecodeBase64OrRaw failed on RawURL base64: %v, err=%v", b, err)
	}

	// 5. Basic prefix stripping
	if b, err := jmapauth.DecodeBase64OrRaw("Basic " + stdPadded); err != nil || string(b) != "user:pass123" {
		t.Fatalf("DecodeBase64OrRaw failed with Basic prefix: %s, err=%v", string(b), err)
	}

	// 6. Invalid base64 error branch
	if _, err := jmapauth.DecodeBase64OrRaw("!!!not-base64!!!"); err == nil {
		t.Fatalf("expected error for invalid base64 input")
	}
}
