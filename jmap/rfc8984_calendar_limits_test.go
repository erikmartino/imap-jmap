package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8984_CalendarQueryCapabilityLimits tests enforcement of minDateTime,
// maxDateTime, and maxExpandedQueryDuration capability limits on CalendarEvent/query
// and CalendarEvent/set per draft-ietf-jmap-calendars-27 Section 5.11 and 1.5.1.
func TestRFC8984_CalendarQueryCapabilityLimits(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.11", spectest.SHOULD,
		"minDateTime/maxDateTime/maxExpandedQueryDuration capability limits are enforced on queries.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create a recurring event
	createResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"ev1": map[string]any{
					"title":    "Daily Standup",
					"start":    "2026-06-01T09:00:00",
					"duration": "PT30M",
					"recurrenceRules": []any{
						map[string]any{
							"@type":     "RecurrenceRule",
							"frequency": "daily",
						},
					},
				},
			},
		}, "s1"},
	})
	created, ok := createResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["ev1"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", createResp.MethodResponses[0].Args)
	}
	evID := created["ev1"].(map[string]any)["id"].(string)

	// 2. Query with expandRecurrences: true within maxExpandedQueryDuration (P730D):
	// A 30-day range (2026-06-01 to 2026-07-01) is well within 730 days -> MUST succeed.
	qWithinLimit := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId":         "primary",
			"expandRecurrences": true,
			"filter": map[string]any{
				"after":  "2026-06-01T00:00:00",
				"before": "2026-07-01T00:00:00",
			},
		}, "q1"},
	})
	if qWithinLimit.MethodResponses[0].Name != "CalendarEvent/query" {
		t.Fatalf("expected CalendarEvent/query for range within limit, got: %+v", qWithinLimit.MethodResponses[0])
	}
	ids, _ := qWithinLimit.MethodResponses[0].Args["ids"].([]any)
	if len(ids) == 0 {
		t.Errorf("expected expanded occurrences within 30-day range, got 0")
	}

	// 3. Query with expandRecurrences: true exceeding maxExpandedQueryDuration (P730D):
	// A 1000-day range (2024-01-01 to 2026-10-01) exceeds 730 days -> MUST fail with expandDurationTooLarge.
	qExceedLimit := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId":         "primary",
			"expandRecurrences": true,
			"filter": map[string]any{
				"after":  "2024-01-01T00:00:00",
				"before": "2026-10-01T00:00:00",
			},
		}, "q2"},
	})
	if qExceedLimit.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for expandRecurrences exceeding maxExpandedQueryDuration, got: %+v", qExceedLimit.MethodResponses[0])
	}
	if errType, _ := qExceedLimit.MethodResponses[0].Args["type"].(string); errType != "expandDurationTooLarge" {
		t.Errorf("expected expandDurationTooLarge error, got: %q", errType)
	}

	// 4. Same 1000-day range with expandRecurrences: false -> MUST NOT return expandDurationTooLarge.
	qNoExpand := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId":         "primary",
			"expandRecurrences": false,
			"filter": map[string]any{
				"after":  "2024-01-01T00:00:00",
				"before": "2026-10-01T00:00:00",
			},
		}, "q3"},
	})
	if qNoExpand.MethodResponses[0].Name != "CalendarEvent/query" {
		t.Fatalf("expected CalendarEvent/query when expandRecurrences is false, got: %+v", qNoExpand.MethodResponses[0])
	}

	// 5. Query with 'after' date earlier than minDateTime (1900-01-01T00:00:00) -> invalidArguments.
	qBeforeMin := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"after": "1850-01-01T00:00:00",
			},
		}, "q4"},
	})
	if qBeforeMin.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for query date before minDateTime, got: %+v", qBeforeMin.MethodResponses[0])
	}
	if errType, _ := qBeforeMin.MethodResponses[0].Args["type"].(string); errType != "invalidArguments" {
		t.Errorf("expected invalidArguments error for query before minDateTime, got: %q", errType)
	}

	// 6. Query with 'before' date later than maxDateTime (9999-12-31T23:59:59) -> invalidArguments.
	qAfterMax := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"before": "10000-01-01T00:00:00",
			},
		}, "q5"},
	})
	if qAfterMax.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for query date after maxDateTime, got: %+v", qAfterMax.MethodResponses[0])
	}
	if errType, _ := qAfterMax.MethodResponses[0].Args["type"].(string); errType != "invalidArguments" {
		t.Errorf("expected invalidArguments error for query after maxDateTime, got: %q", errType)
	}

	// 7. Query within advertised minDateTime / maxDateTime bounds -> MUST succeed.
	qValidBounds := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"after":  "1900-01-01T00:00:00",
				"before": "9999-12-31T23:59:59",
			},
		}, "q6"},
	})
	if qValidBounds.MethodResponses[0].Name != "CalendarEvent/query" {
		t.Fatalf("expected CalendarEvent/query for valid bounds, got: %+v", qValidBounds.MethodResponses[0])
	}

	// 8. CalendarEvent/set create: event start earlier than minDateTime -> notCreated invalidProperties.
	setEarly := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"badEarly": map[string]any{
					"title": "Too Early",
					"start": "1850-05-01T12:00:00",
				},
			},
		}, "s2"},
	})
	notCreated, _ := setEarly.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated["badEarly"] == nil {
		t.Fatalf("expected badEarly in notCreated, got: %+v", setEarly.MethodResponses[0].Args)
	}
	errObj := notCreated["badEarly"].(map[string]any)
	if errObj["type"] != "invalidProperties" {
		t.Errorf("expected invalidProperties for start before minDateTime, got: %v", errObj["type"])
	}

	// 9. CalendarEvent/set create: event start later than maxDateTime -> notCreated invalidProperties.
	setLate := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"badLate": map[string]any{
					"title": "Too Late",
					"start": "10000-01-01T00:00:00",
				},
			},
		}, "s3"},
	})
	notCreatedLate, _ := setLate.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreatedLate["badLate"] == nil {
		t.Fatalf("expected badLate in notCreated, got: %+v", setLate.MethodResponses[0].Args)
	}
	errObjLate := notCreatedLate["badLate"].(map[string]any)
	if errObjLate["type"] != "invalidProperties" {
		t.Errorf("expected invalidProperties for start after maxDateTime, got: %v", errObjLate["type"])
	}

	// 10. CalendarEvent/set update: update existing event with start before minDateTime -> notUpdated invalidProperties.
	updateEarly := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				evID: map[string]any{
					"start": "1800-01-01T00:00:00",
				},
			},
		}, "s4"},
	})
	notUpdated, _ := updateEarly.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if notUpdated[evID] == nil {
		t.Fatalf("expected %s in notUpdated, got: %+v", evID, updateEarly.MethodResponses[0].Args)
	}
	errObjUpdate := notUpdated[evID].(map[string]any)
	if errObjUpdate["type"] != "invalidProperties" {
		t.Errorf("expected invalidProperties for update start before minDateTime, got: %v", errObjUpdate["type"])
	}
}
