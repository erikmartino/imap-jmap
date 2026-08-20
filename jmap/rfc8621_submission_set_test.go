package jmap_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestEmailSubmissionSetDestroyTests tests EmailSubmission/set create, destroy, and error paths per RFC 8621 Section 7.3.
func TestEmailSubmissionSetDestroyTests(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Seed an email
	em, err := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Submission Destroy Test",
		To:         []jmap.EmailAddress{{Email: "destroy-test@example.com"}},
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

	em, err := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
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

	senderCtx := seedCtx()
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

	em, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
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
	em2, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
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

// TestEmailSubmission_OnSuccessUpdateEmail tests the RFC 8621 Section 7.5 top-level
// onSuccessUpdateEmail argument: a patch applied to the Email referenced by the
// EmailSubmission once the submission succeeds, with the implicit Email/set response
// emitted after the EmailSubmission/set response (same client call id).
func TestEmailSubmission_OnSuccessUpdateEmail(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed a draft email in the drafts mailbox with the $draft keyword.
	em, err := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-drafts": true},
		Keywords:   map[string]bool{"$draft": true},
		Subject:    "OnSuccessUpdateEmail Test",
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
			"onSuccessUpdateEmail": map[string]any{
				"#sub1": map[string]any{
					"mailboxIds/mb-sent":   true,
					"mailboxIds/mb-drafts": false,
					"keywords/$draft":      nil,
				},
			},
		}, "c1"},
	})

	if len(res.MethodResponses) != 2 {
		t.Fatalf("Expected 2 method responses (EmailSubmission/set + implicit Email/set), got %d: %v",
			len(res.MethodResponses), res.MethodResponses)
	}
	first, second := res.MethodResponses[0], res.MethodResponses[1]

	if first.Name != "EmailSubmission/set" {
		t.Errorf("First response must be EmailSubmission/set, got %q", first.Name)
	}
	created, _ := first.Args["created"].(map[string]any)
	if created["sub1"] == nil {
		t.Fatalf("Submission creation failed: %v", first.Args)
	}

	// The implicit Email/set MUST follow with the same client call id.
	if second.Name != "Email/set" {
		t.Fatalf("Second response must be the implicit Email/set, got %q", second.Name)
	}
	if second.ClientCallID != "c1" {
		t.Errorf("Implicit Email/set must reuse client call id, got %q", second.ClientCallID)
	}
	updated, _ := second.Args["updated"].(map[string]any)
	if _, ok := updated[string(em.ID)]; !ok {
		t.Errorf("Implicit Email/set must list the updated email, got %v", second.Args)
	}

	// Verify the patch was applied: moved to Sent, out of Drafts, $draft removed.
	emails, _, err := srv.MailBackend.GetEmails(seedCtx(), []jmap.Id{em.ID})
	if err != nil || len(emails) == 0 {
		t.Fatalf("Failed to re-fetch email: %v", err)
	}
	got := emails[0]
	if !got.MailboxIDs["mb-sent"] {
		t.Errorf("Expected email in Sent mailbox after onSuccessUpdateEmail, got %v", got.MailboxIDs)
	}
	if got.MailboxIDs["mb-drafts"] {
		t.Errorf("Expected email removed from Drafts mailbox after onSuccessUpdateEmail, got %v", got.MailboxIDs)
	}
	if got.Keywords["$draft"] {
		t.Errorf("Expected $draft keyword removed after onSuccessUpdateEmail, got %v", got.Keywords)
	}
}

// TestEmailSubmission_OnSuccessDestroyEmail tests the RFC 8621 Section 7.5 top-level
// onSuccessDestroyEmail argument: the Email referenced by a successfully created
// EmailSubmission is destroyed, reported via the implicit Email/set response.
func TestEmailSubmission_OnSuccessDestroyEmail(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	em, err := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "OnSuccessDestroyEmail Test",
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
			"onSuccessDestroyEmail": []any{"#sub1"},
		}, "c1"},
	})

	if len(res.MethodResponses) != 2 {
		t.Fatalf("Expected 2 method responses, got %d: %v", len(res.MethodResponses), res.MethodResponses)
	}
	second := res.MethodResponses[1]
	if second.Name != "Email/set" {
		t.Fatalf("Second response must be the implicit Email/set, got %q", second.Name)
	}
	destroyed, _ := second.Args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0] != string(em.ID) {
		t.Errorf("Implicit Email/set must list destroyed email, got %v", second.Args)
	}

	emails, _, _ := srv.MailBackend.GetEmails(seedCtx(), []jmap.Id{em.ID})
	if len(emails) != 0 {
		t.Errorf("Expected email destroyed after onSuccessDestroyEmail, still present: %v", emails)
	}
}

// TestEmailSubmission_OnSuccessUpdateEmailIgnoredForFailedCreation tests that the
// onSuccessUpdateEmail patch is NOT applied when the submission itself failed
// (RFC 8621 Section 7.5: applied "if the create succeeds").
func TestEmailSubmission_OnSuccessUpdateEmailIgnoredForFailedCreation(t *testing.T) {
	// Submit to a recipient that is not local and not in the allow-list, so the submission
	// is rejected with a forbidden SetError.
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

	em2, err := memBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-drafts": true},
		Keywords:   map[string]bool{"$draft": true},
		Subject:    "Failed Submission Test",
		To:         []jmap.EmailAddress{{Email: "blocked@external.test"}},
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
					"emailId":    string(em2.ID),
					"identityId": "id-primary",
				},
			},
			"onSuccessUpdateEmail": map[string]any{
				"#sub1": map[string]any{
					"mailboxIds/mb-sent": true,
				},
			},
		}, "c1"},
	})

	if len(res.MethodResponses) != 1 {
		t.Fatalf("Expected only EmailSubmission/set response (no implicit Email/set for a failed create), got %d: %v",
			len(res.MethodResponses), res.MethodResponses)
	}
	notCreated, _ := res.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated["sub1"] == nil {
		t.Fatalf("Expected submission in notCreated, got %v", res.MethodResponses[0].Args)
	}

	emails, _, _ := memBackend.GetEmails(seedCtx(), []jmap.Id{em2.ID})
	if len(emails) == 0 {
		t.Fatalf("Email must survive a failed submission")
	}
	if emails[0].MailboxIDs["mb-sent"] {
		t.Errorf("onSuccessUpdateEmail must NOT be applied when the submission failed, got %v", emails[0].MailboxIDs)
	}
}

// TestEmailSubmission_UndoStatusCancelPending verifies that a pending submission (sendAt in future)
// can be canceled by patching undoStatus to "canceled" per RFC 8621 Section 7.5.
func TestEmailSubmission_UndoStatusCancelPending(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	em, err := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Pending Cancel Test",
		To:         []jmap.EmailAddress{{Email: "localuser@example.com"}},
	})
	if err != nil {
		t.Fatalf("Failed to create email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}

	// 1. Create with future sendAt
	futureDate := "2035-01-01T00:00:00Z"
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"emailId":    string(em.ID),
					"identityId": "id-primary",
					"sendAt":     futureDate,
				},
			},
		}, "c1"},
	})
	created, _ := res1.MethodResponses[0].Args["created"].(map[string]any)
	subObj, ok := created["sub1"].(map[string]any)
	if !ok {
		t.Fatalf("Failed to create pending submission: %v", res1.MethodResponses[0].Args)
	}
	subID, _ := subObj["id"].(string)
	if subObj["undoStatus"] != "pending" {
		t.Errorf("Expected undoStatus='pending' for future sendAt, got %v", subObj["undoStatus"])
	}

	// 2. Patch undoStatus to "canceled"
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				subID: map[string]any{
					"undoStatus": "canceled",
				},
			},
		}, "c2"},
	})
	updated, _ := res2.MethodResponses[0].Args["updated"].(map[string]any)
	upObj, ok := updated[subID].(map[string]any)
	if !ok {
		t.Fatalf("Expected subID in updated, got: %v", res2.MethodResponses[0].Args)
	}
	if upObj["undoStatus"] != "canceled" {
		t.Errorf("Expected undoStatus='canceled', got %v", upObj["undoStatus"])
	}

	// 3. Patching already canceled submission returns alreadyCanceled
	res3 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				subID: map[string]any{
					"undoStatus": "canceled",
				},
			},
		}, "c3"},
	})
	notUpdated, _ := res3.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errObj, ok := notUpdated[subID].(map[string]any)
	if !ok || errObj["type"] != "alreadyCanceled" {
		t.Errorf("Expected notUpdated type 'alreadyCanceled', got %v", notUpdated)
	}
}

// TestEmailSubmission_UndoStatusCancelFinalCannotCancel verifies that attempting to cancel
// a finalized submission returns a cannotCancel SetError per RFC 8621 Section 7.5.
func TestEmailSubmission_UndoStatusCancelFinalCannotCancel(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	em, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Final Cancel Test",
		To:         []jmap.EmailAddress{{Email: "localuser@example.com"}},
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}

	// Create immediate submission (undoStatus: final)
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
	created, _ := res1.MethodResponses[0].Args["created"].(map[string]any)
	subObj := created["sub1"].(map[string]any)
	subID := subObj["id"].(string)
	if subObj["undoStatus"] != "final" {
		t.Errorf("Expected undoStatus='final', got %v", subObj["undoStatus"])
	}

	// Attempt to cancel
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				subID: map[string]any{
					"undoStatus": "canceled",
				},
			},
		}, "c2"},
	})
	notUpdated, _ := res2.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errObj, ok := notUpdated[subID].(map[string]any)
	if !ok || errObj["type"] != "cannotCancel" {
		t.Errorf("Expected notUpdated type 'cannotCancel', got %v", notUpdated)
	}
}

// TestEmailSubmission_ImmutablePropertiesNotUpdated verifies that properties other than
// undoStatus cannot be updated on an EmailSubmission (RFC 8621 Section 7.5).
func TestEmailSubmission_ImmutablePropertiesNotUpdated(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	em, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Immutable Properties Test",
		To:         []jmap.EmailAddress{{Email: "localuser@example.com"}},
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}
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
	created, _ := res1.MethodResponses[0].Args["created"].(map[string]any)
	subID := created["sub1"].(map[string]any)["id"].(string)

	// Attempt to patch immutable properties (emailId, sendAt)
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				subID: map[string]any{
					"emailId": "email-2",
					"sendAt":  "2030-01-01T00:00:00Z",
				},
			},
		}, "c2"},
	})
	notUpdated, _ := res2.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errObj, ok := notUpdated[subID].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("Expected notUpdated type 'invalidProperties' for modifying immutable submission fields, got %v", notUpdated)
	}
}

// TestEmailSubmission_IfInStateMismatch verifies stateMismatch error when ifInState is invalid.
func TestEmailSubmission_IfInStateMismatch(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"ifInState": "bad-state-token",
			"create": map[string]any{
				"sub1": map[string]any{
					"emailId":    "email-1",
					"identityId": "id-primary",
				},
			},
		}, "c1"},
	})
	if res.MethodResponses[0].Name != "error" {
		t.Fatalf("Expected method error, got %q", res.MethodResponses[0].Name)
	}
	errType, _ := res.MethodResponses[0].Args["type"].(string)
	if errType != "stateMismatch" {
		t.Errorf("Expected error type 'stateMismatch', got %q", errType)
	}
}

// TestEmailSubmission_ValidationErrors verifies input validation on EmailSubmission/set create.
func TestEmailSubmission_ValidationErrors(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	em, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Validation Test",
		To:         []jmap.EmailAddress{{Email: "localuser@example.com"}},
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}

	// 1. Missing identityId
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"emailId": string(em.ID),
				},
			},
		}, "c1"},
	})
	notCreated1, _ := res1.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated1["sub1"] == nil {
		t.Errorf("Expected notCreated for missing identityId")
	}

	// 2. Non-existent identityId
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub2": map[string]any{
					"identityId": "non-existent-identity",
					"emailId":    string(em.ID),
				},
			},
		}, "c2"},
	})
	notCreated2, _ := res2.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated2["sub2"] == nil {
		t.Errorf("Expected notCreated for non-existent identityId")
	}

	// 3. Non-existent emailId
	res3 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub3": map[string]any{
					"identityId": "id-primary",
					"emailId":    "non-existent-email",
				},
			},
		}, "c3"},
	})
	notCreated3, _ := res3.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated3["sub3"] == nil {
		t.Errorf("Expected notCreated for non-existent emailId")
	}

	// 4. Invalid sendAt format
	res4 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub4": map[string]any{
					"identityId": "id-primary",
					"emailId":    string(em.ID),
					"sendAt":     "not-a-date",
				},
			},
		}, "c4"},
	})
	notCreated4, _ := res4.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated4["sub4"] == nil {
		t.Errorf("Expected notCreated for invalid sendAt date")
	}
}

// TestEmailSubmission_CreationReferences verifies creation references (#creationId)
// linking an Email created in the same request to an EmailSubmission.
func TestEmailSubmission_CreationReferences(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"draft1": map[string]any{
					"mailboxIds": map[string]any{"mb-drafts": true},
					"subject":    "Composite Creation Draft",
					"to":         []any{map[string]any{"email": "localuser@example.com"}},
				},
			},
		}, "c1"},
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"identityId": "id-primary",
					"emailId":    "#draft1",
				},
			},
		}, "c2"},
	})

	if len(res.MethodResponses) != 2 {
		t.Fatalf("Expected 2 method responses, got %d", len(res.MethodResponses))
	}
	emailCreated, _ := res.MethodResponses[0].Args["created"].(map[string]any)
	draftObj := emailCreated["draft1"].(map[string]any)
	realEmailID := draftObj["id"].(string)

	subCreated, _ := res.MethodResponses[1].Args["created"].(map[string]any)
	subObj, ok := subCreated["sub1"].(map[string]any)
	if !ok {
		t.Fatalf("Failed to create submission with creation ref: %v", res.MethodResponses[1].Args)
	}
	if subObj["emailId"] != realEmailID {
		t.Errorf("Expected submission emailId=%q, got %q", realEmailID, subObj["emailId"])
	}
}

type mockOutboundSender struct {
	called     bool
	from       string
	recipients []string
	rawBytes   []byte
}

func (m *mockOutboundSender) SendMail(ctx context.Context, from string, recipients []string, rawMessage []byte) map[string]jmap.OutboundDeliveryResult {
	m.called = true
	m.from = from
	m.recipients = recipients
	m.rawBytes = rawMessage
	results := make(map[string]jmap.OutboundDeliveryResult)
	for _, rcpt := range recipients {
		results[rcpt] = jmap.OutboundDeliveryResult{
			Delivered: true,
			SmtpReply: "250 2.0.0 OK delivered via mock relay",
		}
	}
	return results
}

// TestEmailSubmission_OutboundSenderRelay verifies that when OutboundMailSender is configured,
// external allow-listed submissions are routed through it with full MIME payload.
func TestEmailSubmission_OutboundSenderRelay(t *testing.T) {
	mockSender := &mockOutboundSender{}
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()
	srv := jmap.NewServer(
		nil,
		jmap.WithMailBackend(memBackend),
		jmap.WithBlobBackend(memBlobBackend),
		jmap.WithAllowedRecipients([]string{"external@allowed.org"}),
		jmap.WithOutboundSender(mockSender),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	em, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-drafts": true},
		Subject:    "Outbound Relay Test",
		From:       []jmap.EmailAddress{{Email: "sender@example.com"}},
		To:         []jmap.EmailAddress{{Email: "external@allowed.org"}},
		BodyValues: map[string]jmap.EmailBodyValue{"1": {Value: "Hello external recipient"}},
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"identityId": "id-primary",
					"emailId":    string(em.ID),
				},
			},
		}, "c1"},
	})

	created, _ := res.MethodResponses[0].Args["created"].(map[string]any)
	subObj, ok := created["sub1"].(map[string]any)
	if !ok {
		t.Fatalf("Submission create failed: %v", res.MethodResponses[0].Args)
	}

	ds, _ := subObj["deliveryStatus"].(map[string]any)
	status, _ := ds["external@allowed.org"].(map[string]any)
	if status["delivered"] != "yes" {
		t.Errorf("Expected delivered=yes via outbound relay, got %v", status)
	}
	if !mockSender.called {
		t.Errorf("Expected mock OutboundMailSender to be called")
	}
	if len(mockSender.recipients) != 1 || mockSender.recipients[0] != "external@allowed.org" {
		t.Errorf("Expected recipients=[external@allowed.org], got %v", mockSender.recipients)
	}
	if len(mockSender.rawBytes) == 0 {
		t.Errorf("Expected non-empty raw MIME message payload passed to OutboundMailSender")
	}
}

// TestEmailSubmission_PushStateChange verifies that creating an EmailSubmission emits
// a StateChange SSE event containing EmailSubmission for the account (RFC 8620 Section 7.1).
func TestEmailSubmission_PushStateChange(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()
	srv := jmap.NewServer(
		nil,
		jmap.WithMailBackend(memBackend),
		jmap.WithBlobBackend(memBlobBackend),
	)
	memBackend.SetBroadcaster(srv.Broadcaster)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	em, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-drafts": true},
		Subject:    "SSE StateChange Test",
		To:         []jmap.EmailAddress{{Email: "localuser@example.com"}},
	})

	sseURL := ts.URL + "/eventsource?types=EmailSubmission&closeafter=state"
	req := authedRequest(t, "GET", sseURL, nil)

	sseResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /eventsource failed: %v", err)
	}
	defer sseResp.Body.Close()

	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("Expected SSE HTTP status 200, got %d", sseResp.StatusCode)
	}

	// In background, create submission via authedPost
	go func() {
		time.Sleep(100 * time.Millisecond)
		using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}
		payload := map[string]any{
			"using": using,
			"methodCalls": []any{
				[]any{"EmailSubmission/set", map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"sub1": map[string]any{
							"identityId": "id-primary",
							"emailId":    string(em.ID),
						},
					},
				}, "c1"},
			},
		}
		body, _ := json.Marshal(payload)
		resp, postErr := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if postErr == nil {
			_ = resp.Body.Close()
		}
	}()

	scanner := bufio.NewScanner(sseResp.Body)
	var eventLine, dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventLine = line
		} else if strings.HasPrefix(line, "data:") {
			dataLine = line
			break
		}
	}

	if eventLine != "event: state" {
		t.Fatalf("Expected SSE event 'event: state', got %q", eventLine)
	}
	if !strings.Contains(dataLine, "EmailSubmission") {
		t.Errorf("Expected StateChange event payload containing 'EmailSubmission', got %q", dataLine)
	}
}


