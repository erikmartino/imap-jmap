package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC9219_VerifySmimePayloadStructure tests Email/verifySmime returning complete RFC 9219 verification result payload fields.
func TestRFC9219_VerifySmimePayloadStructure(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := context.Background()

	em, err := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Signed Message Test",
	})
	if err != nil {
		t.Fatalf("Failed to seed email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SmimeCapabilityURI}

	res := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/verifySmime", map[string]any{
			"accountId": "primary",
			"emailIds":  []any{string(em.ID)},
		}, "c1"},
	})

	if len(res.MethodResponses) == 0 || res.MethodResponses[0].Name != "Email/verifySmime" {
		t.Fatalf("Expected Email/verifySmime method response, got %v", res.MethodResponses)
	}

	args := res.MethodResponses[0].Args
	verified, _ := args["verified"].(map[string]any)
	vObj, ok := verified[string(em.ID)].(map[string]any)
	if !ok {
		t.Fatalf("Expected verified entry for %s, got %v", em.ID, args)
	}

	// Verify required RFC 9219 result structure fields
	if status, ok := vObj["smimeStatus"].(string); !ok || status == "" {
		t.Errorf("Expected non-empty 'smimeStatus' in Email/verifySmime response, got %v", vObj["smimeStatus"])
	}
	if statusAt, ok := vObj["smimeStatusAt"].(string); !ok || statusAt == "" {
		t.Errorf("Expected non-empty 'smimeStatusAt' in Email/verifySmime response, got %v", vObj["smimeStatusAt"])
	}
}
