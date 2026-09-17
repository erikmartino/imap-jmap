package jmaptransport_test

import (
	"testing"

	"imap-jmap/jmap/jmappush"
	"imap-jmap/jmap/jmaptransport"
	"imap-jmap/jmap/spectest"
)

func TestTransportPrimitives(t *testing.T) {
	spectest.RequireID(t, "RFC8620#7.1-p1-MUST", "Server-Sent Events filtering")
	spectest.RequireID(t, "RFC8887#4.3.5.2-p1-MUST", "WebSocket push notification filtering")

	// Nil inputs
	if jmaptransport.FilterStateChange(nil, "", "", "*", nil) != nil {
		t.Fatalf("expected nil FilterStateChange for nil event")
	}
	if _, ok := jmaptransport.FilterWebSocketPush(nil, nil); ok {
		t.Fatalf("expected false FilterWebSocketPush for nil event")
	}

	evt := &jmappush.StateChange{
		Type: "StateChange",
		Changed: map[string]map[string]string{
			"acc-1": {
				"Email":   "s-1",
				"Mailbox": "s-2",
			},
		},
	}

	// 1. SSE filtering by types & account matching
	filterTypes := map[string]bool{"Email": true}
	filteredSSE := jmaptransport.FilterStateChange(evt, "acc-1", "", "Email", filterTypes)
	if filteredSSE == nil || filteredSSE.Changed["acc-1"]["Email"] != "s-1" || filteredSSE.Changed["acc-1"]["Mailbox"] != "" {
		t.Fatalf("unexpected SSE filtering result: %+v", filteredSSE)
	}

	// Mismatched account SSE filter
	if jmaptransport.FilterStateChange(evt, "other-acc", "other-user", "*", nil) != nil {
		t.Fatalf("expected nil for mismatched account filter")
	}

	// 2. WebSocket push filtering
	rawWS, ok := jmaptransport.FilterWebSocketPush(evt, []string{"Mailbox"})
	if !ok || len(rawWS) == 0 {
		t.Fatalf("expected WebSocket push filtering to succeed")
	}

	// Unmatched push types
	_, unMatchedOK := jmaptransport.FilterWebSocketPush(evt, []string{"Calendar"})
	if unMatchedOK {
		t.Fatalf("expected false for unmatched WebSocket push data types")
	}

	// Empty pushTypes (all allowed)
	rawAllWS, allOK := jmaptransport.FilterWebSocketPush(evt, nil)
	if !allOK || len(rawAllWS) == 0 {
		t.Fatalf("expected true for empty pushTypes filter")
	}
}
