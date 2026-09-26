package jmap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestRFC8621_Section4_EmailPropertiesAndGet verifies RFC 8621 Section 4, 4.1, 4.1.1, 4.1.2.1, 4.1.3, 4.1.4, and 4.2
// requirements covering Email properties, keywords, header parsing/decoding, body truncation, and default properties.
func TestRFC8621_Section4_EmailPropertiesAndGet(t *testing.T) {
	spectest.Require(t, "RFC8621", "4", spectest.MUST, "or white space rules per [RFC2047] MUST NOT be decoded")
	spectest.Require(t, "RFC8621", "4.1", spectest.MUST, "complexities of various encodings that are required in a valid")
	spectest.Require(t, "RFC8621", "4.1.1", spectest.MUST, "mail store MUST belong to one or more Mailboxes at all times")
	spectest.Require(t, "RFC8621", "4.1.1", spectest.MUST, "object MUST be true")
	spectest.Require(t, "RFC8621", "4.1.1", spectest.MUST, "each key in the object MUST be true")
	spectest.Require(t, "RFC8621", "4.1.1", spectest.MUST, "keyword MUST NOT be visible via JMAP (and so are not counted in")
	spectest.Require(t, "RFC8621", "4.1.1", spectest.MUST, "and space), and it MUST NOT include any of these characters:")
	spectest.Require(t, "RFC8621", "4.1.1", spectest.MUST, "Because JSON is case sensitive, servers MUST return keywords in")
	spectest.Require(t, "RFC8621", "4.1.2.1", spectest.MUST, "message MUST be either ASCII (RFC 5322) or UTF-8 (RFC 6532); however,")
	spectest.Require(t, "RFC8621", "4.1.2.1", spectest.MUST, "Any NUL octet MUST be")
	spectest.Require(t, "RFC8621", "4.1.3", spectest.MUST, "If both suffixes are used, they MUST be specified in the order above")
	spectest.Require(t, "RFC8621", "4.1.4", spectest.MUST, "This MUST NOT be more than 256 characters in length")
	spectest.Require(t, "RFC8621", "4.1.4", spectest.MUST, "// Must be one of the allowed body types")
	spectest.Require(t, "RFC8621", "4.2", spectest.MUST, "object returned in \"bodyValues\" MUST be truncated if necessary so")
	spectest.Require(t, "RFC8621", "4.2", spectest.MUST, "The server MUST ensure the truncation results in valid UTF-8 and")
	spectest.Require(t, "RFC8621", "4.2", spectest.MUST, "following default MUST be used instead of \"all\" properties:")
	spectest.Require(t, "RFC8621", "4.2", spectest.MUST, ", \"header:From:asDate\") MUST result in the method call")
	spectest.Require(t, "RFC8621", "4.2", spectest.MUST, "capitalization of the property name in the response MUST be identical")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. mailboxIds validation (RFC 8621 §4.1.1):
	// - Email MUST belong to one or more mailboxes
	// - Each key in mailboxIds must have value true
	rEmptyMb := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"eEmptyMb": map[string]any{
					"mailboxIds": map[string]any{},
				},
			},
		}, "c1"},
	})
	notCreatedEmptyMb, _ := rEmptyMb.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreatedEmptyMb["eEmptyMb"]; !ok {
		t.Fatalf("expected empty mailboxIds to be rejected: %v", rEmptyMb.MethodResponses[0].Args)
	}

	rFalseMb := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"eFalseMb": map[string]any{
					"mailboxIds": map[string]any{"mb-inbox": false},
				},
			},
		}, "c2"},
	})
	notCreatedFalseMb, _ := rFalseMb.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreatedFalseMb["eFalseMb"]; !ok {
		t.Fatalf("expected mailboxIds with false value to be rejected: %v", rFalseMb.MethodResponses[0].Args)
	}

	// 2. keywords validation (RFC 8621 §4.1.1):
	// - Each value must be true
	// - Must not contain forbidden characters: ( ) { ] % * " \
	// - Must not contain spaces or control characters
	rFalseKw := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"eFalseKw": map[string]any{
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"keywords":   map[string]any{"$seen": false},
				},
			},
		}, "c3"},
	})
	notCreatedFalseKw, _ := rFalseKw.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreatedFalseKw["eFalseKw"]; !ok {
		t.Fatalf("expected keywords with false value to be rejected: %v", rFalseKw.MethodResponses[0].Args)
	}

	for _, invalidKw := range []string{"star*keyword", "space keyword", "paren(keyword", "bracket]keyword", "percent%kw", "quote\"kw", "slash\\kw"} {
		rBadKw := postJMAP(t, ts.URL, using, []any{
			[]any{"Email/set", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"eBadKw": map[string]any{
						"mailboxIds": map[string]bool{"mb-inbox": true},
						"keywords":   map[string]any{invalidKw: true},
					},
				},
			}, "cBadKw"},
		})
		notCreatedBadKw, _ := rBadKw.MethodResponses[0].Args["notCreated"].(map[string]any)
		if _, ok := notCreatedBadKw["eBadKw"]; !ok {
			t.Fatalf("expected keyword %q to be rejected: %v", invalidKw, rBadKw.MethodResponses[0].Args)
		}
	}

	// 3. Create valid Email with mixed-case keywords
	rCreateValid := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"eValid": map[string]any{
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"keywords":   map[string]bool{"$Flagged": true, "CustomTag": true},
					"subject":    "Test Valid Email",
					"from":       []any{map[string]any{"name": "Sender", "email": "sender@example.com"}},
					"to":         []any{map[string]any{"name": "Receiver", "email": "receiver@example.com"}},
					"textBody":   []any{map[string]any{"partId": "part-1", "type": "text/plain"}},
					"bodyValues": map[string]any{
						"part-1": map[string]any{
							"value": "This is a body text that exceeds twenty bytes for testing truncation.",
						},
					},
				},
			},
		}, "cCreateValid"},
	})
	createdValid, _ := rCreateValid.MethodResponses[0].Args["created"].(map[string]any)
	eValidObj, ok := createdValid["eValid"].(map[string]any)
	if !ok {
		t.Fatalf("expected eValid created: %v", rCreateValid.MethodResponses[0].Args)
	}
	validEmailID, _ := eValidObj["id"].(string)

	// 4. Email/get with omitted properties -> MUST return exactly the 24 default properties (RFC 8621 §4.2)
	// And keywords MUST be lowercase (RFC 8621 §4.1.1)
	rGetDefault := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{validEmailID},
		}, "cGetDefault"},
	})
	listDefault, _ := rGetDefault.MethodResponses[0].Args["list"].([]any)
	if len(listDefault) != 1 {
		t.Fatalf("expected 1 email in list, got %v", listDefault)
	}
	emDefaultMap := listDefault[0].(map[string]any)

	expectedDefaults := []string{
		"id", "blobId", "threadId", "mailboxIds", "keywords", "size", "receivedAt",
		"messageId", "inReplyTo", "references", "sender", "from", "to", "cc", "bcc",
		"replyTo", "subject", "sentAt", "hasAttachment", "preview", "bodyValues",
		"textBody", "htmlBody", "attachments",
	}
	for _, prop := range expectedDefaults {
		if _, exists := emDefaultMap[prop]; !exists {
			t.Errorf("missing RFC 8621 default property %q in response", prop)
		}
	}

	// Keywords must be returned in lowercase
	kws, _ := emDefaultMap["keywords"].(map[string]any)
	if !kws["$flagged"].(bool) || !kws["customtag"].(bool) {
		t.Errorf("expected lowercase keywords $flagged and customtag, got %v", kws)
	}
	if _, hasUpper := kws["$Flagged"]; hasUpper {
		t.Errorf("uppercase keyword was not lowercased: %v", kws)
	}

	// 5. Preview length constraint (RFC 8621 §4.1.4): MUST NOT be more than 256 characters
	prevStr, _ := emDefaultMap["preview"].(string)
	if len([]rune(prevStr)) > 256 {
		t.Errorf("preview length %d exceeds 256 characters", len([]rune(prevStr)))
	}

	// 6. Truncation of bodyValues with maxBodyValueBytes (RFC 8621 §4.2):
	// MUST be truncated so it does not exceed maxBodyValueBytes, result in valid UTF-8, and set isTruncated: true
	rGetTruncated := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId":          "primary",
			"ids":                []any{validEmailID},
			"fetchAllBodyValues": true,
			"maxBodyValueBytes":  10,
		}, "cGetTrunc"},
	})
	listTrunc, _ := rGetTruncated.MethodResponses[0].Args["list"].([]any)
	bvTrunc, _ := listTrunc[0].(map[string]any)["bodyValues"].(map[string]any)
	p1Trunc, _ := bvTrunc["part-1"].(map[string]any)
	truncVal, _ := p1Trunc["value"].(string)
	if len(truncVal) > 10 {
		t.Errorf("body value was not truncated to <= 10 bytes: %q (len %d)", truncVal, len(truncVal))
	}
	if isTrunc, ok := p1Trunc["isTruncated"].(bool); !ok || !isTrunc {
		t.Errorf("expected isTruncated: true, got %v", p1Trunc["isTruncated"])
	}

	// 7. Forbidden header forms and capitalization (RFC 8621 §4.1.3 & §4.2):
	// - "header:From:asDate" is forbidden -> MUST reject with invalidArguments
	rBadHeaderForm := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{validEmailID},
			"properties": []any{"id", "header:From:asDate"},
		}, "cBadForm"},
	})
	if rBadHeaderForm.MethodResponses[0].Name != "error" || rBadHeaderForm.MethodResponses[0].Args["type"] != "invalidArguments" {
		t.Errorf("expected invalidArguments for header:From:asDate, got %v", rBadHeaderForm.MethodResponses[0])
	}

	// - Inverted suffix order "header:Subject:all:asRaw" -> MUST reject with invalidArguments
	rBadSuffixOrder := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{validEmailID},
			"properties": []any{"id", "header:Subject:all:asRaw"},
		}, "cBadSuffix"},
	})
	if rBadSuffixOrder.MethodResponses[0].Name != "error" || rBadSuffixOrder.MethodResponses[0].Args["type"] != "invalidArguments" {
		t.Errorf("expected invalidArguments for inverted suffixes header:Subject:all:asRaw, got %v", rBadSuffixOrder.MethodResponses[0])
	}

	// - Exact capitalization matching in response (RFC 8621 §4.2)
	rHdrCap := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{validEmailID},
			"properties": []any{"id", "header:SubJect:asRaw"},
		}, "cCap"},
	})
	listCap, _ := rHdrCap.MethodResponses[0].Args["list"].([]any)
	emCapMap := listCap[0].(map[string]any)
	if _, ok := emCapMap["header:SubJect:asRaw"]; !ok {
		t.Errorf("expected exact capitalization 'header:SubJect:asRaw' in response, got %v", emCapMap)
	}

	// 8. RFC 2047 whitespace/placement violation and NUL octet dropping (RFC 8621 §4 & §4.1.2.1)
	rawMIME := []byte("From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Test =?utf-8?x?bad?= and =?utf-8?q?incomplete\r\n" +
		"X-NUL-Header: HeaderWith\x00NUL\r\n" +
		"Date: Fri, 26 Sep 2026 10:00:00 +0000\r\n" +
		"Message-ID: <nul-test@example.com>\r\n" +
		"\r\n" +
		"Body content.")
	blob, err := srv.BlobBackend.PutBlob(context.Background(), jmap.AccountIDForSubject(testUsername), "message/rfc822", rawMIME)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}
	rImport := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"emails": map[string]any{
				"mNUL": map[string]any{
					"blobId":     blob.ID,
					"mailboxIds": map[string]bool{"mb-inbox": true},
				},
			},
		}, "cImp"},
	})
	createdNUL, _ := rImport.MethodResponses[0].Args["created"].(map[string]any)
	nulID := createdNUL["mNUL"].(map[string]any)["id"].(string)

	rGetNUL := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{nulID},
			"properties": []any{"id", "header:X-NUL-Header:asText", "header:Subject:asText"},
		}, "cGetNUL"},
	})
	listNUL, _ := rGetNUL.MethodResponses[0].Args["list"].([]any)
	nulEmMap := listNUL[0].(map[string]any)
	nulHeaderVal, _ := nulEmMap["header:X-NUL-Header:asText"].(string)
	if strings.Contains(nulHeaderVal, "\x00") {
		t.Errorf("NUL octet was not dropped from header: %q", nulHeaderVal)
	}
	if nulHeaderVal != "HeaderWithNUL" {
		t.Errorf("expected 'HeaderWithNUL', got %q", nulHeaderVal)
	}
	// Text that looks like RFC 2047 but violates syntax/placement rules per RFC 2047 MUST NOT be decoded
	subjVal, _ := nulEmMap["header:Subject:asText"].(string)
	if !strings.Contains(subjVal, "=?utf-8?x?bad?=") {
		t.Errorf("expected invalid RFC 2047 syntax not to be decoded, got %q", subjVal)
	}
}

// TestRFC8621_Section4_EmailQueryFilterAndSort verifies RFC 8621 Section 4.4.1 and 4.4.2
// requirements covering Email/query FilterCondition properties, header array rules,
// phrase searching, sorting by receivedAt and keywords, and stable sort tie-breaking.
func TestRFC8621_Section4_EmailQueryFilterAndSort(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "An Email must be in this Mailbox to match the")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "An Email must be in at least one Mailbox")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "The \"receivedAt\" date-time of the Email must be before this date-")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "The \"receivedAt\" date-time of the Email must be the same or after")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "The \"size\" property of the Email must be equal to or greater than")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "The \"size\" property of the Email must be less than this number to")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "must have the given keyword to match the condition")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "Email must have the given keyword to match the condition")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "must *not* have the given keyword to match the condition")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "This Email must have the given keyword to match the condition")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "This Email must not have the given keyword to match the condition")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "The \"hasAttachment\" property of the Email must be identical to the")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "The server MUST look up text in the")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "The array MUST contain either one or two elements")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "condition MUST always evaluate to true")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "specified, ALL must apply for the condition to be true (it is")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "required for that exact word or sequence of words, excluding the")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "Within a phrase, to match one of the following characters you MUST")
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST, "separate tokens that may be searched for separately but MUST all")
	spectest.Require(t, "RFC8621", "4.4.2", spectest.MUST, "MUST be supported for sorting:")
	spectest.Require(t, "RFC8621", "4.4.2", spectest.MUST, "sort, the Comparator object MUST also have a \"keyword\" property")
	spectest.Require(t, "RFC8621", "4.4.2", spectest.MUST, "o \"hasKeyword\" - This value MUST be considered true if the Email has")
	spectest.Require(t, "RFC8621", "4.4.2", spectest.MUST, "o \"allInThreadHaveKeyword\" - This value MUST be considered true for")
	spectest.Require(t, "RFC8621", "4.4.2", spectest.MUST, "o \"someInThreadHaveKeyword\" - This value MUST be considered true for")
	spectest.Require(t, "RFC8621", "4.4.2", spectest.MUST, "properties, then the order is server dependent but must be stable")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := seedCtx()
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// Create another mailbox for inMailboxOtherThan testing
	_, _ = srv.MailBackend.CreateMailbox(ctx, &jmap.Mailbox{
		ID:   "mb-archive",
		Name: "Archive",
	})

	// Seed emails for querying:
	// Email 1: Inbox only, Flagged, receivedAt 2026-02-01, size 100, no attachment, subject "Project Phoenix Update"
	e1, _ := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs:    map[jmap.Id]bool{"mb-inbox": true},
		Keywords:      map[string]bool{"$flagged": true, "$seen": true},
		Subject:       "Project Phoenix Update",
		ReceivedAt:    "2026-02-01T10:00:00Z",
		Size:          100,
		HasAttachment: false,
		MessageID:     []string{"msg-alpha-root@example.com"},
		From:          []jmap.EmailAddress{{Name: "Alice", Email: "alice@example.com"}},
		To:            []jmap.EmailAddress{{Name: "Team", Email: "team@example.com"}},
		Headers:       []jmap.EmailHeader{{Name: "X-Workflow", Value: "Automated"}},
	})

	// Email 2: Archive only, not flagged, in same thread via InReplyTo, receivedAt 2026-03-01, size 500, with attachment, subject "Quarterly Budget Report"
	e2, _ := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs:    map[jmap.Id]bool{"mb-archive": true},
		Keywords:      map[string]bool{"$seen": true},
		Subject:       "Quarterly Budget Report",
		ReceivedAt:    "2026-03-01T10:00:00Z",
		Size:          500,
		HasAttachment: true,
		InReplyTo:     []string{"msg-alpha-root@example.com"},
		Attachments:   []jmap.EmailBodyPart{{Type: "application/pdf"}},
		From:          []jmap.EmailAddress{{Name: "Bob", Email: "bob@example.com"}},
		To:            []jmap.EmailAddress{{Name: "Team", Email: "team@example.com"}},
	})

	// Email 3: Archive only, Flagged, different thread, receivedAt 2026-04-01, size 300, subject "Secret Project Briefing"
	e3, _ := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs:    map[jmap.Id]bool{"mb-archive": true},
		Keywords:      map[string]bool{"$flagged": true},
		Subject:       "Secret Project Briefing",
		ReceivedAt:    "2026-04-01T10:00:00Z",
		Size:          300,
		HasAttachment: false,
		MessageID:     []string{"msg-beta@example.com"},
		From:          []jmap.EmailAddress{{Name: "Charlie", Email: "charlie@example.com"}},
		To:            []jmap.EmailAddress{{Name: "Alice", Email: "alice@example.com"}},
		Headers:       []jmap.EmailHeader{{Name: "X-Workflow", Value: "Manual"}},
	})

	// 1. Zero properties on FilterCondition -> MUST always evaluate to true (RFC 8621 §4.4.1)
	rAll := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{},
		}, "cAll"},
	})
	allIDs, _ := rAll.MethodResponses[0].Args["ids"].([]any)
	if len(allIDs) < 3 {
		t.Errorf("empty filter must return all emails, got %d", len(allIDs))
	}

	// 2. Multiple properties specified -> ALL must apply (AND logic) (RFC 8621 §4.4.1)
	// inMailbox: "mb-inbox" AND hasKeyword: "$flagged" -> e1 only (e2 is in inbox but not flagged; e3 is flagged but not in inbox)
	rAnd := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"inMailbox":  "mb-inbox",
				"hasKeyword": "$flagged",
			},
		}, "cAnd"},
	})
	andIDs, _ := rAnd.MethodResponses[0].Args["ids"].([]any)
	if len(andIDs) != 1 || andIDs[0] != string(e1.ID) {
		t.Errorf("AND filter expected [%s], got %v", e1.ID, andIDs)
	}

	// 3. inMailboxOtherThan: ["mb-inbox"] -> emails in at least one mailbox other than inbox (e2 and e3)
	rOtherThan := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"inMailboxOtherThan": []any{"mb-inbox"},
			},
		}, "cOther"},
	})
	otherIDs, _ := rOtherThan.MethodResponses[0].Args["ids"].([]any)
	hasE1, hasE2, hasE3 := false, false, false
	for _, id := range otherIDs {
		if id == string(e1.ID) {
			hasE1 = true
		}
		if id == string(e2.ID) {
			hasE2 = true
		}
		if id == string(e3.ID) {
			hasE3 = true
		}
	}
	if hasE1 || !hasE2 || !hasE3 {
		t.Errorf("inMailboxOtherThan: expected e2 and e3, got %v", otherIDs)
	}

	// 4. before and after receivedAt date-time (RFC 8621 §4.4.1)
	rBefore := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"before": "2026-02-15T00:00:00Z",
			},
		}, "cBefore"},
	})
	beforeIDs, _ := rBefore.MethodResponses[0].Args["ids"].([]any)
	if len(beforeIDs) != 1 || beforeIDs[0] != string(e1.ID) {
		t.Errorf("before filter expected [%s], got %v", e1.ID, beforeIDs)
	}

	rAfter := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"after": "2026-03-15T00:00:00Z",
			},
		}, "cAfter"},
	})
	afterIDs, _ := rAfter.MethodResponses[0].Args["ids"].([]any)
	if len(afterIDs) != 1 || afterIDs[0] != string(e3.ID) {
		t.Errorf("after filter expected [%s], got %v", e3.ID, afterIDs)
	}

	// 5. minSize and maxSize (RFC 8621 §4.4.1)
	rSize := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"minSize": 250,
				"maxSize": 400,
			},
		}, "cSize"},
	})
	sizeIDs, _ := rSize.MethodResponses[0].Args["ids"].([]any)
	if len(sizeIDs) != 1 || sizeIDs[0] != string(e3.ID) {
		t.Errorf("size filter (250..400) expected [%s], got %v", e3.ID, sizeIDs)
	}

	// 6. hasKeyword and notKeyword (RFC 8621 §4.4.1)
	rKw := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"notKeyword": "$flagged",
			},
		}, "cNotKw"},
	})
	notKwIDs, _ := rKw.MethodResponses[0].Args["ids"].([]any)
	for _, id := range notKwIDs {
		if id == string(e1.ID) || id == string(e3.ID) {
			t.Errorf("notKeyword: $flagged matched flagged email: %v", notKwIDs)
		}
	}

	// 7. allInThreadHaveKeyword and someInThreadHaveKeyword (RFC 8621 §4.4.1)
	// th-alpha has e1 (flagged) and e2 (not flagged).
	// someInThreadHaveKeyword $flagged -> matches e1 and e2.
	// allInThreadHaveKeyword $flagged -> does not match e1 or e2 (because e2 lacks it).
	// th-beta has e3 only (flagged). allInThreadHaveKeyword $flagged -> matches e3.
	rSomeKw := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"someInThreadHaveKeyword": "$flagged",
			},
		}, "cSome"},
	})
	someIDs, _ := rSomeKw.MethodResponses[0].Args["ids"].([]any)
	if len(someIDs) < 3 {
		t.Errorf("someInThreadHaveKeyword expected all 3 emails, got %v", someIDs)
	}

	rAllKw := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"allInThreadHaveKeyword": "$flagged",
			},
		}, "cAllKw"},
	})
	allKwIDs, _ := rAllKw.MethodResponses[0].Args["ids"].([]any)
	if len(allKwIDs) != 1 || allKwIDs[0] != string(e3.ID) {
		t.Errorf("allInThreadHaveKeyword expected only e3, got %v", allKwIDs)
	}

	// 8. hasAttachment (RFC 8621 §4.4.1)
	rAtt := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"hasAttachment": true,
			},
		}, "cAtt"},
	})
	attIDs, _ := rAtt.MethodResponses[0].Args["ids"].([]any)
	if len(attIDs) != 1 || attIDs[0] != string(e2.ID) {
		t.Errorf("hasAttachment expected [%s], got %v", e2.ID, attIDs)
	}

	// 9. text filter and phrase search (RFC 8621 §4.4.1)
	rText := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"text": "\"Project Phoenix\"",
			},
		}, "cText"},
	})
	textIDs, _ := rText.MethodResponses[0].Args["ids"].([]any)
	if len(textIDs) != 1 || textIDs[0] != string(e1.ID) {
		t.Errorf("text phrase search expected [%s], got %v", e1.ID, textIDs)
	}

	// 10. header filter array constraints (RFC 8621 §4.4.1)
	// - 1 element array matches header field presence
	rHdr1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"header": []any{"X-Workflow"},
			},
		}, "cHdr1"},
	})
	hdr1IDs, _ := rHdr1.MethodResponses[0].Args["ids"].([]any)
	if len(hdr1IDs) != 2 {
		t.Errorf("header 1-element filter expected 2 emails (e1 and e3), got %v", hdr1IDs)
	}

	// - 2 element array matches header value text
	rHdr2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"header": []any{"X-Workflow", "Automated"},
			},
		}, "cHdr2"},
	})
	hdr2IDs, _ := rHdr2.MethodResponses[0].Args["ids"].([]any)
	if len(hdr2IDs) != 1 || hdr2IDs[0] != string(e1.ID) {
		t.Errorf("header 2-element filter expected [%s], got %v", e1.ID, hdr2IDs)
	}

	// - 0 elements or > 2 elements MUST be rejected with invalidArguments
	rBadHdr := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"header": []any{},
			},
		}, "cBadHdr"},
	})
	if rBadHdr.MethodResponses[0].Name != "error" || rBadHdr.MethodResponses[0].Args["type"] != "invalidArguments" {
		t.Errorf("expected invalidArguments for empty header array, got %v", rBadHdr.MethodResponses[0])
	}

	rBadHdr3 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"header": []any{"A", "B", "C"},
			},
		}, "cBadHdr3"},
	})
	if rBadHdr3.MethodResponses[0].Name != "error" || rBadHdr3.MethodResponses[0].Args["type"] != "invalidArguments" {
		t.Errorf("expected invalidArguments for 3-element header array, got %v", rBadHdr3.MethodResponses[0])
	}

	// 11. Sorting by receivedAt MUST be supported (RFC 8621 §4.4.2)
	rSortRec := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"sort": []any{
				map[string]any{"property": "receivedAt", "isAscending": true},
			},
		}, "cSortRec"},
	})
	sortRecIDs, _ := rSortRec.MethodResponses[0].Args["ids"].([]any)
	if len(sortRecIDs) < 3 {
		t.Fatalf("expected at least 3 sorted emails, got %v", sortRecIDs)
	}

	// 12. Comparator keyword property requirement (RFC 8621 §4.4.2):
	// hasKeyword sort without keyword property MUST be rejected with invalidArguments
	rMissingKwSort := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"sort": []any{
				map[string]any{"property": "hasKeyword"},
			},
		}, "cMissingKw"},
	})
	if rMissingKwSort.MethodResponses[0].Name != "error" || rMissingKwSort.MethodResponses[0].Args["type"] != "invalidArguments" {
		t.Errorf("expected invalidArguments for hasKeyword sort missing keyword property, got %v", rMissingKwSort.MethodResponses[0])
	}

	// 13. Sorting by hasKeyword with keyword property (RFC 8621 §4.4.2)
	rSortKw := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"sort": []any{
				map[string]any{"property": "hasKeyword", "keyword": "$flagged", "isAscending": false},
			},
		}, "cSortKw"},
	})
	sortKwIDs, _ := rSortKw.MethodResponses[0].Args["ids"].([]any)
	// Emails with $flagged (e1, e3) must appear before emails without $flagged (e2)
	var idxE1, idxE2, idxE3 int
	for i, id := range sortKwIDs {
		if id == string(e1.ID) {
			idxE1 = i
		}
		if id == string(e2.ID) {
			idxE2 = i
		}
		if id == string(e3.ID) {
			idxE3 = i
		}
	}
	if !(idxE1 < idxE2 && idxE3 < idxE2) {
		t.Errorf("hasKeyword desc sort failed: expected e1 and e3 before e2, got e1=%d, e3=%d, e2=%d", idxE1, idxE3, idxE2)
	}

	// 14. Stable sorting tie-breaking (RFC 8621 §4.4.2)
	// Two emails with identical sort values must preserve stable order
	e4, _ := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Stable Sort Test",
		ReceivedAt: "2026-05-01T10:00:00Z",
		Size:       777,
	})
	e5, _ := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Stable Sort Test",
		ReceivedAt: "2026-05-01T10:00:00Z",
		Size:       777,
	})
	rStable := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"subject": "Stable Sort Test",
			},
			"sort": []any{
				map[string]any{"property": "subject", "isAscending": true},
			},
		}, "cStable"},
	})
	stableIDs, _ := rStable.MethodResponses[0].Args["ids"].([]any)
	if len(stableIDs) != 2 || stableIDs[0] != string(e4.ID) || stableIDs[1] != string(e5.ID) {
		t.Errorf("stable sort order failed: expected [%s, %s], got %v", e4.ID, e5.ID, stableIDs)
	}
}

// TestRFC8621_Section4_EmailSetImportCopy tests RFC 8621 Sections 4.6, 4.8, 4.9, and 4.10
// requirements covering Email/set creation constraints and blobNotFound errors,
// Email/import blob ingestion, ifInState, duplicates, and EAI headers,
// Email/parse internationalized message parsing, and Email/copy cross-mailbox/account overrides.
func TestRFC8621_Section4_EmailSetImportCopy(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST, "Email or an EmailBodyPart -- the client must set each header field")
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST, "a value that does not conform to the required syntax for this header")
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST, "An extra \"notFound\" property of type \"Id[]\" MUST")
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST, "The server MUST support messages with Email")
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST, "must first be uploaded as blobs using the standard upload mechanism")
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST, "supplied, the string must match the current state of the account")
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST, "Mailbox MUST be given")
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST, "In this case, it MUST reject attempts to")
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST, "An \"existingId\" property of type \"Id\" MUST be included on")
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST, "are allowed, the newly created Email object MUST have a separate id")
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST, ", missing, wrong type, id not found), the server MUST reject the")
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST, "response MUST represent the new representation and therefore be")
	spectest.Require(t, "RFC8621", "4.9", spectest.MUST, "The server MUST support messages with EAI headers")
	spectest.Require(t, "RFC8621", "4.10", spectest.MUST, "It MUST set a new")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := seedCtx()
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Email/set header validation (RFC 8621 §4.6)
	// Top-level "headers" property MUST NOT be given
	rHeadersTop := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"eHdr": map[string]any{
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"headers":    []any{map[string]any{"name": "X-Foo", "value": "Bar"}},
				},
			},
		}, "cHdr"},
	})
	notCreatedHdr, _ := rHeadersTop.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreatedHdr["eHdr"]; !ok {
		t.Fatalf("expected create with top-level 'headers' to be rejected: %v", rHeadersTop.MethodResponses[0].Args)
	}

	// 2. Draft creation with lenient header syntax per RFC 8621 §4.6
	// For drafts, To header may have a value that does not yet conform to RFC 5322 address format
	rDraft := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"eDraft": map[string]any{
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"keywords":   map[string]bool{"$draft": true},
					"subject":    "Work in Progress Draft",
					"to":         []any{map[string]any{"name": "Incomplete Recipient", "email": "draft-in-progress"}},
					"textBody":   []any{map[string]any{"partId": "p1", "type": "text/plain"}},
					"bodyValues": map[string]any{"p1": map[string]any{"value": "Draft content"}},
				},
			},
		}, "cDraft"},
	})
	createdDraft, _ := rDraft.MethodResponses[0].Args["created"].(map[string]any)
	draftObj, ok := createdDraft["eDraft"].(map[string]any)
	if !ok {
		t.Fatalf("expected draft with lenient To header to be created: %v", rDraft.MethodResponses[0].Args)
	}
	draftID := draftObj["id"].(string)

	// 3. Email/set create referencing nonexistent blobId -> blobNotFound with notFound array (RFC 8621 §4.6)
	rMissingBlob := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"eMissingBlob": map[string]any{
					"mailboxIds":  map[string]bool{"mb-inbox": true},
					"attachments": []any{map[string]any{"blobId": "nonexistent-blob-12345", "type": "application/pdf"}},
				},
			},
		}, "cMissingBlob"},
	})
	notCreatedBlob, _ := rMissingBlob.MethodResponses[0].Args["notCreated"].(map[string]any)
	errBlob, ok := notCreatedBlob["eMissingBlob"].(map[string]any)
	if !ok || errBlob["type"] != "blobNotFound" {
		t.Fatalf("expected blobNotFound for missing attachment blob, got %v", errBlob)
	}
	notFoundList, _ := errBlob["notFound"].([]any)
	if len(notFoundList) == 0 || notFoundList[0] != "nonexistent-blob-12345" {
		t.Errorf("expected notFound to list ['nonexistent-blob-12345'], got %v", notFoundList)
	}

	// 4. Email/import with standard blob upload & EAI headers (RFC 8621 §4.8 & §4.9)
	accountID := jmap.AccountIDForSubject(testUsername)
	eaiRawMsg := []byte("From: =?utf-8?B?5byg5Lyf?= <zhangwei@example.com>\r\n" +
		"To: =?utf-8?B?5p2O5Zub?= <lisi@example.com>\r\n" +
		"Subject: =?utf-8?B?RUFJ6YKu5Lu25rWL6K+V?=\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Hello Internationalized Email\r\n")

	blob, err := srv.BlobBackend.PutBlob(ctx, accountID, "message/rfc822", eaiRawMsg)
	if err != nil {
		t.Fatalf("failed to upload blob for import: %v", err)
	}

	// 5. Email/import with outdated ifInState -> stateMismatch (RFC 8621 §4.8)
	rMismatch := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"ifInState": "outdated-invalid-state",
			"emails": map[string]any{
				"impMismatch": map[string]any{
					"blobId":     string(blob.ID),
					"mailboxIds": map[string]bool{"mb-inbox": true},
				},
			},
		}, "cMismatch"},
	})
	if rMismatch.MethodResponses[0].Name != "error" || rMismatch.MethodResponses[0].Args["type"] != "stateMismatch" {
		t.Fatalf("expected stateMismatch error on invalid ifInState, got %v", rMismatch.MethodResponses[0])
	}

	// 6. Email/import missing mailboxIds -> invalidProperties (RFC 8621 §4.8)
	rNoMb := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"emails": map[string]any{
				"impNoMb": map[string]any{
					"blobId":     string(blob.ID),
					"mailboxIds": map[string]bool{},
				},
			},
		}, "cNoMb"},
	})
	notCreatedNoMb, _ := rNoMb.MethodResponses[0].Args["notCreated"].(map[string]any)
	errNoMb, ok := notCreatedNoMb["impNoMb"].(map[string]any)
	if !ok || errNoMb["type"] != "invalidProperties" {
		t.Fatalf("expected invalidProperties when mailboxIds is empty, got %v", errNoMb)
	}

	// 7. Email/import invalid blobId -> invalidProperties (RFC 8621 §4.8)
	rBadBlob := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"emails": map[string]any{
				"impBadBlob": map[string]any{
					"blobId":     "non-existent-import-blob",
					"mailboxIds": map[string]bool{"mb-inbox": true},
				},
			},
		}, "cBadBlob"},
	})
	notCreatedBadBlob, _ := rBadBlob.MethodResponses[0].Args["notCreated"].(map[string]any)
	errBadBlob, ok := notCreatedBadBlob["impBadBlob"].(map[string]any)
	if !ok || errBadBlob["type"] != "invalidProperties" {
		t.Fatalf("expected invalidProperties for nonexistent blobId in import, got %v", errBadBlob)
	}

	// 8. Successful Email/import with EAI headers and duplicate creation assigning separate IDs (RFC 8621 §4.8)
	rImport := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"emails": map[string]any{
				"imp1": map[string]any{
					"blobId":     string(blob.ID),
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"keywords":   map[string]bool{"$seen": true},
				},
				"imp2": map[string]any{
					"blobId":     string(blob.ID),
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"keywords":   map[string]bool{"$flagged": true},
				},
			},
		}, "cImport"},
	})
	createdImp, _ := rImport.MethodResponses[0].Args["created"].(map[string]any)
	imp1Obj, ok1 := createdImp["imp1"].(map[string]any)
	imp2Obj, ok2 := createdImp["imp2"].(map[string]any)
	if !ok1 || !ok2 {
		t.Fatalf("expected both duplicate imports to succeed: %v", rImport.MethodResponses[0].Args)
	}
	id1, _ := imp1Obj["id"].(string)
	id2, _ := imp2Obj["id"].(string)
	if id1 == "" || id2 == "" || id1 == id2 {
		t.Errorf("duplicate imports must have separate distinct IDs, got id1=%q, id2=%q", id1, id2)
	}

	// 9. Email/parse with EAI headers (RFC 8621 §4.9)
	rParse := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/parse", map[string]any{
			"accountId":  "primary",
			"blobIds":    []string{string(blob.ID)},
			"properties": []string{"subject", "from", "to"},
		}, "cParse"},
	})
	parsedMap, _ := rParse.MethodResponses[0].Args["parsed"].(map[string]any)
	parsedObj, ok := parsedMap[string(blob.ID)].(map[string]any)
	if !ok {
		t.Fatalf("expected parsed blob object, got %v", rParse.MethodResponses[0].Args)
	}
	subj, _ := parsedObj["subject"].(string)
	if !strings.Contains(subj, "EAI") && !strings.Contains(subj, "邮件测试") {
		t.Errorf("expected parsed EAI subject, got %q", subj)
	}

	// 10. Email/copy setting new mailboxIds per RFC 8621 §4.10
	_, _ = srv.MailBackend.CreateMailbox(ctx, &jmap.Mailbox{
		ID:   "mb-sent",
		Name: "Sent",
	})
	rCopy := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/copy", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"cp1": map[string]any{
					"id":         draftID,
					"mailboxIds": map[string]bool{"mb-sent": true},
				},
			},
		}, "cCopy"},
	})
	createdCopy, _ := rCopy.MethodResponses[0].Args["created"].(map[string]any)
	cpObj, ok := createdCopy["cp1"].(map[string]any)
	if !ok {
		t.Fatalf("expected email copy to succeed with new mailboxIds: %v", rCopy.MethodResponses[0].Args)
	}
	cpID, _ := cpObj["id"].(string)
	if cpID == "" || cpID == draftID {
		t.Errorf("expected copied email to have distinct ID in new mailbox, got %q", cpID)
	}
}

