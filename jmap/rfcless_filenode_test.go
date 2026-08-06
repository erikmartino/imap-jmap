package jmap_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
)

// TestFileNode_CapabilityAndHandlers tests advertising urn:ietf:params:jmap:filenode and FileNode/* handlers.
// FileNode is a custom (non-RFC) file-storage extension; see the rfcless_ test prefix convention.
func TestFileNode_CapabilityAndHandlers(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Verify capability URI in /.well-known/jmap
	respSession, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer respSession.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(respSession.Body).Decode(&session); err != nil {
		t.Fatalf("Decode session failed: %v", err)
	}

	if _, ok := session.Capabilities[jmap.FileNodeCapabilityURI]; !ok {
		t.Errorf("Expected capability %q in session.Capabilities", jmap.FileNodeCapabilityURI)
	}

	// 2. Issue FileNode/get and FileNode/query method calls
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI},
		"methodCalls": []any{
			[]any{"FileNode/get", map[string]any{
				"accountId": "primary",
			}, "c1"},
			[]any{"FileNode/query", map[string]any{
				"accountId": "primary",
			}, "c2"},
		},
	}
	bodyBytes, _ := json.Marshal(reqPayload)

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Decode Response failed: %v", err)
	}

	if len(jmapResp.MethodResponses) != 2 {
		t.Fatalf("Expected 2 method responses, got %d", len(jmapResp.MethodResponses))
	}

	if jmapResp.MethodResponses[0].Name != "FileNode/get" {
		t.Errorf("Expected 'FileNode/get', got %q", jmapResp.MethodResponses[0].Name)
	}
	if jmapResp.MethodResponses[1].Name != "FileNode/query" {
		t.Errorf("Expected 'FileNode/query', got %q", jmapResp.MethodResponses[1].Name)
	}
}

// doFileNodeRequest issues a single JMAP request against ts and returns the decoded response.
func doFileNodeRequest(t *testing.T, url string, methodCalls []any) jmap.Response {
	t.Helper()
	payload := map[string]any{
		"using":       []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI},
		"methodCalls": methodCalls,
	}
	body, _ := json.Marshal(payload)
	resp, err := authedPost(url+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", resp.StatusCode)
	}
	var jr jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
		t.Fatalf("Decode Response failed: %v", err)
	}
	return jr
}

// TestFileNode_SetGetQueryChangesRoundTrip drives the FileNode extension through the real
// in-memory backend, proving create/get/query/update/destroy and change tracking persist,
// not merely that handlers respond.
func TestFileNode_SetGetQueryChangesRoundTrip(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Create a folder.
	folderResp := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"folder": map[string]any{"name": "Documents", "isFolder": true},
			},
		}, "c1"},
	})
	folderArgs := folderResp.MethodResponses[0].Args
	// Capture the state before any writes so /changes below covers every mutation.
	oldState, _ := folderArgs["oldState"].(string)
	newState, _ := folderArgs["newState"].(string)
	if oldState == newState {
		t.Errorf("state MUST advance after a create; oldState=%q newState=%q", oldState, newState)
	}
	folderCreated, ok := folderArgs["created"].(map[string]any)
	if !ok || len(folderCreated) != 1 {
		t.Fatalf("Expected 1 created folder, got %#v", folderArgs["created"])
	}
	folder := folderCreated["folder"].(map[string]any)
	folderID, _ := folder["id"].(string)
	if folderID == "" {
		t.Fatal("server did not assign an id to the created folder")
	}
	// Server MUST set timestamps so a client cannot tell it apart from a real server.
	if folder["createdAt"] == nil || folder["createdAt"] == "" {
		t.Error("created folder is missing server-set createdAt")
	}

	// Create a file inside the folder, referencing the real folder id.
	fileResp := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"file": map[string]any{
					"name":     "report.txt",
					"parentId": folderID,
					"blobId":   "blob-1",
					"size":     float64(1024),
					"type":     "text/plain",
				},
			},
		}, "c1"},
	})
	fileCreated, ok := fileResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || len(fileCreated) != 1 {
		t.Fatalf("Expected 1 created file, got %#v", fileResp.MethodResponses[0].Args["created"])
	}
	file := fileCreated["file"].(map[string]any)
	fileID, _ := file["id"].(string)
	if pid, _ := file["parentId"].(string); pid != folderID {
		t.Errorf("file parentId not persisted: got %q want %q", pid, folderID)
	}
	// State after both creates, before the update — used to assert update tracking below.
	stateAfterCreates, _ := fileResp.MethodResponses[0].Args["newState"].(string)

	// 2. Retrieve the file by id and verify persisted properties.
	getResp := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{fileID},
		}, "c1"},
	})
	list, ok := getResp.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("Expected 1 FileNode from get, got %#v", getResp.MethodResponses[0].Args["list"])
	}
	got := list[0].(map[string]any)
	if got["name"] != "report.txt" {
		t.Errorf("name not persisted: got %v", got["name"])
	}
	if got["type"] != "text/plain" {
		t.Errorf("type not persisted: got %v", got["type"])
	}
	if got["size"].(float64) != 1024 {
		t.Errorf("size not persisted: got %v", got["size"])
	}

	// 3. Query by parent folder — the file MUST be found under it.
	queryResp := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"parentId": folderID},
		}, "c1"},
	})
	qArgs := queryResp.MethodResponses[0].Args
	if qArgs["total"].(float64) != 1 {
		t.Errorf("Expected total=1 for query by parent, got %v", qArgs["total"])
	}
	qIDs := qArgs["ids"].([]any)
	if len(qIDs) != 1 || qIDs[0].(string) != fileID {
		t.Errorf("Expected query to return file id %q, got %#v", fileID, qIDs)
	}

	// 4. Partial update: rename the file, leaving all other fields intact.
	updResp := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				fileID: map[string]any{"name": "final.txt"},
			},
		}, "c1"},
	})
	if updated, ok := updResp.MethodResponses[0].Args["updated"].(map[string]any); !ok || len(updated) != 1 {
		t.Fatalf("Expected 1 updated FileNode, got %#v", updResp.MethodResponses[0].Args["updated"])
	}

	// Verify the rename persisted AND the untouched fields survived the partial update.
	getResp2 := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/get", map[string]any{"accountId": "primary", "ids": []any{fileID}}, "c1"},
	})
	got2 := getResp2.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if got2["name"] != "final.txt" {
		t.Errorf("rename not persisted: got %v", got2["name"])
	}
	if got2["type"] != "text/plain" || got2["size"].(float64) != 1024 {
		t.Errorf("partial update dropped untouched fields: %#v", got2)
	}

	// 5a. Changes since the initial state: both nodes are new to the client, so they
	// appear only in created (an item created+updated within the window is "created",
	// per RFC 8620 Section 5.2).
	changesResp := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": oldState,
		}, "c1"},
	})
	cArgs := changesResp.MethodResponses[0].Args
	if createdIDs := cArgs["created"].([]any); len(createdIDs) != 2 {
		t.Errorf("Expected 2 created ids in changes since initial state, got %#v", createdIDs)
	}
	if updatedIDs := cArgs["updated"].([]any); len(updatedIDs) != 0 {
		t.Errorf("Expected 0 updated ids in changes since initial state, got %#v", updatedIDs)
	}

	// 5b. Changes since the post-create state: the client already knew both nodes, so
	// the rename MUST surface as an update.
	changesResp2 := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": stateAfterCreates,
		}, "c1"},
	})
	c2Args := changesResp2.MethodResponses[0].Args
	updatedIDs := c2Args["updated"].([]any)
	if len(updatedIDs) != 1 || updatedIDs[0].(string) != fileID {
		t.Errorf("Expected file %q as the sole update, got %#v", fileID, updatedIDs)
	}

	// 6. Destroy the file, then the (now empty) folder.
	destroyResp := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{fileID},
		}, "c1"},
	})
	destroyed := destroyResp.MethodResponses[0].Args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0].(string) != fileID {
		t.Errorf("Expected file %q destroyed, got %#v", fileID, destroyed)
	}

	// The file MUST now be reported in notFound.
	getResp3 := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/get", map[string]any{"accountId": "primary", "ids": []any{fileID}}, "c1"},
	})
	nf := getResp3.MethodResponses[0].Args["notFound"].([]any)
	if len(nf) != 1 || nf[0].(string) != fileID {
		t.Errorf("Expected destroyed file in notFound, got %#v", nf)
	}
}

// TestFileNode_CreationReferenceCompositeSet proves a single /set can create a folder and a
// child that references it via a #creationId placeholder (RFC 8620 Section 5.3), so a client
// performs a composite update in one round trip exactly as it would against a real server.
func TestFileNode_CreationReferenceCompositeSet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"folder": map[string]any{"name": "Projects", "isFolder": true},
				"child": map[string]any{
					"name":     "notes.md",
					"parentId": "#folder", // forward reference to the folder created in the same call
					"type":     "text/markdown",
				},
			},
		}, "c1"},
	})

	args := resp.MethodResponses[0].Args
	if notCreated, ok := args["notCreated"].(map[string]any); ok && len(notCreated) != 0 {
		t.Fatalf("Expected no creation failures, got %#v", notCreated)
	}
	created, ok := args["created"].(map[string]any)
	if !ok || len(created) != 2 {
		t.Fatalf("Expected 2 created nodes, got %#v", args["created"])
	}

	folderID := created["folder"].(map[string]any)["id"].(string)
	child := created["child"].(map[string]any)
	// The #folder placeholder MUST have been substituted with the real assigned id.
	if pid, _ := child["parentId"].(string); pid != folderID {
		t.Errorf("creation reference #folder not resolved: child parentId=%q want %q", pid, folderID)
	}
}

// TestFileNode_QueryChanges proves FileNode/queryChanges reports newly-matching objects as
// added (with an index) and changed/removed objects as removed, driven by the real backend.
func TestFileNode_QueryChanges(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Baseline query state with no nodes.
	q0 := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/query", map[string]any{"accountId": "primary"}, "c1"},
	})
	baseState, _ := q0.MethodResponses[0].Args["queryState"].(string)

	// Create a node after the baseline.
	setResp := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"f1": map[string]any{"name": "a.txt"}},
		}, "c1"},
	})
	newID := setResp.MethodResponses[0].Args["created"].(map[string]any)["f1"].(map[string]any)["id"].(string)

	// queryChanges since the baseline MUST report the new node as added at index 0.
	qc := doFileNodeRequest(t, ts.URL, []any{
		[]any{"FileNode/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": baseState,
		}, "c1"},
	})
	args := qc.MethodResponses[0].Args
	addedRaw, _ := args["added"].([]any)
	if len(addedRaw) != 1 {
		t.Fatalf("expected 1 added item, got %#v", args["added"])
	}
	added := addedRaw[0].(map[string]any)
	if added["id"] != newID {
		t.Errorf("added id = %v, want %q", added["id"], newID)
	}
	if added["index"].(float64) != 0 {
		t.Errorf("added index = %v, want 0", added["index"])
	}
}

// TestFileNode_PushStateChangeOnSet proves a FileNode mutation emits an RFC 8620 Section 7.1
// StateChange push event over the EventSource stream, so a subscribed UI is notified.
func TestFileNode_PushStateChangeOnSet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req := authedRequest(t, "GET", ts.URL+"/eventsource?types=FileNode&closeafter=state", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /eventsource failed: %v", err)
	}
	defer resp.Body.Close()

	// Trigger a FileNode change after the listener connects.
	go func() {
		time.Sleep(100 * time.Millisecond)
		doFileNodeRequest(t, ts.URL, []any{
			[]any{"FileNode/set", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"f1": map[string]any{"name": "pushed.txt", "type": "text/plain"},
				},
			}, "c1"},
		})
	}()

	scanner := bufio.NewScanner(resp.Body)
	var eventLine, dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventLine = line
		} else if strings.HasPrefix(line, "data:") {
			dataLine = line
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner error reading SSE stream: %v", err)
	}

	if eventLine != "event: state" {
		t.Errorf("Expected 'event: state', got %q", eventLine)
	}
	if !strings.Contains(dataLine, "StateChange") || !strings.Contains(dataLine, "FileNode") {
		t.Errorf("Expected StateChange payload naming FileNode, got %q", dataLine)
	}
}
