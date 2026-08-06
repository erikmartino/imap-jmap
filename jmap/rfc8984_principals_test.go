package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

func TestRFC8984_PrincipalsCapability(t *testing.T) {
	pb := memory.NewMemoryPrincipalsBackend()
	srv := jmap.NewServer(nil, jmap.WithPrincipalsBackend(pb))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Decode session failed: %v", err)
	}

	if _, ok := session.Capabilities[jmap.PrincipalsCapabilityURI]; !ok {
		t.Errorf("expected capability %q in session", jmap.PrincipalsCapabilityURI)
	}
	if _, ok := session.Capabilities[jmap.AvailabilityCapabilityURI]; !ok {
		t.Errorf("expected capability %q in session", jmap.AvailabilityCapabilityURI)
	}

	accCap := session.Accounts["primary"].AccountCapabilities[jmap.PrincipalsCapabilityURI]
	if accCap == nil {
		t.Fatalf("expected account capability %q", jmap.PrincipalsCapabilityURI)
	}
	capBytes, _ := json.Marshal(accCap)
	var pCap jmap.PrincipalCapability
	_ = json.Unmarshal(capBytes, &pCap)

	if pCap.MaxAvailabilityDuration != "P30D" {
		t.Errorf("expected maxAvailabilityDuration 'P30D', got %v", pCap.MaxAvailabilityDuration)
	}
}

func TestRFC8984_PrincipalGetAndQuery(t *testing.T) {
	pb := memory.NewMemoryPrincipalsBackend()
	srv := jmap.NewServer(nil, jmap.WithPrincipalsBackend(pb))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.PrincipalsCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jr)
		return jr
	}

	// 1. Get all principals
	r1 := post([]any{
		[]any{"Principal/get", map[string]any{"accountId": "primary"}, "c1"},
	})
	list1 := r1.MethodResponses[0].Args["list"].([]any)
	if len(list1) == 0 {
		t.Fatal("expected seeded principal in list")
	}

	// 2. Query principal by email
	r2 := post([]any{
		[]any{"Principal/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"email": "user@example.com"},
		}, "c2"},
	})
	ids := r2.MethodResponses[0].Args["ids"].([]any)
	if len(ids) != 1 || ids[0] != "p-primary" {
		t.Errorf("expected query result ['p-primary'], got %v", ids)
	}
}

func TestRFC8984_PrincipalSetLifecycle(t *testing.T) {
	pb := memory.NewMemoryPrincipalsBackend()
	srv := jmap.NewServer(nil, jmap.WithPrincipalsBackend(pb))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.PrincipalsCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jr)
		return jr
	}

	// 1. Create principal
	r1 := post([]any{
		[]any{"Principal/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"p1": map[string]any{
					"name":  "Alice Resource",
					"type":  "resource",
					"email": "alice.resource@example.com",
				},
			},
		}, "c1"},
	})
	created := r1.MethodResponses[0].Args["created"].(map[string]any)
	p1 := created["p1"].(map[string]any)
	p1ID := p1["id"].(string)

	if p1["type"] != "resource" {
		t.Errorf("expected type 'resource', got %v", p1["type"])
	}

	// 2. Update principal
	r2 := post([]any{
		[]any{"Principal/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				p1ID: map[string]any{"description": "Conference Room A"},
			},
		}, "c2"},
	})
	updated := r2.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := updated[p1ID]; !ok {
		t.Fatalf("expected updated entry for %s", p1ID)
	}

	// 3. Destroy principal
	r3 := post([]any{
		[]any{"Principal/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{p1ID},
		}, "c3"},
	})
	destroyed := r3.MethodResponses[0].Args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0] != p1ID {
		t.Errorf("expected destroyed [%s], got %v", p1ID, destroyed)
	}
}

func TestRFC8984_PrincipalGetAvailability(t *testing.T) {
	pb := memory.NewMemoryPrincipalsBackend()
	cb := memory.NewMemoryCalendarsBackend()
	pb.SetCalendarsBackend(cb)

	srv := jmap.NewServer(nil, jmap.WithPrincipalsBackend(pb), jmap.WithCalendarsBackend(cb))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.PrincipalsCapabilityURI, jmap.AvailabilityCapabilityURI, jmap.CalendarsCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jr)
		return jr
	}

	// 1. Get availability for seeded principal p-primary
	r1 := post([]any{
		[]any{"Principal/getAvailability", map[string]any{
			"accountId":   "primary",
			"principalId": "p-primary",
			"utcStart":    "2026-08-01T00:00:00Z",
			"utcEnd":      "2026-08-31T23:59:59Z",
		}, "c1"},
	})
	if r1.MethodResponses[0].Name != "Principal/getAvailability" {
		t.Fatalf("expected Principal/getAvailability, got %s", r1.MethodResponses[0].Name)
	}
	args1 := r1.MethodResponses[0].Args
	if args1["principalId"] != "p-primary" {
		t.Errorf("expected principalId 'p-primary', got %v", args1["principalId"])
	}

	// 2. Disallow availability and assert forbidden error
	_, _ = pb.UpdatePrincipal(nil, "p-primary", map[string]any{"mayGetAvailability": false})
	r2 := post([]any{
		[]any{"Principal/getAvailability", map[string]any{
			"accountId":   "primary",
			"principalId": "p-primary",
			"utcStart":    "2026-08-01T00:00:00Z",
			"utcEnd":      "2026-08-31T23:59:59Z",
		}, "c2"},
	})
	if r2.MethodResponses[0].Name != "error" {
		t.Errorf("expected error response when mayGetAvailability is false, got %s", r2.MethodResponses[0].Name)
	}
}
