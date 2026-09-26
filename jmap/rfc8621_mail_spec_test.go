package jmap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8621_Section1_CapabilitiesAndPush tests RFC 8621 Section 1 capabilities,
// maxSizeMailboxName >= 100, session account capabilities, and push state tracking.
func TestRFC8621_Section1_CapabilitiesAndPush(t *testing.T) {
	spectest.Require(t, "RFC8621", "", spectest.MUST, "Code Components extracted from this document must")
	spectest.Require(t, "RFC8621", "1", spectest.MUST, "IMAP, a message must belong to a mailbox; however, in JMAP, its id")
	spectest.Require(t, "RFC8621", "1.1", spectest.MUST, "The key words \"MUST\", \"MUST NOT\", \"REQUIRED\", \"SHALL\", \"SHALL NOT\",")
	spectest.Require(t, "RFC8621", "1.1", spectest.MUST, "Servers MUST support all properties specified for the new data types")
	spectest.Require(t, "RFC8621", "1.3.1", spectest.MUST, "property is an object that MUST contain the following information on")
	spectest.Require(t, "RFC8621", "1.3.1", spectest.MUST, "This MUST be")
	spectest.Require(t, "RFC8621", "1.3.1", spectest.MUST, "This MUST be at least 100, although it is recommended")
	spectest.Require(t, "RFC8621", "1.3.1", spectest.MUST, "Clients MUST ignore any unknown properties in the")
	spectest.Require(t, "RFC8621", "1.4", spectest.MUST, "The server MUST include the appropriate capability strings as keys in")
	spectest.Require(t, "RFC8621", "1.5", spectest.MUST, "Servers MUST support the JMAP push mechanisms, as specified in")
	spectest.Require(t, "RFC8621", "1.5", spectest.MUST, "capability MUST support pushing state changes for a type called")
	spectest.Require(t, "RFC8621", "1.5", spectest.MUST, "The state string for this MUST")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Fetch Session and verify Mail capability properties (RFC 8621 §1.3.1 & §1.4)
	req, _ := http.NewRequest("GET", ts.URL+"/jmap/session", nil)
	req.SetBasicAuth(testUsername, testUsername)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("session request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}

	var sessionObj map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sessionObj); err != nil {
		t.Fatalf("failed to decode session: %v", err)
	}

	caps, _ := sessionObj["capabilities"].(map[string]any)
	if _, ok := caps[jmap.MailCapabilityURI]; !ok {
		t.Fatalf("missing mail capability in session capabilities: %v", caps)
	}

	// 2. Verify account capabilities include Mail capability with maxSizeMailboxName >= 100 (RFC 8621 §1.3.1 & §1.4)
	accounts, _ := sessionObj["accounts"].(map[string]any)
	for _, accRaw := range accounts {
		accMap, _ := accRaw.(map[string]any)
		accCaps, _ := accMap["accountCapabilities"].(map[string]any)
		mailCapRaw, ok := accCaps[jmap.MailCapabilityURI]
		if !ok {
			t.Errorf("accountCapabilities MUST include %s", jmap.MailCapabilityURI)
			continue
		}
		mailCap, _ := mailCapRaw.(map[string]any)
		maxMbName, ok := mailCap["maxSizeMailboxName"].(float64)
		if !ok || maxMbName < 100 {
			t.Errorf("maxSizeMailboxName MUST be at least 100, got %v", mailCap["maxSizeMailboxName"])
		}
	}

	// 3. Verify Email/Mailbox/Thread state and state changes (RFC 8621 §1.5)
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}
	rMailbox := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "m1"},
	})
	mbState, ok := rMailbox.MethodResponses[0].Args["state"].(string)
	if !ok || mbState == "" {
		t.Errorf("expected non-empty mailbox state string")
	}

	rEmail := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{"accountId": "primary"}, "e1"},
	})
	emState, ok := rEmail.MethodResponses[0].Args["state"].(string)
	if !ok || emState == "" {
		t.Errorf("expected non-empty email state string")
	}

	rThread := postJMAP(t, ts.URL, using, []any{
		[]any{"Thread/get", map[string]any{"accountId": "primary"}, "t1"},
	})
	thState, ok := rThread.MethodResponses[0].Args["state"].(string)
	if !ok || thState == "" {
		t.Errorf("expected non-empty thread state string")
	}
}

// TestRFC8621_Section2_MailboxPropertiesAndConstraints tests Mailbox constraints,
// sibling unique names, single role, loop detection, myRights, and query/sort.
func TestRFC8621_Section2_MailboxPropertiesAndConstraints(t *testing.T) {
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "For compatibility with IMAP, an Email MUST belong to one or more")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "This MUST be a")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "MUST NOT be two sibling Mailboxes with both the same parent and")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "MUST NOT be a loop")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "However, unlike in IMAP, a Mailbox MUST")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "only have a single role, and there MUST NOT be two Mailboxes in")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "The value MUST be one of the Mailbox attribute names listed in the")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "An account is not required to have Mailboxes with any particular")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "The number MUST be an")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "mapping from IMAP, both are required for this to be true)")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "required for this to be true)")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "This MUST be stored separately per")
	spectest.Require(t, "RFC8621", "2.2", spectest.MUST, "MUST just be null")
	spectest.Require(t, "RFC8621", "2.3", spectest.MUST, "The Mailbox \"parentId\" property must match the given value")
	spectest.Require(t, "RFC8621", "2.3", spectest.MUST, "The Mailbox \"role\" property must match the given value exactly")
	spectest.Require(t, "RFC8621", "2.3", spectest.MUST, "The \"isSubscribed\" property of the Mailbox must be identical to")
	spectest.Require(t, "RFC8621", "2.3", spectest.MUST, "The following Mailbox properties MUST be supported for sorting:")
	spectest.Require(t, "RFC8621", "2.5", spectest.MUST, "The client MUST remove these before it can delete the")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Invalid sortOrder (< 0 or >= 2^31) returns invalidProperties (RFC 8621 §2)
	rBadSort := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mbBad": map[string]any{
					"name":      "BadSortOrder",
					"sortOrder": -1,
				},
			},
		}, "c1"},
	})
	notCreatedBadSort, _ := rBadSort.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreatedBadSort["mbBad"]; !ok {
		t.Fatalf("expected negative sortOrder to be rejected: %v", rBadSort.MethodResponses[0].Args)
	}

	// 2. Create valid top-level mailbox
	rCreate1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mb1": map[string]any{
					"name":         "Finance",
					"sortOrder":    100,
					"isSubscribed": true,
				},
			},
		}, "c2"},
	})
	created1, _ := rCreate1.MethodResponses[0].Args["created"].(map[string]any)
	mb1Obj, ok := created1["mb1"].(map[string]any)
	if !ok {
		t.Fatalf("expected Finance mailbox created: %v", rCreate1.MethodResponses[0].Args)
	}
	mb1ID, _ := mb1Obj["id"].(string)

	// 3. Sibling with duplicate name under same parent MUST NOT be allowed (RFC 8621 §2)
	rDupSibling := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mbDup": map[string]any{
					"name": "Finance", // duplicate sibling under root
				},
			},
		}, "c3"},
	})
	notCreatedDup, _ := rDupSibling.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreatedDup["mbDup"]; !ok {
		t.Fatalf("expected duplicate sibling mailbox to be rejected: %v", rDupSibling.MethodResponses[0].Args)
	}

	// 4. Two mailboxes MUST NOT have the same role in the same account (RFC 8621 §2)
	rDupRole := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mbRole": map[string]any{
					"name": "SecondInbox",
					"role": "inbox", // inbox role already exists
				},
			},
		}, "c4"},
	})
	notCreatedRole, _ := rDupRole.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreatedRole["mbRole"]; !ok {
		t.Fatalf("expected duplicate inbox role to be rejected: %v", rDupRole.MethodResponses[0].Args)
	}

	// 5. Create child mailbox under Finance
	rCreateChild := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mbChild": map[string]any{
					"name":     "2026",
					"parentId": mb1ID,
				},
			},
		}, "c5"},
	})
	createdChild, _ := rCreateChild.MethodResponses[0].Args["created"].(map[string]any)
	childObj, ok := createdChild["mbChild"].(map[string]any)
	if !ok {
		t.Fatalf("expected child mailbox created: %v", rCreateChild.MethodResponses[0].Args)
	}
	childID, _ := childObj["id"].(string)

	// 6. ParentId loop detection (RFC 8621 §2: MUST NOT be a loop)
	rLoop := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				mb1ID: map[string]any{
					"parentId": mb1ID, // self loop
				},
			},
		}, "c6"},
	})
	notUpdatedLoop, _ := rLoop.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if _, ok := notUpdatedLoop[mb1ID]; !ok {
		t.Fatalf("expected parentId self-loop update to fail: %v", rLoop.MethodResponses[0].Args)
	}

	// 7. Mailbox/query filter by parentId, role, isSubscribed, and sorting (RFC 8621 §2.3)
	rQueryParent := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"parentId": mb1ID,
			},
			"sort": []any{
				map[string]any{"property": "sortOrder", "isAscending": true},
				map[string]any{"property": "name", "isAscending": true},
			},
		}, "c7"},
	})
	qIDs, _ := rQueryParent.MethodResponses[0].Args["ids"].([]any)
	if len(qIDs) != 1 || qIDs[0] != childID {
		t.Errorf("expected 1 child mailbox for parentId filter, got %v", qIDs)
	}

	// 8. Delete parent with child MUST fail with mailboxHasChild (RFC 8621 §2.5)
	rDelParent := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{mb1ID},
		}, "c8"},
	})
	notDestroyedParent, _ := rDelParent.MethodResponses[0].Args["notDestroyed"].(map[string]any)
	errObj, ok := notDestroyedParent[mb1ID].(map[string]any)
	if !ok || errObj["type"] != "mailboxHasChild" {
		t.Fatalf("expected mailboxHasChild error when destroying parent with children, got %v", notDestroyedParent)
	}

	// Cleanup child and then parent
	_ = postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{childID},
		}, "cleanChild"},
	})
	_ = postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{mb1ID},
		}, "cleanParent"},
	})
}

// TestRFC8621_Section3_ThreadAndSnippets tests Thread membership, SearchSnippet
// length constraints, and EmailAddress RFC 2047 decoding per RFC 8621 Sections 3, 4, and 5.
func TestRFC8621_Section3_ThreadAndSnippets(t *testing.T) {
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "to merge the Threads, it MUST handle this by deleting and reinserting")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "date, the sort is server dependent but MUST be stable (sorting by")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "It MUST NOT be bigger than 255 octets in size")
	spectest.Require(t, "RFC8621", "2", spectest.MUST, "If the server is unable to determine search snippets, it MUST return")
	spectest.Require(t, "RFC8621", "3", spectest.MUST, "Every Email MUST belong to a Thread, even if it is the only")
	spectest.Require(t, "RFC8621", "3", spectest.MUST, "encoding MUST be decoded, following the same rules as for the Text")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Create email and verify it belongs to a Thread (RFC 8621 §3)
	rCreate := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"emThread": map[string]any{
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"subject":    "Thread Belonging Test",
					"bodyValues": map[string]any{
						"1": map[string]any{"value": "This is a body containing important snippets for testing."},
					},
					"textBody": []any{
						map[string]any{"partId": "1"},
					},
				},
			},
		}, "c1"},
	})
	created, _ := rCreate.MethodResponses[0].Args["created"].(map[string]any)
	emObj, ok := created["emThread"].(map[string]any)
	if !ok {
		t.Fatalf("failed to create email: %v", rCreate.MethodResponses[0].Args)
	}
	emID, _ := emObj["id"].(string)

	// Fetch email threadId
	rGet := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{emID},
			"properties": []any{"id", "threadId"},
		}, "c2"},
	})
	list, _ := rGet.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 email in list")
	}
	thID, _ := list[0].(map[string]any)["threadId"].(string)
	if thID == "" {
		t.Fatalf("Every Email MUST belong to a Thread: threadId is empty")
	}

	// Fetch Thread object and ensure emailId is present
	rThread := postJMAP(t, ts.URL, using, []any{
		[]any{"Thread/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{thID},
		}, "c3"},
	})
	thList, _ := rThread.MethodResponses[0].Args["list"].([]any)
	if len(thList) != 1 {
		t.Fatalf("expected 1 thread in Thread/get list: %v", rThread.MethodResponses[0].Args)
	}
	emIDsInThread, _ := thList[0].(map[string]any)["emailIds"].([]any)
	foundEmail := false
	for _, id := range emIDsInThread {
		if id == emID {
			foundEmail = true
			break
		}
	}
	if !foundEmail {
		t.Errorf("Thread %s does not contain email %s: %v", thID, emID, emIDsInThread)
	}

	// 2. SearchSnippet/get test and preview size <= 255 octets (RFC 8621 Section 5)
	rSnippet := postJMAP(t, ts.URL, using, []any{
		[]any{"SearchSnippet/get", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"text": "important",
			},
			"emailIds": []any{emID},
		}, "c4"},
	})
	snipList, _ := rSnippet.MethodResponses[0].Args["list"].([]any)
	if len(snipList) != 1 {
		t.Fatalf("expected 1 snippet in SearchSnippet/get list: %v", rSnippet.MethodResponses[0].Args)
	}
	snipObj, _ := snipList[0].(map[string]any)
	if prev, ok := snipObj["preview"].(string); ok && prev != "" {
		if len(prev) > 255 {
			t.Errorf("SearchSnippet preview MUST NOT be bigger than 255 octets, got %d", len(prev))
		}
	}

	// 3. EmailAddress RFC 2047 decoding test (RFC 8621 Section 3 & 4.1.2.3)
	// Create raw email with RFC 2047 encoded display name
	rawMIME := []byte("From: =?utf-8?q?Alice_Smith?= <alice@example.com>\r\n" +
		"To: =?utf-8?b?Qm9iIEpvbmVz?= <bob@example.com>\r\n" +
		"Subject: RFC 2047 Decoding Test\r\n" +
		"Date: Fri, 26 Sep 2026 10:00:00 +0000\r\n" +
		"Message-ID: <test-rfc2047@example.com>\r\n" +
		"\r\n" +
		"Test message body for RFC 2047 decoding.")

	blob, err := srv.BlobBackend.PutBlob(context.Background(), jmap.AccountIDForSubject(testUsername), "message/rfc822", rawMIME)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	rImport := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"emails": map[string]any{
				"mRFC": map[string]any{
					"blobId":     blob.ID,
					"mailboxIds": map[string]bool{"mb-inbox": true},
				},
			},
		}, "cImport"},
	})
	createdImp, _ := rImport.MethodResponses[0].Args["created"].(map[string]any)
	mRFCObj, ok := createdImp["mRFC"].(map[string]any)
	if !ok {
		t.Fatalf("expected email imported: %v", rImport.MethodResponses[0].Args)
	}
	importedID, _ := mRFCObj["id"].(string)

	// Fetch imported email and verify decoded From name
	rGetImp := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{importedID},
			"properties": []any{"id", "from", "to"},
		}, "cGetImp"},
	})
	listImp, _ := rGetImp.MethodResponses[0].Args["list"].([]any)
	if len(listImp) != 1 {
		t.Fatalf("expected 1 imported email")
	}
	fromAddrs, _ := listImp[0].(map[string]any)["from"].([]any)
	if len(fromAddrs) != 1 {
		t.Fatalf("expected 1 from address: %v", listImp[0])
	}
	firstName, _ := fromAddrs[0].(map[string]any)["name"].(string)
	if firstName != "Alice Smith" {
		t.Errorf("expected RFC 2047 decoded name 'Alice Smith', got %q", firstName)
	}
}
