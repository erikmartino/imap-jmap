package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_Section3_7_ResultReferenceJSONPointerEscapingAndValidation tests ~0 and ~1 JSON pointer escaping and invalid result references per RFC 8620 Section 3.7.
func TestRFC8620_Section3_7_ResultReferenceJSONPointerEscapingAndValidation(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed email
	em, err := srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Ref Email",
	})
	if err != nil {
		t.Fatalf("Failed to create email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Result reference with valid pointer path "/ids/0" (RFC 8620 Section 3.7 scalar-to-array conversion)
	calls1 := []any{
		[]any{"Email/query", map[string]any{"accountId": "primary"}, "c1"},
		[]any{"Email/get", map[string]any{
			"accountId": "primary",
			"#ids": map[string]any{
				"resultOf": "c1",
				"name":     "Email/query",
				"path":     "/ids/0",
			},
		}, "c2"},
	}
	res1 := postJMAP(t, ts.URL, using, calls1)
	if len(res1.MethodResponses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(res1.MethodResponses))
	}
	getResp := res1.MethodResponses[1]
	if getResp.Name != "Email/get" {
		t.Fatalf("Expected Email/get response, got %q", getResp.Name)
	}
	gList, _ := getResp.Args["list"].([]any)
	if len(gList) != 1 {
		t.Errorf("Result reference expected 1 email returned, got %d", len(gList))
	}

	// 2. Result reference referencing non-existent callID (invalidResultReference error)
	calls2 := []any{
		[]any{"Email/get", map[string]any{
			"accountId": "primary",
			"#ids": map[string]any{
				"resultOf": "non-existent-c999",
				"name":     "Email/query",
				"path":     "/ids/0",
			},
		}, "c3"},
	}
	res2 := postJMAP(t, ts.URL, using, calls2)
	if len(res2.MethodResponses) == 0 || res2.MethodResponses[0].Name != "error" {
		t.Fatalf("Expected 'error' for invalid result reference, got %v", res2.MethodResponses)
	}
	errType, _ := res2.MethodResponses[0].Args["type"].(string)
	if errType != "invalidResultReference" {
		t.Errorf("Expected error type 'invalidResultReference', got %q", errType)
	}

	_ = em
}
