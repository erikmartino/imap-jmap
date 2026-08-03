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

// postMethod is a small helper to run a single JMAP method call and return the
// response args.
func postMethod(t *testing.T, srv *jmap.Server, method string, args map[string]any) map[string]any {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{
			[]any{method, args, "c1"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /jmap %s failed: %v", method, err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode %s response: %v", method, err)
	}
	if len(jmapResp.MethodResponses) != 1 {
		t.Fatalf("Expected 1 method response for %s", method)
	}
	return jmapResp.MethodResponses[0].Args
}

// TestRFC8620_ChangesStatesAdvance verifies that set/get/changes methods no longer hardcode
// the change state to "0" and that created objects appear in subsequent /changes per RFC 8620 Section 5.2.
func TestRFC8620_ChangesStatesAdvance(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))

	// AddressBook/set must return a real oldState/newState pair.
	setArgs := postMethod(t, srv, "AddressBook/set", map[string]any{
		"accountId": "primary",
		"create": map[string]any{
			"ab1": map[string]any{"name": "Work"},
		},
	})
	oldState, _ := setArgs["oldState"].(string)
	newState, _ := setArgs["newState"].(string)
	if oldState == "0" || newState == "0" || oldState == "" || newState == "" {
		t.Errorf("AddressBook/set returned hardcoded states: old=%q new=%q", oldState, newState)
	}
	if oldState == newState {
		t.Errorf("AddressBook/set oldState must differ from newState, both %q", newState)
	}
	createdAB := setArgs["created"].(map[string]any)
	abID := createdAB["ab1"].(map[string]any)["id"].(string)

	// AddressBook/changes from oldState must report the new address book as created.
	changesArgs := postMethod(t, srv, "AddressBook/changes", map[string]any{
		"accountId":  "primary",
		"sinceState": oldState,
	})
	cCreated := changesArgs["created"].([]any)
	if len(cCreated) != 1 || cCreated[0].(string) != abID {
		t.Errorf("AddressBook/changes created=%v, want [%s]", cCreated, abID)
	}
	if cNew, _ := changesArgs["newState"].(string); cNew != newState {
		t.Errorf("AddressBook/changes newState=%q, want %q", cNew, newState)
	}
}

// TestRFC9007_MDNParseRejectsMissing uses the MDN capability to ensure blobIds that do
// not exist are reported via notFound and not fabricated (RFC 9007 Section 3.3).
func TestRFC9007_MDNParseRejectsMissing(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/parse", map[string]any{
				"accountId": "primary",
				"blobIds":   []any{"does-not-exist"},
			}, "c1"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /jmap MDN/parse failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	args := jmapResp.MethodResponses[0].Args

	if nf, ok := args["notFound"].([]any); !ok || len(nf) != 1 {
		t.Errorf("expected missing blob in notFound, got %v", args["notFound"])
	}
	parsed, _ := args["parsed"].(map[string]any)
	if len(parsed) != 0 {
		t.Errorf("fabricated MDN for missing blob: %v", parsed)
	}
}

// TestRFC8984_CalendarStateAdvance verifies Calendar/set state tokens advance on the
// calendar backend as well per RFC 8984.
func TestRFC8984_CalendarStateAdvance(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := jmap.NewServer(nil, jmap.WithCalendarsBackend(calBackend))

	setArgs := postMethod(t, srv, "Calendar/set", map[string]any{
		"accountId": "primary",
		"create": map[string]any{
			"cal1": map[string]any{"name": "Holidays"},
		},
	})
	oldState, _ := setArgs["oldState"].(string)
	newState, _ := setArgs["newState"].(string)
	if oldState == "0" || newState == "0" || oldState == "" || newState == "" {
		t.Errorf("Calendar/set returned hardcoded states: old=%q new=%q", oldState, newState)
	}
	if oldState == newState {
		t.Errorf("Calendar/set oldState must differ from newState, both %q", newState)
	}
}

// TestRFC8620_IfInStateMismatch verifies RFC 8620 Section 5.3 stateMismatch error when ifInState does not match current state.
func TestRFC8620_IfInStateMismatch(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/set", map[string]any{
				"accountId": "primary",
				"ifInState": "invalid-token",
				"create": map[string]any{
					"e1": map[string]any{"subject": "Should fail"},
				},
			}, "c1"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "error" {
		t.Fatalf("Expected error method response, got %q", methodResp.Name)
	}
	errType, _ := methodResp.Args["type"].(string)
	if errType != "stateMismatch" {
		t.Errorf("Expected error type 'stateMismatch', got %q", errType)
	}
}

