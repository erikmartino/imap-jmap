package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestEmailSubmissionSetDestroyTests tests EmailSubmission/set create, destroy, and error paths per RFC 8621 Section 7.3.
func TestEmailSubmissionSetDestroyTests(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Seed an email
	em, err := srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Submission Destroy Test",
	})
	if err != nil {
		t.Fatalf("Failed to create email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}

	// 2. Create an EmailSubmission
	calls1 := []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"emailId":    string(em.ID),
					"identityId": "id-primary",
				},
			},
		}, "c1"},
	}
	res1 := postJMAP(t, ts.URL, using, calls1)
	var subID string
	if len(res1.MethodResponses) > 0 {
		args := res1.MethodResponses[0].Args
		if created, ok := args["created"].(map[string]any); ok {
			if subMap, ok := created["sub1"].(map[string]any); ok {
				subID, _ = subMap["id"].(string)
			}
		}
	}
	if subID == "" {
		t.Fatalf("Failed to create EmailSubmission")
	}

	// 3. Destroy the submission & attempt to update (must produce notUpdated invalidProperties and destroyed)
	calls2 := []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				subID: map[string]any{"sendAt": "2030-01-01T00:00:00Z"},
			},
			"destroy": []any{subID, "non-existent-sub"},
		}, "c2"},
	}
	res2 := postJMAP(t, ts.URL, using, calls2)
	if len(res2.MethodResponses) == 0 {
		t.Fatalf("Empty response for EmailSubmission/set destroy")
	}
	args2 := res2.MethodResponses[0].Args
	destroyed, _ := args2["destroyed"].([]any)
	notDestroyed, _ := args2["notDestroyed"].(map[string]any)
	notUpdated, _ := args2["notUpdated"].(map[string]any)

	if len(destroyed) != 1 || destroyed[0] != subID {
		t.Errorf("Expected destroyed=[%q], got %v", subID, destroyed)
	}
	if notDestroyed["non-existent-sub"] == nil {
		t.Errorf("Expected notDestroyed for 'non-existent-sub', got %v", notDestroyed)
	}
	if notUpdated[subID] == nil {
		t.Errorf("Expected notUpdated for %q (submissions immutable), got %v", subID, notUpdated)
	}
}

// TestEmailSubmission_Envelope_RoundTrip tests creating and retrieving an EmailSubmission with an explicit envelope per RFC 8621 Section 7.1.
func TestEmailSubmission_Envelope_RoundTrip(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	em, err := srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Envelope Test",
		To:         []jmap.EmailAddress{{Email: "to@example.com"}},
	})
	if err != nil {
		t.Fatalf("Failed to create email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}

	calls1 := []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"emailId":    string(em.ID),
					"identityId": "id-primary",
					"envelope": map[string]any{
						"mailFrom": map[string]any{"email": "sender@example.com"},
						"rcptTo":   []any{map[string]any{"email": "rcpt1@example.com"}},
					},
				},
			},
		}, "c1"},
	}
	res1 := postJMAP(t, ts.URL, using, calls1)
	var subID string
	if len(res1.MethodResponses) > 0 {
		args := res1.MethodResponses[0].Args
		if created, ok := args["created"].(map[string]any); ok {
			if subMap, ok := created["sub1"].(map[string]any); ok {
				subID, _ = subMap["id"].(string)
			}
		}
	}
	if subID == "" {
		t.Fatalf("Failed to create EmailSubmission with envelope: %v", res1.MethodResponses[0].Args)
	}

	calls2 := []any{
		[]any{"EmailSubmission/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{subID},
		}, "c2"},
	}
	res2 := postJMAP(t, ts.URL, using, calls2)
	list, _ := res2.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("Expected 1 submission, got %d", len(list))
	}
	subObj := list[0].(map[string]any)
	envObj, ok := subObj["envelope"].(map[string]any)
	if !ok {
		t.Fatalf("Expected envelope in EmailSubmission, got %v", subObj)
	}
	mailFrom, _ := envObj["mailFrom"].(map[string]any)
	if mailFrom["email"] != "sender@example.com" {
		t.Errorf("Expected mailFrom sender@example.com, got %v", mailFrom)
	}
}

