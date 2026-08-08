package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_QueryRejectsUnknownFilter verifies CalendarEvent/query rejects an unknown filter
// condition with unsupportedFilter instead of silently matching everything (AGENTS "No
// Fallthrough Match Defaults" rule; RFC 8620 Section 5.5).
func TestRFC8984_QueryRejectsUnknownFilter(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"bogusProperty": "x"},
		}, "c1"},
	})
	if resp.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for unknown filter, got %+v", resp.MethodResponses[0])
	}
	if got, _ := resp.MethodResponses[0].Args["type"].(string); got != "unsupportedFilter" {
		t.Errorf("expected unsupportedFilter, got %q", got)
	}
}

// TestRFC8984_QueryRejectsUnknownSort verifies an unsupported sort property is rejected.
func TestRFC8984_QueryRejectsUnknownSort(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"sort":      []any{map[string]any{"property": "nonsense"}},
		}, "c1"},
	})
	if resp.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for unknown sort, got %+v", resp.MethodResponses[0])
	}
	if got, _ := resp.MethodResponses[0].Args["type"].(string); got != "unsupportedSort" {
		t.Errorf("expected unsupportedSort, got %q", got)
	}
}

// TestRFC8984_QueryFilterOperator verifies AND/OR/NOT FilterOperator evaluation.
func TestRFC8984_QueryFilterOperator(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"a": map[string]any{"title": "Alpha Review", "start": "2026-08-01T10:00:00Z"},
				"b": map[string]any{"title": "Beta Review", "start": "2026-08-02T10:00:00Z"},
			},
		}, "s"},
	})

	// (title~Alpha OR title~Beta) AND NOT(title~Beta) -> only Alpha.
	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"operator": "AND",
				"conditions": []any{
					map[string]any{
						"operator": "OR",
						"conditions": []any{
							map[string]any{"title": "Alpha"},
							map[string]any{"title": "Beta"},
						},
					},
					map[string]any{
						"operator":   "NOT",
						"conditions": []any{map[string]any{"title": "Beta"}},
					},
				},
			},
		}, "c1"},
	})
	if resp.MethodResponses[0].Name == "error" {
		t.Fatalf("operator filter rejected: %+v", resp.MethodResponses[0].Args)
	}
	ids, _ := resp.MethodResponses[0].Args["ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("expected exactly 1 match (Alpha), got %d: %+v", len(ids), ids)
	}
}

// TestRFC8984_QueryCanCalculateChanges verifies canCalculateChanges is true for a normal query
// but false when expandRecurrences is set (synthetic occurrence ids are not change-tracked).
func TestRFC8984_QueryCanCalculateChanges(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	normal := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary"}, "c1"},
	})
	if ccc, _ := normal.MethodResponses[0].Args["canCalculateChanges"].(bool); !ccc {
		t.Errorf("expected canCalculateChanges=true for a normal query")
	}

	expanded := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary", "expandRecurrences": true}, "c2"},
	})
	if ccc, _ := expanded.MethodResponses[0].Args["canCalculateChanges"].(bool); ccc {
		t.Errorf("expected canCalculateChanges=false when expandRecurrences is set")
	}
}
