package nextcloud

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSyncStateRoundTrip(t *testing.T) {
	tokens := map[string]syncToken{
		"cal-a": {Kind: syncTokenKindSync, Token: "http://sabre.io/ns/sync/5"},
		"cal-b": {Kind: syncTokenKindCTag, Token: "ctag-9"},
	}
	state := encodeSyncState(tokens)
	if !strings.HasPrefix(state, "sync-v2:") {
		t.Fatalf("expected sync-v2 state, got %q", state)
	}
	// Deterministic: the same vector must produce the same opaque string.
	if again := encodeSyncState(tokens); again != state {
		t.Errorf("encodeSyncState is not deterministic:\n %q\n %q", state, again)
	}

	got, err := decodeSyncState(state)
	if err != nil {
		t.Fatalf("decodeSyncState: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tokens, got %v", got)
	}
	if got["cal-a"].Kind != syncTokenKindSync || got["cal-a"].Token != "http://sabre.io/ns/sync/5" {
		t.Errorf("cal-a = %+v", got["cal-a"])
	}
	if got["cal-b"].Kind != syncTokenKindCTag || got["cal-b"].Token != "ctag-9" {
		t.Errorf("cal-b = %+v", got["cal-b"])
	}
}

func TestDecodeSyncStateLegacyV1(t *testing.T) {
	// v1 encoded map[string]string with no token kind.
	data := base64.RawURLEncoding.EncodeToString([]byte(`{"cal-a":"tok-1"}`))
	got, err := decodeSyncState("sync-v1:" + data)
	if err != nil {
		t.Fatalf("decodeSyncState v1: %v", err)
	}
	if got["cal-a"].Token != "tok-1" {
		t.Errorf("expected token tok-1, got %+v", got["cal-a"])
	}
	if got["cal-a"].Kind != "" {
		t.Errorf("legacy v1 token should have no kind, got %q", got["cal-a"].Kind)
	}
}

func TestSyncTokensFromStatuses(t *testing.T) {
	statuses := map[string]*CalendarSyncStatus{
		"a": {SyncToken: "s1", CTag: "c1"},
		"b": {CTag: "c2"},
		"c": {},
	}
	got := syncTokensFromStatuses(statuses)
	if got["a"].Kind != syncTokenKindSync || got["a"].Token != "s1" {
		t.Errorf("a = %+v, want sync/s1", got["a"])
	}
	if got["b"].Kind != syncTokenKindCTag || got["b"].Token != "c2" {
		t.Errorf("b = %+v, want ctag/c2", got["b"])
	}
	if _, ok := got["c"]; ok {
		t.Errorf("status with no token should be omitted, got %+v", got["c"])
	}
}
