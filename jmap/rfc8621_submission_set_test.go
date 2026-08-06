package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
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

// TestEmailSubmission_LocalDelivery tests that submissions to local domain addresses deliver the message in-process to the recipient account.
func TestEmailSubmission_LocalDelivery(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	senderCtx := context.Background()
	em, err := srv.MailBackend.CreateEmail(senderCtx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Local Delivery Test",
		To:         []jmap.EmailAddress{{Email: "localuser@example.com"}},
	})
	if err != nil {
		t.Fatalf("Failed to create email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"emailId":    string(em.ID),
					"identityId": "id-primary",
				},
			},
		}, "c1"},
	})

	created, _ := res.MethodResponses[0].Args["created"].(map[string]any)
	subMap, ok := created["sub1"].(map[string]any)
	if !ok {
		t.Fatalf("Failed to create submission: %v", res.MethodResponses[0].Args)
	}

	ds, _ := subMap["deliveryStatus"].(map[string]any)
	status, _ := ds["localuser@example.com"].(map[string]any)
	if status["delivered"] != "yes" {
		t.Errorf("Expected deliveryStatus delivered='yes', got %v", status)
	}

	// Verify recipient's account received the email
	rcptAccountID := jmap.AccountIDForSubject("localuser@example.com")
	rcptCtx := jmap.ContextWithAccountID(context.Background(), rcptAccountID)
	rcptEmails, err := srv.MailBackend.GetAllEmails(rcptCtx)
	if err != nil || len(rcptEmails) == 0 {
		t.Fatalf("Recipient account %q did not receive the email: err=%v, count=%d", rcptAccountID, err, len(rcptEmails))
	}
	var found *jmap.Email
	for _, em := range rcptEmails {
		if em.Subject == "Local Delivery Test" {
			found = em
			break
		}
	}
	if found == nil {
		t.Errorf("Expected recipient account to receive email with subject 'Local Delivery Test'")
	}
}

// TestEmailSubmission_ExternalAllowList tests the external recipient allow-list gate.
func TestEmailSubmission_ExternalAllowList(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()
	srv := jmap.NewServer(
		nil,
		jmap.WithMailBackend(memBackend),
		jmap.WithBlobBackend(memBlobBackend),
		jmap.WithAllowedRecipients([]string{"allowed@external.com"}),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	em, _ := srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "External Test",
		To:         []jmap.EmailAddress{{Email: "allowed@external.com"}},
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}

	// 1. Submit to allowed external recipient -> OK
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"emailId":    string(em.ID),
					"identityId": "id-primary",
				},
			},
		}, "c1"},
	})
	created1, _ := res1.MethodResponses[0].Args["created"].(map[string]any)
	if created1["sub1"] == nil {
		t.Errorf("Allowed external submission failed: %v", res1.MethodResponses[0].Args)
	}

	// 2. Submit to unlisted external recipient -> forbidden SetError
	em2, _ := srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Unlisted Test",
		To:         []jmap.EmailAddress{{Email: "unlisted@external.com"}},
	})
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub2": map[string]any{
					"emailId":    string(em2.ID),
					"identityId": "id-primary",
				},
			},
		}, "c2"},
	})
	notCreated2, _ := res2.MethodResponses[0].Args["notCreated"].(map[string]any)
	errObj2, ok := notCreated2["sub2"].(map[string]any)
	if !ok {
		t.Fatalf("Expected sub2 in notCreated, got %v", res2.MethodResponses[0].Args)
	}
	if errObj2["type"] != "forbidden" {
		t.Errorf("Expected SetError type 'forbidden', got %q", errObj2["type"])
	}
}
