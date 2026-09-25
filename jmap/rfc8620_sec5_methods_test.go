package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8620_Section5_1_InvalidPropertiesRejected verifies that if an invalid property
// is requested in a /get method call, the call MUST be rejected with an "invalidArguments" error.
func TestRFC8620_Section5_1_InvalidPropertiesRejected(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.1", spectest.MUST, "requested, the call MUST be rejected with an \"invalidArguments\"")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Mailbox/get with invalid property
	r1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/get", map[string]any{
			"accountId":  "primary",
			"properties": []any{"id", "notAValidMailboxProperty"},
		}, "c1"},
	})
	if len(r1.MethodResponses) != 1 || r1.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error response for invalid property in Mailbox/get, got: %v", r1.MethodResponses)
	}
	errType, _ := r1.MethodResponses[0].Args["type"].(string)
	if errType != "invalidArguments" {
		t.Errorf("expected invalidArguments error, got: %q", errType)
	}

	// 2. Thread/get with invalid property
	r2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Thread/get", map[string]any{
			"accountId":  "primary",
			"properties": []any{"id", "nonExistentThreadProp"},
		}, "c2"},
	})
	if len(r2.MethodResponses) != 1 || r2.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error response for invalid property in Thread/get, got: %v", r2.MethodResponses)
	}
	errType2, _ := r2.MethodResponses[0].Args["type"].(string)
	if errType2 != "invalidArguments" {
		t.Errorf("expected invalidArguments error, got: %q", errType2)
	}
}

// TestRFC8620_Section5_1_StateChangesOnDataChange verifies that if object data changes,
// the returned state string MUST change.
func TestRFC8620_Section5_1_StateChangesOnDataChange(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.1", spectest.MUST, "MUST change")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Initial state
	r1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c1"},
	})
	state1, _ := r1.MethodResponses[0].Args["state"].(string)
	if state1 == "" {
		t.Fatalf("expected non-empty initial state")
	}

	// 2. Create mailbox -> state must change
	r2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mb1": map[string]any{"name": "StateTestFolder"},
			},
		}, "c2"},
	})
	newState2, _ := r2.MethodResponses[0].Args["newState"].(string)
	if newState2 == state1 {
		t.Errorf("state MUST change after create: state1=%q, newState2=%q", state1, newState2)
	}

	created, _ := r2.MethodResponses[0].Args["created"].(map[string]any)
	mbObj, ok := created["mb1"].(map[string]any)
	if !ok {
		t.Fatalf("failed to create mb1: %v", r2.MethodResponses[0].Args)
	}
	mbID, _ := mbObj["id"].(string)

	// 3. Update mailbox -> state must change
	r3 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				mbID: map[string]any{"name": "StateTestFolderRenamed"},
			},
		}, "c3"},
	})
	newState3, _ := r3.MethodResponses[0].Args["newState"].(string)
	if newState3 == newState2 {
		t.Errorf("state MUST change after update: newState2=%q, newState3=%q", newState2, newState3)
	}

	// 4. Destroy mailbox -> state must change
	r4 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{mbID},
		}, "c4"},
	})
	newState4, _ := r4.MethodResponses[0].Args["newState"].(string)
	if newState4 == newState3 {
		t.Errorf("state MUST change after destroy: newState3=%q, newState4=%q", newState3, newState4)
	}
}

// TestRFC8620_Section5_1_DuplicateIdsReturnedOnce verifies that if an id is included more
// than once in the request, the server MUST only return it once in list or notFound.
func TestRFC8620_Section5_1_DuplicateIdsReturnedOnce(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.1", spectest.MUST, "included more than once in the request, the server MUST only")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// Create a mailbox
	rCreate := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mb1": map[string]any{"name": "DedupFolder"},
			},
		}, "c0"},
	})
	created, _ := rCreate.MethodResponses[0].Args["created"].(map[string]any)
	mbID, _ := created["mb1"].(map[string]any)["id"].(string)

	// Fetch with duplicate existing ID and duplicate nonexistent ID
	rGet := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{mbID, mbID, mbID, "missing-xyz", "missing-xyz"},
		}, "c1"},
	})

	args := rGet.MethodResponses[0].Args
	list, _ := args["list"].([]any)
	notFound, _ := args["notFound"].([]any)

	if len(list) != 1 {
		t.Errorf("expected duplicate existing ID to be returned exactly once in list, got %d items: %v", len(list), list)
	}
	if len(notFound) != 1 {
		t.Errorf("expected duplicate missing ID to be returned exactly once in notFound, got %d items: %v", len(notFound), notFound)
	}
}

// TestRFC8620_Section5_2_MaxChangesValidationAndEnforcement verifies maxChanges parameter
// requirements per RFC 8620 Section 5.2.
func TestRFC8620_Section5_2_MaxChangesValidationAndEnforcement(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.2", spectest.MUST, "MAY choose to return fewer than this value but MUST NOT return")
	spectest.Require(t, "RFC8620", "5.2", spectest.MUST, "If supplied by the client, the value MUST be a")
	spectest.Require(t, "RFC8620", "5.2", spectest.MUST, "is given, the server MUST reject the call with an")
	spectest.Require(t, "RFC8620", "5.2", spectest.MUST, "the server MUST ensure the number of ids returned across \"created\",")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Initial Mailbox/get state
	rGet := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c0"},
	})
	initialState, _ := rGet.MethodResponses[0].Args["state"].(string)

	// 2. Reject maxChanges = 0 with invalidArguments
	rZero := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": initialState,
			"maxChanges": 0,
		}, "c1"},
	})
	if len(rZero.MethodResponses) != 1 || rZero.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for maxChanges=0, got %v", rZero.MethodResponses)
	}
	if errType, _ := rZero.MethodResponses[0].Args["type"].(string); errType != "invalidArguments" {
		t.Errorf("expected invalidArguments for maxChanges=0, got %q", errType)
	}

	// 3. Reject negative maxChanges with invalidArguments
	rNeg := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": initialState,
			"maxChanges": -5,
		}, "c2"},
	})
	if len(rNeg.MethodResponses) != 1 || rNeg.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for negative maxChanges, got %v", rNeg.MethodResponses)
	}
	if errType, _ := rNeg.MethodResponses[0].Args["type"].(string); errType != "invalidArguments" {
		t.Errorf("expected invalidArguments for negative maxChanges, got %q", errType)
	}

	// 4. Reject non-integer float maxChanges with invalidArguments
	rFloat := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": initialState,
			"maxChanges": 2.5,
		}, "c3"},
	})
	if len(rFloat.MethodResponses) != 1 || rFloat.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for fractional maxChanges, got %v", rFloat.MethodResponses)
	}
	if errType, _ := rFloat.MethodResponses[0].Args["type"].(string); errType != "invalidArguments" {
		t.Errorf("expected invalidArguments for fractional maxChanges, got %q", errType)
	}

	// 5. Reject string maxChanges with invalidArguments
	rStr := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": initialState,
			"maxChanges": "five",
		}, "c4"},
	})
	if len(rStr.MethodResponses) != 1 || rStr.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for string maxChanges, got %v", rStr.MethodResponses)
	}
	if errType, _ := rStr.MethodResponses[0].Args["type"].(string); errType != "invalidArguments" {
		t.Errorf("expected invalidArguments for string maxChanges, got %q", errType)
	}

	// 6. Create multiple mailboxes to produce changes
	postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mb1": map[string]any{"name": "MC1"},
				"mb2": map[string]any{"name": "MC2"},
			},
		}, "c5"},
	})

	// 7. Request changes with maxChanges = 1
	rLimit := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": initialState,
			"maxChanges": 1,
		}, "c6"},
	})
	argsLimit := rLimit.MethodResponses[0].Args
	cList, _ := argsLimit["created"].([]any)
	uList, _ := argsLimit["updated"].([]any)
	dList, _ := argsLimit["destroyed"].([]any)
	totalReturned := len(cList) + len(uList) + len(dList)
	if totalReturned > 1 {
		t.Errorf("total changes returned (%d) MUST NOT exceed maxChanges (1)", totalReturned)
	}
}

// TestRFC8620_Section5_3_PatchRestrictions verifies JSON Pointer patch restrictions,
// server-set property protections, and atomicity per RFC 8620 Section 5.3.
func TestRFC8620_Section5_3_PatchRestrictions(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "All paths MUST also conform to the following restrictions; if")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "there is any violation, the update MUST be rejected with an")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "* The pointer MUST NOT reference inside an array (i")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "NOT insert/delete from an array; the array MUST be replaced in")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "slash) MUST already exist on the object being patched")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "* There MUST NOT be two patches in the PatchObject where the")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "Otherwise, the update MUST be")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "commit changes to some objects but not others; however, it MUST NOT")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// Create a test mailbox
	rCreate := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mb1": map[string]any{"name": "PatchSubject"},
			},
		}, "c0"},
	})
	created, _ := rCreate.MethodResponses[0].Args["created"].(map[string]any)
	mbID, _ := created["mb1"].(map[string]any)["id"].(string)

	// Test A: Prefix conflict in PatchObject ("name" and "name/sub")
	rConflict := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				mbID: map[string]any{
					"name":     "NameA",
					"name/sub": "NameB",
				},
			},
		}, "c1"},
	})
	notUpdatedA, _ := rConflict.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errA, _ := notUpdatedA[mbID].(map[string]any)
	if errA["type"] != "invalidPatch" {
		t.Errorf("expected invalidPatch for prefix conflict, got: %v", errA)
	}

	// Test B: Pointer referencing inside an array or indexing an array
	rArray := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				mbID: map[string]any{
					"alerts/0/offset": "-PT15M",
				},
			},
		}, "c2"},
	})
	notUpdatedB, _ := rArray.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errB, _ := notUpdatedB[mbID].(map[string]any)
	if errB["type"] != "invalidPatch" {
		t.Errorf("expected invalidPatch for array indexing pointer, got: %v", errB)
	}

	// Test C: Non-existent parent path
	rNonExistent := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				mbID: map[string]any{
					"missingParent/child": "val",
				},
			},
		}, "c3"},
	})
	notUpdatedC, _ := rNonExistent.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errC, _ := notUpdatedC[mbID].(map[string]any)
	if errC["type"] != "invalidPatch" {
		t.Errorf("expected invalidPatch for non-existent parent path, got: %v", errC)
	}

	// Test D: Modifying server-set property (e.g. totalEmails) -> invalidProperties
	rServerSet := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				mbID: map[string]any{
					"totalEmails": 9999,
				},
			},
		}, "c4"},
	})
	notUpdatedD, _ := rServerSet.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errD, _ := notUpdatedD[mbID].(map[string]any)
	if errD["type"] != "invalidProperties" {
		t.Errorf("expected invalidProperties for modifying server-set property, got: %v", errD)
	}

	// Test E: Providing server-set property with identical current value is allowed
	rIdentical := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				mbID: map[string]any{
					"id":   mbID,
					"name": "IdenticalAllowed",
				},
			},
		}, "c5"},
	})
	updatedE, _ := rIdentical.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := updatedE[mbID]; !ok {
		t.Errorf("expected update with identical server-set property to succeed, got: %v", rIdentical.MethodResponses[0].Args)
	}

	// Test F: Update atomicity: an update containing both a valid change and an invalid change
	// MUST NOT commit the valid change partially.
	rAtomic := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				mbID: map[string]any{
					"name":        "MustNotBeRenamedPartially",
					"totalEmails": 9999,
				},
			},
		}, "c6"},
	})
	notUpdatedF, _ := rAtomic.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if _, ok := notUpdatedF[mbID]; !ok {
		t.Fatalf("expected update to fail atomically, got: %v", rAtomic.MethodResponses[0].Args)
	}

	// Verify mailbox name remains "IdenticalAllowed"
	rVerify := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary", "ids": []any{mbID}}, "c7"},
	})
	listV, _ := rVerify.MethodResponses[0].Args["list"].([]any)
	currentName := listV[0].(map[string]any)["name"].(string)
	if currentName != "IdenticalAllowed" {
		t.Errorf("atomic update violation: partial change committed, current name is %q", currentName)
	}
}

// TestRFC8620_Section5_3_CreationRefs_MapAndOrderingAndReuse verifies creation reference
// map persistence during request, dependency ordering, and reuse mapping to most recent id.
func TestRFC8620_Section5_3_CreationRefs_MapAndOrderingAndReuse(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "processing a request, the server MUST keep a simple map for the")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "references to the same type, the server MUST order the creates and")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "If a creation id is reused, the server MUST map the")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// Test A: Dependency ordering within the same type:
	// Child is listed before Parent; the server MUST order creates such that Parent
	// is created before Child.
	rOrder := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"child":  map[string]any{"name": "ChildFolder", "parentId": "#parent"},
				"parent": map[string]any{"name": "ParentFolder"},
			},
		}, "c1"},
	})
	createdA, _ := rOrder.MethodResponses[0].Args["created"].(map[string]any)
	parentObj, parentOK := createdA["parent"].(map[string]any)
	childObj, childOK := createdA["child"].(map[string]any)
	if !parentOK || !childOK {
		t.Fatalf("both parent and child MUST be created, got: %v", createdA)
	}
	parentID := parentObj["id"].(string)
	if childObj["parentId"] != parentID {
		t.Errorf("child.parentId %v MUST resolve to parent id %q", childObj["parentId"], parentID)
	}

	// Test B: Server keeps creation id map during the request:
	// Call 1 creates a mailbox with creation ID "m1"
	// Call 2 in same request updates that mailbox using "#m1" as update key
	rMap := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"m1": map[string]any{"name": "KeepMapTest"},
			},
		}, "c2"},
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				"#m1": map[string]any{"name": "KeepMapTestRenamed"},
			},
		}, "c3"},
	})
	createdB, _ := rMap.MethodResponses[0].Args["created"].(map[string]any)
	realID, _ := createdB["m1"].(map[string]any)["id"].(string)
	updatedB, _ := rMap.MethodResponses[1].Args["updated"].(map[string]any)
	if _, ok := updatedB[realID]; !ok {
		t.Errorf("server MUST keep creation map across calls in request: expected updated[%q], got: %v", realID, updatedB)
	}

	// Test C: Reuse of creation ID maps to most recently created id:
	// Method 1 creates "Orig" with creation id "reusedCid"
	// Method 2 creates "Recent" with same creation id "reusedCid"
	// Method 3 creates a child with parentId "#reusedCid"
	// child.parentId MUST resolve to the most recently created id (Recent)!
	rReuse := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"reusedCid": map[string]any{"name": "ReuseFirst"},
			},
		}, "step1"},
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"reusedCid": map[string]any{"name": "ReuseSecond"},
			},
		}, "step2"},
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"childOfReused": map[string]any{
					"name":     "ChildOfReused",
					"parentId": "#reusedCid",
				},
			},
		}, "step3"},
	})

	idFirst, _ := rReuse.MethodResponses[0].Args["created"].(map[string]any)["reusedCid"].(map[string]any)["id"].(string)
	idSecond, _ := rReuse.MethodResponses[1].Args["created"].(map[string]any)["reusedCid"].(map[string]any)["id"].(string)
	childOfReused, _ := rReuse.MethodResponses[2].Args["created"].(map[string]any)["childOfReused"].(map[string]any)

	if idFirst == "" || idSecond == "" || idFirst == idSecond {
		t.Fatalf("expected distinct ids for step1 and step2, got %q and %q", idFirst, idSecond)
	}
	actualParentID := childOfReused["parentId"].(string)
	if actualParentID != idSecond {
		t.Errorf("reused creation ID MUST map to most recently created id %q, got: %q", idSecond, actualParentID)
	}
}
