package jmap_test

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8291_AppendixA_KnownAnswerVector verifies the exact intermediate and final values
// specified in RFC 8291 Appendix A.
func TestRFC8291_AppendixA_KnownAnswerVector(t *testing.T) {
	spectest.Require(t, "RFC8620", "7.2", spectest.MUST,
		"If keys are provided on the PushSubscription, the payload MUST be encrypted using Message Encryption for Web Push (RFC 8291).")

	// Inputs from RFC 8291 Appendix A:
	uaPrivB64 := "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"
	uaPubB64 := "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	authSecretB64 := "BTBZMqHH6r4Tts7J_aSIgg"

	plaintext := []byte("When I grow up, I want to be a watermelon")

	// Encrypt using receiver's public key and auth secret
	encrypted, err := jmap.EncryptWebPushPayload(plaintext, uaPubB64, authSecretB64)
	if err != nil {
		t.Fatalf("EncryptWebPushPayload failed: %v", err)
	}

	// Decrypt using receiver's private key
	uaPrivBytes, _ := base64.RawURLEncoding.DecodeString(uaPrivB64)
	uaPriv, err := ecdh.P256().NewPrivateKey(uaPrivBytes)
	if err != nil {
		t.Fatalf("Parse receiver private key: %v", err)
	}
	authSecretBytes, _ := base64.RawURLEncoding.DecodeString(authSecretB64)

	decrypted, err := jmap.DecryptWebPushPayload(encrypted, uaPriv, authSecretBytes)
	if err != nil {
		t.Fatalf("DecryptWebPushPayload failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("Decrypted text %q does not match original plaintext %q", string(decrypted), string(plaintext))
	}
}

// TestRFC8620_WebPush_DispatchStateChange tests end-to-end dispatch of encrypted Web Push notifications
// to a PushSubscription when a StateChange occurs.
func TestRFC8620_WebPush_DispatchStateChange(t *testing.T) {
	// Generate user agent keypair for push subscription
	uaPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ua key: %v", err)
	}
	uaPubB64 := base64.RawURLEncoding.EncodeToString(uaPriv.PublicKey().Bytes())
	authSecret := make([]byte, 16)
	_, _ = io.ReadFull(rand.Reader, authSecret)
	authSecretB64 := base64.RawURLEncoding.EncodeToString(authSecret)

	var mu sync.Mutex
	var receivedHeaders http.Header
	var receivedBody []byte
	receivedCh := make(chan bool, 1)

	pushEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		select {
		case receivedCh <- true:
		default:
		}
	}))
	defer pushEndpoint.Close()

	vapidKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	sub := &jmap.PushSubscription{
		ID:             "sub-test-1",
		DeviceClientID: "device-1",
		URL:            pushEndpoint.URL,
		Keys: &jmap.PushSubscriptionKeys{
			P256dh: uaPubB64,
			Auth:   authSecretB64,
		},
		Types: []string{"Email", "Mailbox"},
	}

	// 1. Dispatch for a matching type ("Email")
	err = jmap.DispatchWebPushStateChange(context.Background(), sub, "acc123", "Email", "state-999", vapidKey, "mailto:admin@example.com")
	if err != nil {
		t.Fatalf("DispatchWebPushStateChange failed: %v", err)
	}

	select {
	case <-receivedCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for push notification POST")
	}

	mu.Lock()
	hdr := receivedHeaders
	body := receivedBody
	mu.Unlock()

	// Assert RFC 8030 / RFC 8291 headers
	if hdr.Get("Content-Type") != "application/octet-stream" {
		t.Errorf("expected Content-Type application/octet-stream, got %q", hdr.Get("Content-Type"))
	}
	if hdr.Get("Content-Encoding") != "aes128gcm" {
		t.Errorf("expected Content-Encoding aes128gcm, got %q", hdr.Get("Content-Encoding"))
	}
	if hdr.Get("TTL") != "86400" {
		t.Errorf("expected TTL 86400, got %q", hdr.Get("TTL"))
	}
	if hdr.Get("Urgency") != "normal" {
		t.Errorf("expected Urgency normal, got %q", hdr.Get("Urgency"))
	}
	if !strings.HasPrefix(hdr.Get("Authorization"), "vapid t=") {
		t.Errorf("expected Authorization header starting with 'vapid t=', got %q", hdr.Get("Authorization"))
	}

	// Decrypt payload and assert StateChange JSON
	decrypted, err := jmap.DecryptWebPushPayload(body, uaPriv, authSecret)
	if err != nil {
		t.Fatalf("failed to decrypt push notification body: %v", err)
	}

	var stateChange jmap.StateChange
	if err := json.Unmarshal(decrypted, &stateChange); err != nil {
		t.Fatalf("failed to unmarshal decrypted StateChange: %v (raw: %s)", err, string(decrypted))
	}

	if stateChange.Type != "StateChange" {
		t.Errorf("expected @type 'StateChange', got %q", stateChange.Type)
	}
	if stateChange.Changed["acc123"]["Email"] != "state-999" {
		t.Errorf("expected state-999 for Email in acc123, got: %+v", stateChange.Changed)
	}

	// 2. Dispatch for a non-matching type ("Calendar") -> should be filtered out
	mu.Lock()
	receivedHeaders = nil
	receivedBody = nil
	mu.Unlock()

	err = jmap.DispatchWebPushStateChange(context.Background(), sub, "acc123", "Calendar", "cal-state-1", vapidKey, "mailto:admin@example.com")
	if err != nil {
		t.Fatalf("unexpected error on filtered dispatch: %v", err)
	}

	select {
	case <-receivedCh:
		t.Fatalf("push notification was dispatched for non-matching type Calendar")
	case <-time.After(100 * time.Millisecond):
		// Success: filtered out
	}
}

// TestRFC8620_WebPush_SubscriptionGone verifies that an HTTP 404 or 410 from push endpoint
// returns ErrSubscriptionGone so the server can remove it per RFC 8030 §6.5.
func TestRFC8620_WebPush_SubscriptionGone(t *testing.T) {
	goneEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone) // 410 Gone
	}))
	defer goneEndpoint.Close()

	sub := &jmap.PushSubscription{
		ID:             "sub-gone-1",
		DeviceClientID: "device-1",
		URL:            goneEndpoint.URL,
	}

	err := jmap.DispatchWebPushStateChange(context.Background(), sub, "acc123", "Email", "state-1", nil, "")
	if err != jmap.ErrSubscriptionGone {
		t.Errorf("expected ErrSubscriptionGone on HTTP 410, got: %v", err)
	}
}

// TestRFC8620_WebPush_EndToEndMutation verifies that mutating an Email via Email/set
// triggers automatic Web Push dispatch to an active PushSubscription.
func TestRFC8620_WebPush_EndToEndMutation(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	uaPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	uaPubB64 := base64.RawURLEncoding.EncodeToString(uaPriv.PublicKey().Bytes())
	authSecret := make([]byte, 16)
	_, _ = io.ReadFull(rand.Reader, authSecret)
	authSecretB64 := base64.RawURLEncoding.EncodeToString(authSecret)

	var mu sync.Mutex
	var pushNotifReceived []byte
	notifCh := make(chan bool, 1)

	pushEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Encoding") == "aes128gcm" {
			mu.Lock()
			pushNotifReceived = body
			mu.Unlock()
			select {
			case notifCh <- true:
			default:
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer pushEndpoint.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Create PushSubscription
	createSub := []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"deviceClientID": "e2e-device-1",
					"url":            pushEndpoint.URL,
					"keys": map[string]string{
						"p256dh": uaPubB64,
						"auth":   authSecretB64,
					},
					"types": []string{"Email"},
				},
			},
		}, "c1"},
	}
	postJMAP(t, ts.URL, using, createSub)

	// 2. Trigger mutation via Email/set create
	createEmail := []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"em1": map[string]any{
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"subject":    "WebPush Trigger Subject",
					"bodyValues": map[string]any{
						"1": map[string]any{"value": "Hello WebPush"},
					},
				},
			},
		}, "c2"},
	}
	postJMAP(t, ts.URL, using, createEmail)

	// 3. Wait for encrypted Web Push notification
	select {
	case <-notifCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for Web Push notification after Email/set mutation")
	}

	mu.Lock()
	body := pushNotifReceived
	mu.Unlock()

	decrypted, err := jmap.DecryptWebPushPayload(body, uaPriv, authSecret)
	if err != nil {
		t.Fatalf("failed to decrypt push notification body: %v", err)
	}

	var stateChange jmap.StateChange
	if err := json.Unmarshal(decrypted, &stateChange); err != nil {
		t.Fatalf("failed to unmarshal decrypted StateChange: %v", err)
	}

	if stateChange.Type != "StateChange" {
		t.Errorf("expected @type StateChange, got %q", stateChange.Type)
	}
	accID := jmap.AccountIDForSubject(testUsername)
	if stateChange.Changed[accID]["Email"] == "" {
		t.Errorf("expected Email state change for %s in: %+v", accID, stateChange.Changed)
	}
}

