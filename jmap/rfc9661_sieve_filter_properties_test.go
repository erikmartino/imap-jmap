package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC9661_SieveScriptFilterPropertiesPosNeg tests SieveScript/query name and isValid filter conditions per RFC 9661 Section 4.
func TestRFC9661_SieveScriptFilterPropertiesPosNeg(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := seedCtx()

	s1, _ := srv.SieveBackend.CreateSieveScript(ctx, &jmap.SieveScript{
		Name:    "VacationFilter",
		Content: `require ["fileinto"]; fileinto "Vacation";`,
		IsValid: true,
	})
	s2, _ := srv.SieveBackend.CreateSieveScript(ctx, &jmap.SieveScript{
		Name:    "BrokenDraft",
		Content: `invalid sieve syntax {{{`,
		IsValid: false,
	})

	using := []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}

	// 1. Positive filter by isValid: true -> returns s1
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"SieveScript/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"isValid": true},
		}, "c1"},
	})
	ids1, _ := res1.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 1 || ids1[0] != string(s1.ID) {
		t.Errorf("SieveScript isValid:true expected [%s], got %v", s1.ID, ids1)
	}

	// 2. Positive filter by name: "VacationFilter" -> returns s1
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"SieveScript/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"name": "VacationFilter"},
		}, "c2"},
	})
	ids2, _ := res2.MethodResponses[0].Args["ids"].([]any)
	if len(ids2) != 1 || ids2[0] != string(s1.ID) {
		t.Errorf("SieveScript name positive expected [%s], got %v", s1.ID, ids2)
	}

	// 3. Negative filter by name: "NonExistentScript999" -> returns empty []
	res3 := postJMAP(t, ts.URL, using, []any{
		[]any{"SieveScript/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"name": "NonExistentScript999"},
		}, "c3"},
	})
	ids3, _ := res3.MethodResponses[0].Args["ids"].([]any)
	if len(ids3) != 0 {
		t.Errorf("SieveScript name negative expected empty [], got %v", ids3)
	}

	_ = s2
}
