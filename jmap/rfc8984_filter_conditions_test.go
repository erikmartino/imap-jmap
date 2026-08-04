package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_OwnerAndAttendeeFilterConditions tests owner and attendee filter conditions positive and negative matching.
func TestRFC8984_OwnerAndAttendeeFilterConditions(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create two events: one owned by Alice, one with Bob as attendee
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title": "Alice's Strategy Meeting",
					"participants": map[string]any{
						"p1": map[string]any{
							"name":  "Alice Smith",
							"email": "alice@example.com",
							"roles": map[string]any{"owner": true},
						},
						"p2": map[string]any{
							"name":  "Charlie Brown",
							"email": "charlie@example.com",
							"roles": map[string]any{"attendee": true},
						},
					},
				},
				"c2": map[string]any{
					"title": "Bob's Design Review",
					"participants": map[string]any{
						"p1": map[string]any{
							"name":  "Bob Jones",
							"email": "bob@example.com",
							"roles": map[string]any{"attendee": true},
						},
					},
				},
			},
		}, "call-1"},
	}

	resp := postJMAP(t, ts.URL, using, createReq)
	created, ok := resp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c1"] == nil || created["c2"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", resp.MethodResponses[0].Args)
	}

	id1 := created["c1"].(map[string]any)["id"].(string)
	id2 := created["c2"].(map[string]any)["id"].(string)

	// 2. Query filter owner: "alice@example.com" -> MUST return id1, exclude id2
	queryOwnerReq := []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"owner": "alice@example.com",
			},
		}, "call-2"},
	}

	queryOwnerResp := postJMAP(t, ts.URL, using, queryOwnerReq)
	idsOwner, _ := queryOwnerResp.MethodResponses[0].Args["ids"].([]any)

	found1, found2 := false, false
	for _, rawID := range idsOwner {
		s := rawID.(string)
		if s == id1 {
			found1 = true
		}
		if s == id2 {
			found2 = true
		}
	}
	if !found1 || found2 {
		t.Errorf("expected owner filter to match id1 and exclude id2, got ids=%+v", idsOwner)
	}

	// 3. Query filter attendee: "bob@example.com" -> MUST return id2, exclude id1
	queryAttendeeReq := []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"attendee": "bob@example.com",
			},
		}, "call-3"},
	}

	queryAttendeeResp := postJMAP(t, ts.URL, using, queryAttendeeReq)
	idsAttendee, _ := queryAttendeeResp.MethodResponses[0].Args["ids"].([]any)

	found1, found2 = false, false
	for _, rawID := range idsAttendee {
		s := rawID.(string)
		if s == id1 {
			found1 = true
		}
		if s == id2 {
			found2 = true
		}
	}
	if found1 || !found2 {
		t.Errorf("expected attendee filter to match id2 and exclude id1, got ids=%+v", idsAttendee)
	}
}
