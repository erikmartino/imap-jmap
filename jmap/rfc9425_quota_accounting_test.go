package jmap_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/spectest"
)

// TestRFC9425_Section4_1_Quota_DataTypesAndProperties verifies that Quota objects
// contain all mandatory RFC 9425 Section 4.1 properties including dataTypes.
func TestRFC9425_Section4_1_Quota_DataTypesAndProperties(t *testing.T) {
	spectest.Require(t, "RFC9425", "4.1", "MUST", "A Quota object represents a resource limit... dataTypes: A list of all the data type names that are counted against this quota")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.QuotaCapabilityURI}
	calls := []any{
		[]any{"Quota/get", map[string]any{"accountId": "primary"}, "c1"},
	}

	res := postJMAP(t, ts.URL, using, calls)
	if len(res.MethodResponses) != 1 {
		t.Fatalf("Expected 1 response, got %d", len(res.MethodResponses))
	}

	list, ok := res.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list) < 2 {
		t.Fatalf("Expected at least 2 quotas in list, got %v", res.MethodResponses[0].Args["list"])
	}

	for _, item := range list {
		q, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("Expected quota object, got %v", item)
		}
		if q["id"] == nil || q["id"] == "" {
			t.Errorf("Quota missing id: %v", q)
		}
		if q["name"] == nil || q["name"] == "" {
			t.Errorf("Quota missing name: %v", q)
		}
		if q["resourceType"] != "octets" && q["resourceType"] != "messages" {
			t.Errorf("Unexpected resourceType: %v", q["resourceType"])
		}
		if _, ok := q["used"].(float64); !ok {
			t.Errorf("Quota missing or invalid used counter: %v", q["used"])
		}
		if _, ok := q["hardLimit"].(float64); !ok {
			t.Errorf("Quota missing or invalid hardLimit: %v", q["hardLimit"])
		}
		if q["scope"] != "account" {
			t.Errorf("Expected scope 'account', got %v", q["scope"])
		}

		dtRaw, ok := q["dataTypes"].([]any)
		if !ok || len(dtRaw) == 0 {
			t.Fatalf("Quota missing mandatory dataTypes array: %v", q)
		}
		hasEmail := false
		for _, dt := range dtRaw {
			if s, ok := dt.(string); ok && s == "Email" {
				hasEmail = true
			}
		}
		if !hasEmail {
			t.Errorf("Expected dataTypes to contain 'Email', got %v", dtRaw)
		}
	}
}

// TestRFC9425_DynamicQuotaAccounting_EmailCreateDestroy verifies that creating and destroying
// emails dynamically adjusts used octets and used message count.
func TestRFC9425_DynamicQuotaAccounting_EmailCreateDestroy(t *testing.T) {
	spectest.Require(t, "RFC9425", "4.1", "MUST", "used: UnsignedInt. The current usage of the specified resource.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.QuotaCapabilityURI}

	// 1. Check initial quota usage
	getQuotas := func() (octetsUsed uint64, msgsUsed uint64, state string) {
		res := postJMAP(t, ts.URL, using, []any{
			[]any{"Quota/get", map[string]any{"accountId": "primary"}, "c1"},
		})
		list := res.MethodResponses[0].Args["list"].([]any)
		state, _ = res.MethodResponses[0].Args["state"].(string)
		for _, it := range list {
			q := it.(map[string]any)
			if q["resourceType"] == "octets" {
				octetsUsed = uint64(q["used"].(float64))
			} else if q["resourceType"] == "messages" {
				msgsUsed = uint64(q["used"].(float64))
			}
		}
		return
	}

	initOctets, initMsgs, initState := getQuotas()

	// 2. Create an email via Email/set
	content := "This is a test message body to verify dynamic quota tracking."
	createRes := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"msg1": map[string]any{
					"subject":    "Quota Accounting Test",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{
						"1": map[string]any{"value": content},
					},
					"textBody": []any{
						map[string]any{"partId": "1", "type": "text/plain"},
					},
				},
			},
		}, "c1"},
	})

	createdMap, ok := createRes.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || createdMap["msg1"] == nil {
		t.Fatalf("Failed to create email: %v", createRes.MethodResponses[0].Args)
	}
	emailObj := createdMap["msg1"].(map[string]any)
	emailID := emailObj["id"].(string)
	emailSize := uint64(emailObj["size"].(float64))

	// 3. Check quota usage after creation
	afterOctets, afterMsgs, afterState := getQuotas()
	if afterOctets != initOctets+emailSize {
		t.Errorf("Expected octets used %d, got %d", initOctets+emailSize, afterOctets)
	}
	if afterMsgs != initMsgs+1 {
		t.Errorf("Expected messages used %d, got %d", initMsgs+1, afterMsgs)
	}
	if afterState == initState {
		t.Errorf("Expected Quota state to change after email create, stayed %q", initState)
	}

	// 4. Destroy the email via Email/set
	destroyRes := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{emailID},
		}, "c1"},
	})
	destroyedList, ok := destroyRes.MethodResponses[0].Args["destroyed"].([]any)
	if !ok || len(destroyedList) != 1 {
		t.Fatalf("Failed to destroy email: %v", destroyRes.MethodResponses[0].Args)
	}

	// 5. Check quota usage after deletion
	finalOctets, finalMsgs, finalState := getQuotas()
	if finalOctets != initOctets {
		t.Errorf("Expected octets used to return to %d, got %d", initOctets, finalOctets)
	}
	if finalMsgs != initMsgs {
		t.Errorf("Expected messages used to return to %d, got %d", initMsgs, finalMsgs)
	}
	if finalState == afterState {
		t.Errorf("Expected Quota state to change after email destroy, stayed %q", afterState)
	}
}

// TestRFC9425_OverQuota_EmailSetCreate verifies that creating an email when limits are exceeded
// returns an overQuota error in notCreated per RFC 8620 Section 5.3 and RFC 9425.
func TestRFC9425_OverQuota_EmailSetCreate(t *testing.T) {
	spectest.Require(t, "RFC9425", "5.1", "MUST", "overQuota: The create would exceed a quota limit.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accountID := jmap.AccountIDForSubject(testUsername)
	backend := srv.MailBackend.(*imapsmtp.IMAPSMTPBackend)

	// Set tight storage quota (hardLimit: 50 octets)
	backend.SetQuotaHardLimits(accountID, 50, 1000)

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.QuotaCapabilityURI}
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"oversized": map[string]any{
					"subject":    "Large message",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{
						"1": map[string]any{"value": "This text definitely exceeds 50 octets of total serialized message size."},
					},
					"textBody": []any{
						map[string]any{"partId": "1", "type": "text/plain"},
					},
				},
			},
		}, "c1"},
	})

	notCreated, ok := res.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok || notCreated["oversized"] == nil {
		t.Fatalf("Expected notCreated error for oversized message, got %v", res.MethodResponses[0].Args)
	}
	errObj := notCreated["oversized"].(map[string]any)
	if errObj["type"] != "overQuota" {
		t.Errorf("Expected error type 'overQuota', got %v", errObj["type"])
	}

	// Now test message count quota: set message count limit to 1
	backend.SetQuotaHardLimits(accountID, 1073741824, 1)

	// Create 1st email (should succeed)
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"first": map[string]any{
					"subject":    "Allowed message",
					"mailboxIds": map[string]any{"mb-inbox": true},
				},
			},
		}, "c1"},
	})
	created, ok := res1.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["first"] == nil {
		t.Fatalf("Expected 1st message to succeed, got %v", res1.MethodResponses[0].Args)
	}

	// Create 2nd email (should fail with overQuota)
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"second": map[string]any{
					"subject":    "Exceeded message count",
					"mailboxIds": map[string]any{"mb-inbox": true},
				},
			},
		}, "c2"},
	})
	notCreated2, ok := res2.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok || notCreated2["second"] == nil {
		t.Fatalf("Expected notCreated error for 2nd message, got %v", res2.MethodResponses[0].Args)
	}
	errObj2 := notCreated2["second"].(map[string]any)
	if errObj2["type"] != "overQuota" {
		t.Errorf("Expected error type 'overQuota' on message limit exceeded, got %v", errObj2["type"])
	}
}

// TestRFC9425_OverQuota_EmailImport verifies that importing an email when quota is exceeded
// returns an overQuota error in notCreated.
func TestRFC9425_OverQuota_EmailImport(t *testing.T) {
	spectest.Require(t, "RFC9425", "5.1", "MUST", "Email/import overQuota: The create would exceed a quota limit.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accountID := jmap.AccountIDForSubject(testUsername)
	backend := srv.MailBackend.(*imapsmtp.IMAPSMTPBackend)

	// Set tiny limit
	backend.SetQuotaHardLimits(accountID, 10, 1000)

	// Upload a blob to import
	rawEmail := "From: alice@example.com\r\nTo: bob@example.com\r\nSubject: Test\r\n\r\nHello World!"
	blobRes, err := authedPost(ts.URL+"/upload/"+accountID, "message/rfc822", strings.NewReader(rawEmail))
	if err != nil || blobRes.StatusCode != 201 {
		t.Fatalf("Failed to upload blob: %v, status: %d", err, blobRes.StatusCode)
	}
	var blobObj map[string]any
	json.NewDecoder(blobRes.Body).Decode(&blobObj)
	blobID := blobObj["blobId"].(string)

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.QuotaCapabilityURI}
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"emails": map[string]any{
				"imp1": map[string]any{
					"blobId":     blobID,
					"mailboxIds": map[string]any{"mb-inbox": true},
				},
			},
		}, "c1"},
	})

	notCreated, ok := res.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok || notCreated["imp1"] == nil {
		t.Fatalf("Expected notCreated in Email/import, got %v", res.MethodResponses[0].Args)
	}
	errObj := notCreated["imp1"].(map[string]any)
	if errObj["type"] != "overQuota" {
		t.Errorf("Expected error type 'overQuota' on Email/import, got %v", errObj["type"])
	}
}

// TestRFC9425_QuotaQuery_FiltersAndUnsupportedSort tests Quota/query filtering by dataTypes
// and rejection of unsupported sort arguments.
func TestRFC9425_QuotaQuery_FiltersAndUnsupportedSort(t *testing.T) {
	spectest.Require(t, "RFC9425", "4.4", "MUST", "dataTypes: String. The Quota dataTypes property must contain the given datatype to match.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.QuotaCapabilityURI}

	// 1. Query by dataTypes: "Email" -> matches both quotas
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"Quota/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"dataTypes": "Email"},
		}, "c1"},
		[]any{"Quota/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"dataTypes": "Calendar"},
		}, "c2"},
		[]any{"Quota/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"operator": "AND",
				"conditions": []any{
					map[string]any{"dataTypes": "Email"},
					map[string]any{"resourceType": "octets"},
				},
			},
		}, "c3"},
	})

	if len(res.MethodResponses) != 3 {
		t.Fatalf("Expected 3 responses, got %d", len(res.MethodResponses))
	}

	ids1 := res.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 2 {
		t.Errorf("Expected 2 quotas for dataTypes:Email, got %d", len(ids1))
	}

	ids2 := res.MethodResponses[1].Args["ids"].([]any)
	if len(ids2) != 0 {
		t.Errorf("Expected 0 quotas for dataTypes:Calendar, got %d", len(ids2))
	}

	ids3 := res.MethodResponses[2].Args["ids"].([]any)
	if len(ids3) != 1 {
		t.Errorf("Expected 1 quota for dataTypes:Email AND resourceType:octets, got %d", len(ids3))
	}

	// 2. Query with unsupported sort argument
	sortRes := postJMAP(t, ts.URL, using, []any{
		[]any{"Quota/query", map[string]any{
			"accountId": "primary",
			"sort":      []any{map[string]any{"property": "name"}},
		}, "c4"},
	})
	if sortRes.MethodResponses[0].Name != "error" {
		t.Errorf("Expected error method response for Quota/query sort, got %s", sortRes.MethodResponses[0].Name)
	}
	errType := sortRes.MethodResponses[0].Args["type"]
	if errType != "unsupportedSort" {
		t.Errorf("Expected 'unsupportedSort' error, got %v", errType)
	}
}

// TestRFC9425_QuotaChanges verifies that Quota/changes reports updated quotas after email mutations.
func TestRFC9425_QuotaChanges(t *testing.T) {
	spectest.Require(t, "RFC9425", "4.3", "MUST", "Quota/changes: standard changes method per RFC 8620 Section 5.2")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.QuotaCapabilityURI}

	// Get initial state
	getRes := postJMAP(t, ts.URL, using, []any{
		[]any{"Quota/get", map[string]any{"accountId": "primary"}, "c1"},
	})
	initState := getRes.MethodResponses[0].Args["state"].(string)

	// Create an email to advance quota state
	postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"m1": map[string]any{
					"subject":    "Mutation",
					"mailboxIds": map[string]any{"mb-inbox": true},
				},
			},
		}, "c2"},
	})

	// Call Quota/changes
	changesRes := postJMAP(t, ts.URL, using, []any{
		[]any{"Quota/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": initState,
		}, "c3"},
	})

	args := changesRes.MethodResponses[0].Args
	newState := args["newState"].(string)
	if newState == initState {
		t.Errorf("Expected newState %q != oldState %q", newState, initState)
	}
	updated, ok := args["updated"].([]any)
	if !ok || len(updated) == 0 {
		t.Errorf("Expected updated quotas in Quota/changes, got %v", args["updated"])
	}
}
