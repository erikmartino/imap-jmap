package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_TaskAndGroupObjects tests Task (@type:"Task") and Group (@type:"Group") objects per RFC 8984 §5.2 / §5.3.
func TestRFC8984_TaskAndGroupObjects(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create a Task object per RFC 8984 §5.2
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"t1": map[string]any{
					"@type":             "Task",
					"title":             "Complete Security Audit",
					"due":               "2026-08-15T17:00:00Z",
					"estimatedDuration": "PT4H",
					"percentComplete":   50,
					"progress":          "in-progress",
					"progressUpdated":   "2026-08-04T12:00:00Z",
				},
				"g1": map[string]any{
					"@type":  "Group",
					"title":  "Project Milestones",
					"source": "https://example.com/project/milestones.json",
					"entries": map[string]any{
						"entry-1": map[string]any{
							"@type": "Task",
							"title": "Subtask A",
						},
					},
				},
			},
		}, "call-1"},
	}

	resp := postJMAP(t, ts.URL, using, createReq)
	created, ok := resp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["t1"] == nil || created["g1"] == nil {
		t.Fatalf("CalendarEvent/set task/group create failed: %+v", resp.MethodResponses[0].Args)
	}

	taskID := created["t1"].(map[string]any)["id"].(string)
	groupID := created["g1"].(map[string]any)["id"].(string)

	// 2. Retrieve Task and Group objects and verify properties
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{taskID, groupID},
		}, "call-2"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(list))
	}

	var taskData, groupData map[string]any
	for _, item := range list {
		m := item.(map[string]any)
		if m["id"] == taskID {
			taskData = m
		} else if m["id"] == groupID {
			groupData = m
		}
	}

	if taskData["@type"] != "Task" || taskData["due"] != "2026-08-15T17:00:00Z" || taskData["progress"] != "in-progress" {
		t.Errorf("unexpected Task data: %+v", taskData)
	}

	if groupData["@type"] != "Group" || groupData["source"] != "https://example.com/project/milestones.json" {
		t.Errorf("unexpected Group data: %+v", groupData)
	}

	// 3. Patch Task progress
	updateReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				taskID: map[string]any{
					"percentComplete": 100,
					"progress":        "completed",
				},
			},
		}, "call-3"},
	}

	updateResp := postJMAP(t, ts.URL, using, updateReq)
	updated, _ := updateResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := updated[taskID]; !ok {
		t.Fatalf("CalendarEvent/set task update failed: %+v", updateResp.MethodResponses[0].Args)
	}
}
