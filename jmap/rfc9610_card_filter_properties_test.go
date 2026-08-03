package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC9610_Section3_3_CardFilterPropertiesPosNeg tests Card/query positive and negative filter matching per RFC 9610 Section 3.3.1.
func TestRFC9610_Section3_3_CardFilterPropertiesPosNeg(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := context.Background()

	ab, _ := srv.ContactsBackend.CreateAddressBook(ctx, &jmap.AddressBook{Name: "Main AddressBook"})
	card1, _ := srv.ContactsBackend.CreateCard(ctx, &jmap.Card{
		AddressBookIDs: map[jmap.Id]bool{ab.ID: true},
		Name:           &jmap.JSContactName{Full: "Alice Smith"},
		Emails:         map[string]*jmap.JSContactEmailAddress{"e1": {Address: "alice@example.com"}},
	})
	card2, _ := srv.ContactsBackend.CreateCard(ctx, &jmap.Card{
		AddressBookIDs: map[jmap.Id]bool{ab.ID: true},
		Name:           &jmap.JSContactName{Full: "Bob Jones"},
		Emails:         map[string]*jmap.JSContactEmailAddress{"e2": {Address: "bob@example.com"}},
	})

	using := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}

	// 1. Positive filter by email: "alice@example.com" -> returns card1
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Card/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"email": "alice@example.com"},
		}, "c1"},
	})
	ids1, _ := res1.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 1 || ids1[0] != string(card1.ID) {
		t.Errorf("Card email positive expected [%s], got %v", card1.ID, ids1)
	}

	// 2. Negative filter by email: "charlie@example.com" -> returns empty []
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Card/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"email": "charlie@example.com"},
		}, "c2"},
	})
	ids2, _ := res2.MethodResponses[0].Args["ids"].([]any)
	if len(ids2) != 0 {
		t.Errorf("Card email negative expected empty [], got %v", ids2)
	}

	_ = card2
}
