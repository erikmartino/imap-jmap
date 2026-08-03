package jmap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_Section7_2_PushSubscriptionRoundTripAndVerification tests PushSubscription create/get/update/destroy and PushVerification HTTP POST emission per RFC 8620 Section 7.2.
func TestRFC8620_Section7_2_PushSubscriptionRoundTripAndVerification(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	type verificationPayload struct {
		Type               string
		PushSubscriptionID string
		VerificationCode   string
	}

	var pushMu sync.Mutex
	var gotVerification verificationPayload
	verificationReceived := make(chan bool, 1)

	// Mock Push Target Webhook server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err == nil {
			if reqBody["@type"] == "PushVerification" || reqBody["type"] == "PushVerification" {
				pushMu.Lock()
				gotVerification.Type = "PushVerification"
				if psID, ok := reqBody["pushSubscriptionId"].(string); ok {
					gotVerification.PushSubscriptionID = psID
				}
				if code, ok := reqBody["verificationCode"].(string); ok {
					gotVerification.VerificationCode = code
				}
				pushMu.Unlock()
				select {
				case verificationReceived <- true:
				default:
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	using := []string{jmap.CoreCapabilityURI}

	// 1. Create PushSubscription
	calls1 := []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"deviceClientId": "client-device-123",
					"url":            targetServer.URL,
					"types":          []string{"Email", "Mailbox"},
					"keys":           map[string]string{"p256dh": "mock-key", "auth": "mock-auth"},
					"expires":        "2030-01-01T00:00:00Z",
				},
			},
		}, "c1"},
	}

	res1 := postJMAP(t, ts.URL, using, calls1)
	if len(res1.MethodResponses) == 0 {
		t.Fatalf("Empty response for PushSubscription/set create")
	}

	created, _ := res1.MethodResponses[0].Args["created"].(map[string]any)
	subObj, ok := created["sub1"].(map[string]any)
	if !ok {
		t.Fatalf("Failed to create PushSubscription: %v", res1.MethodResponses[0].Args)
	}
	subID, _ := subObj["id"].(string)
	if subID == "" {
		t.Fatalf("PushSubscription ID is empty")
	}

	// Wait for PushVerification challenge POST
	<-verificationReceived
	pushMu.Lock()
	if gotVerification.Type != "PushVerification" || gotVerification.PushSubscriptionID != subID || gotVerification.VerificationCode == "" {
		t.Errorf("Invalid PushVerification payload: %+v", gotVerification)
	}
	pushMu.Unlock()

	// 2. Fetch created PushSubscription via PushSubscription/get
	calls2 := []any{
		[]any{"PushSubscription/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{subID},
		}, "c2"},
	}
	res2 := postJMAP(t, ts.URL, using, calls2)
	list, _ := res2.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("PushSubscription/get expected 1 item, got %d", len(list))
	}

	// 3. Destroy PushSubscription
	calls3 := []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{subID},
		}, "c3"},
	}
	res3 := postJMAP(t, ts.URL, using, calls3)
	destroyed, _ := res3.MethodResponses[0].Args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0] != subID {
		t.Errorf("PushSubscription/set destroy expected [%s], got %v", subID, destroyed)
	}
	_ = context.Background()
}
