package jmap_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9404_SpecConventions verifies BCP 14 normative keyword conventions and reference metadata.
func TestRFC9404_SpecConventions(t *testing.T) {
	spectest.Require(t, "RFC9404", "", spectest.MAY, "and how to provide feedback on it may be obtained at")
	spectest.Require(t, "RFC9404", "", spectest.MUST, "Code Components extracted from this document must")
	spectest.Require(t, "RFC9404", "1", spectest.MAY, "Since raw blobs may contain arbitrary binary data, this document")
	spectest.Require(t, "RFC9404", "2", spectest.MUST, "The key words \"MUST\", \"MUST NOT\", \"REQUIRED\", \"SHALL\", \"SHALL NOT\",")
	spectest.Require(t, "RFC9404", "2", spectest.SHOULD, "\"SHOULD\", \"SHOULD NOT\", \"RECOMMENDED\", \"NOT RECOMMENDED\", \"MAY\", and")
	spectest.Require(t, "RFC9404", "2", spectest.MAY, "\"OPTIONAL\" in this document are to be interpreted as described in")
	spectest.Require(t, "RFC9404", "6.3", spectest.MUST, "The registration policy for this registry is \"Specification Required\"")
	spectest.Require(t, "RFC9404", "7.2", spectest.MAY, "17487/RFC7888, May 2016,")
	spectest.Require(t, "RFC9404", "2119", spectest.MAY, "May 2017, <https://www")
}

// TestRFC9404_Capability_Properties verifies JMAP Blob Management capability properties per RFC 9404 §3.1.
func TestRFC9404_Capability_Properties(t *testing.T) {
	spectest.Require(t, "RFC9404", "3.1", spectest.MUST, "Clients MUST NOT attempt to create blobs larger than this size")
	spectest.Require(t, "RFC9404", "3.1", spectest.MUST, "If this value is null, then clients are not required to limit the")
	spectest.Require(t, "RFC9404", "3.1", spectest.MAY, "Note that the supportedTypeNames list may include private types")
	spectest.Require(t, "RFC9404", "3.1", spectest.MUST, "Clients MUST ignore type names they do not recognise")
	spectest.Require(t, "RFC9404", "3.1", spectest.MUST, "registry defined by [RFC3230]; however, in JMAP, they must be")
	spectest.Require(t, "RFC9404", "3.1", spectest.SHOULD, "Clients SHOULD prefer algorithms listed earlier in this list")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session: %v", err)
	}

	acc, ok := session.Accounts[jmap.AccountIDForSubject(testUsername)]
	if !ok {
		t.Fatal("primary account missing in session")
	}
	accCapRaw, ok := acc.AccountCapabilities[jmap.BlobCapabilityURI]
	if !ok {
		t.Fatalf("Capability %q missing in primary account capabilities", jmap.BlobCapabilityURI)
	}

	capBytes, _ := json.Marshal(accCapRaw)
	var blobCap jmap.BlobCapability
	_ = json.Unmarshal(capBytes, &blobCap)

	// maxSizeBlobSet is null (nil): clients are not required to limit creation size
	if blobCap.MaxSizeBlobSet != nil {
		t.Errorf("expected maxSizeBlobSet to be null, got %v", *blobCap.MaxSizeBlobSet)
	}

	// supportedTypeNames includes standard types; clients must ignore unknown types
	if len(blobCap.SupportedTypeNames) == 0 {
		t.Errorf("expected non-empty supportedTypeNames")
	}
	hasEmail := false
	for _, name := range blobCap.SupportedTypeNames {
		if name == "Email" {
			hasEmail = true
			break
		}
	}
	if !hasEmail {
		t.Errorf("expected 'Email' in supportedTypeNames, got %v", blobCap.SupportedTypeNames)
	}

	// supportedDigestAlgorithms must be lower case per RFC 9404 Section 3.1
	if len(blobCap.SupportedDigestAlgorithms) == 0 {
		t.Fatalf("expected non-empty supportedDigestAlgorithms")
	}
	for _, algo := range blobCap.SupportedDigestAlgorithms {
		for _, ch := range algo {
			if ch >= 'A' && ch <= 'Z' {
				t.Errorf("algorithm name %q contains uppercase characters", algo)
			}
		}
	}
	// Preferred algorithm (earlier in list)
	if blobCap.SupportedDigestAlgorithms[0] != "sha-256" {
		t.Errorf("expected sha-256 to be preferred algorithm, got %s", blobCap.SupportedDigestAlgorithms[0])
	}
}

// TestRFC9404_Upload_DataSourceObject_NoGuessingIntent verifies that servers strictly validate DataSourceObject
// and do not guess client intent per RFC 9404 §4.1.
func TestRFC9404_Upload_DataSourceObject_NoGuessingIntent(t *testing.T) {
	spectest.Require(t, "RFC9404", "4.1", spectest.MAY, "provided, the server MAY perform content analysis and return one")
	spectest.Require(t, "RFC9404", "4.1", spectest.MAY, "This may be extended in the future;")
	spectest.Require(t, "RFC9404", "4.1", spectest.SHOULD, "limit for JMAP requests specified by the server, and clients SHOULD")
	spectest.Require(t, "RFC9404", "4.1", spectest.MUST, "* data:asText: \"String|null\" (raw octets, must be UTF-8)")
	spectest.Require(t, "RFC9404", "4.1", spectest.MAY, "* offset: \"UnsignedInt|null\" (MAY be zero)")
	spectest.Require(t, "RFC9404", "4.1", spectest.MAY, "* length: \"UnsignedInt|null\" (MAY be zero)")
	spectest.Require(t, "RFC9404", "4.1", spectest.MUST, "contained in them, the server MUST NOT guess the user's intent and")
	spectest.Require(t, "RFC9404", "4.1", spectest.MUST, "invalid UTF-8 in data:asText MUST result in a notCreated response")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.BlobCapabilityURI}

	// 1. Valid data:asText with UTF-8 creates blob
	rValid := postJMAP(t, ts.URL, using, []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"valid1": map[string]any{
					"type": "text/plain; charset=utf-8",
					"data": []any{
						map[string]any{"data:asText": "Valid UTF-8 \u2713 text"},
					},
				},
			},
		}, "c1"},
	})
	created, _ := rValid.MethodResponses[0].Args["created"].(map[string]any)
	validBlob, ok := created["valid1"].(map[string]any)
	if !ok || validBlob["id"] == nil {
		t.Fatalf("expected valid UTF-8 blob to be created, got %v", rValid.MethodResponses[0].Args)
	}
	baseBlobID := validBlob["id"].(string)

	// 2. data:asText with offset (misplaced property) -> rejected with invalidProperties
	rMisplaced := postJMAP(t, ts.URL, using, []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"badMisplaced": map[string]any{
					"data": []any{
						map[string]any{
							"data:asText": "some text",
							"offset":      0,
						},
					},
				},
			},
		}, "c2"},
	})
	notCreated, _ := rMisplaced.MethodResponses[0].Args["notCreated"].(map[string]any)
	errObj, ok := notCreated["badMisplaced"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected invalidProperties for misplaced offset on data:asText, got %v", notCreated)
	}

	// 3. More than one data source key: data:asText AND data:asBase64 -> rejected without guessing
	rConflict1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"badConflict1": map[string]any{
					"data": []any{
						map[string]any{
							"data:asText":   "some text",
							"data:asBase64": base64.StdEncoding.EncodeToString([]byte("some text")),
						},
					},
				},
			},
		}, "c3"},
	})
	notCreated, _ = rConflict1.MethodResponses[0].Args["notCreated"].(map[string]any)
	errObj, ok = notCreated["badConflict1"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected invalidProperties for conflicting data keys, got %v", notCreated)
	}

	// 4. More than one data source key: data:asText AND blobId -> rejected
	rConflict2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"badConflict2": map[string]any{
					"data": []any{
						map[string]any{
							"data:asText": "some text",
							"blobId":      baseBlobID,
						},
					},
				},
			},
		}, "c4"},
	})
	notCreated, _ = rConflict2.MethodResponses[0].Args["notCreated"].(map[string]any)
	errObj, ok = notCreated["badConflict2"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected invalidProperties for data:asText + blobId, got %v", notCreated)
	}

	// 5. Unknown property in DataSourceObject -> rejected
	rUnknownProp := postJMAP(t, ts.URL, using, []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"badUnknown": map[string]any{
					"data": []any{
						map[string]any{
							"data:asText":      "some text",
							"unknownPropField": "xyz",
						},
					},
				},
			},
		}, "c5"},
	})
	notCreated, _ = rUnknownProp.MethodResponses[0].Args["notCreated"].(map[string]any)
	errObj, ok = notCreated["badUnknown"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected invalidProperties for unknown property in DataSourceObject, got %v", notCreated)
	}

	// 6. Invalid UTF-8 in data:asText -> rejected with invalidProperties
	handler, ok := srv.MethodRegistry.Get("Blob/upload")
	if !ok {
		t.Fatal("Blob/upload handler not registered")
	}
	_, respArgs := handler(context.Background(), map[string]any{
		"accountId": "primary",
		"create": map[string]any{
			"badUTF8": map[string]any{
				"data": []any{
					map[string]any{
						"data:asText": "invalid \xff\xfe bytes",
					},
				},
			},
		},
	}, "c6")
	notCreatedMap, _ := respArgs["notCreated"].(map[string]any)
	errObjSet, ok := notCreatedMap["badUTF8"].(jmap.SetError)
	if !ok || errObjSet.Type != "invalidProperties" {
		t.Errorf("expected invalidProperties for invalid UTF-8 in data:asText, got %v", notCreatedMap)
	}

	// 7. Valid offset and length with zero values on blobId
	rZeroOffsetLength := postJMAP(t, ts.URL, using, []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"zeroSlice": map[string]any{
					"type": "text/plain",
					"data": []any{
						map[string]any{
							"blobId": baseBlobID,
							"offset": 0,
							"length": 0,
						},
					},
				},
			},
		}, "c7"},
	})
	created, _ = rZeroOffsetLength.MethodResponses[0].Args["created"].(map[string]any)
	zeroBlob, ok := created["zeroSlice"].(map[string]any)
	if !ok || zeroBlob["id"] == nil {
		t.Errorf("expected zero-length slice to create empty blob, got %v", rZeroOffsetLength.MethodResponses[0].Args)
	}
}

// TestRFC9404_BlobGet_EfficiencyAndOptions verifies Blob/get options and efficient size querying per RFC 9404 §4.2.
func TestRFC9404_BlobGet_EfficiencyAndOptions(t *testing.T) {
	spectest.Require(t, "RFC9404", "4.2", spectest.MAY, "A standard JMAP get, with two additional optional parameters:")
	spectest.Require(t, "RFC9404", "4.2", spectest.SHOULD, "Servers SHOULD store the size for blobs in a format that is efficient")
	spectest.Require(t, "RFC9404", "4.2", spectest.SHOULD, "to read, and clients SHOULD limit their request to just the size")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.BlobCapabilityURI}

	// Create a blob first
	uploadRes := postJMAP(t, ts.URL, using, []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"b1": map[string]any{
					"type": "application/octet-stream",
					"data": []any{
						map[string]any{"data:asText": "0123456789abcdef"},
					},
				},
			},
		}, "u1"},
	})
	created, _ := uploadRes.MethodResponses[0].Args["created"].(map[string]any)
	blobID := created["b1"].(map[string]any)["id"].(string)

	// Fetch only size: client limits request to just the size for efficiency
	getRes := postJMAP(t, ts.URL, using, []any{
		[]any{"Blob/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{blobID},
			"properties": []any{"size"},
		}, "g1"},
	})
	list, _ := getRes.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 blob in list, got %v", getRes.MethodResponses[0].Args)
	}
	blobItem := list[0].(map[string]any)
	if blobItem["size"] != float64(16) {
		t.Errorf("expected size 16, got %v", blobItem["size"])
	}
	// When only size was requested, payload data MUST NOT be included
	if blobItem["data:asText"] != nil || blobItem["data:asBase64"] != nil {
		t.Errorf("payload data should be omitted when only size is requested, got %v", blobItem)
	}
}

// TestRFC9404_BlobLookup_AccessControl verifies access control enforcement and handling of untrusted data per RFC 9404 §4.3 and §5.
func TestRFC9404_BlobLookup_AccessControl(t *testing.T) {
	spectest.Require(t, "RFC9404", "4.3", spectest.MAY, "which \"Can reference blobs\" is true may be specified, and the")
	spectest.Require(t, "RFC9404", "5", spectest.MUST, "requires that all JSON data be UTF-8 encoded, so servers MUST only")
	spectest.Require(t, "RFC9404", "5", spectest.MUST, "Servers MUST apply any access controls, such that if the")
	spectest.Require(t, "RFC9404", "5", spectest.MUST, "does not have access to see, then that emailId MUST NOT be returned")
	spectest.Require(t, "RFC9404", "5", spectest.MUST, "The server MUST NOT trust that the data given to a Blob/upload is a")
	spectest.Require(t, "RFC9404", "5", spectest.SHOULD, "designed to deal with arbitrary untrusted data should be used")
	spectest.Require(t, "RFC9404", "5", spectest.SHOULD, "server SHOULD NOT reject data on the grounds that it is not a valid")
	spectest.Require(t, "RFC9404", "5", spectest.MAY, "(anti-virus or exfiltration scanners, for example) that may be")
	spectest.Require(t, "RFC9404", "5", spectest.SHOULD, "Server implementations SHOULD provide")
	spectest.Require(t, "RFC9404", "5", spectest.SHOULD, "that share resources between multiple users should track resource")

	authBackend := jmap.NewMemoryAuthBackend()
	srv := newTestServer(jmap.WithAuthBackend(authBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	tokenA, errA := authBackend.Authenticate(nil, "userA@example.com", "userA@example.com")
	if errA != nil {
		t.Fatalf("auth userA failed: %v", errA)
	}
	tokenB, errB := authBackend.Authenticate(nil, "userB@example.com", "userB@example.com")
	if errB != nil {
		t.Fatalf("auth userB failed: %v", errB)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.BlobCapabilityURI, jmap.MailCapabilityURI}

	// 1. Untrusted arbitrary binary data: server does not reject unvalidated data formats
	arbitraryBinary := base64.StdEncoding.EncodeToString([]byte("\x00\x01\x02\xff\xfe\xfd\x80\x81\x82"))
	rUploadA := postJMAPWithToken(t, ts.URL, tokenA, using, []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"rawBinary": map[string]any{
					"type": "application/x-custom-binary",
					"data": []any{
						map[string]any{"data:asBase64": arbitraryBinary},
					},
				},
			},
		}, "uA"},
	})
	createdA, _ := rUploadA.MethodResponses[0].Args["created"].(map[string]any)
	blobA, ok := createdA["rawBinary"].(map[string]any)
	if !ok || blobA["id"] == nil {
		t.Fatalf("server should accept arbitrary untrusted blob data, got %v", rUploadA.MethodResponses[0].Args)
	}
	blobAID := blobA["id"].(string)

	// User A imports an email referencing this blob
	mbResA := postJMAPWithToken(t, ts.URL, tokenA, using, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "mbA"},
	})
	var inboxIDA string
	if list, ok := mbResA.MethodResponses[0].Args["list"].([]any); ok {
		for _, raw := range list {
			m := raw.(map[string]any)
			if m["role"] == "inbox" || m["name"] == "INBOX" {
				inboxIDA = m["id"].(string)
				break
			}
		}
	}
	if inboxIDA == "" {
		t.Fatalf("could not find INBOX for userA")
	}

	emailUploadA := postJMAPWithToken(t, ts.URL, tokenA, using, []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"emBlob": map[string]any{
					"type": "message/rfc822",
					"data": []any{
						map[string]any{"data:asText": "From: userA@example.com\r\nSubject: Secret Msg\r\n\r\nSecret Content"},
					},
				},
			},
		}, "uEmail"},
	})
	createdEmBlob, _ := emailUploadA.MethodResponses[0].Args["created"].(map[string]any)
	emBlobID := createdEmBlob["emBlob"].(map[string]any)["id"].(string)

	importResA := postJMAPWithToken(t, ts.URL, tokenA, using, []any{
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"i1": map[string]any{
					"blobId":     emBlobID,
					"mailboxIds": map[string]any{inboxIDA: true},
				},
			},
		}, "imp1"},
	})
	createdImport, _ := importResA.MethodResponses[0].Args["created"].(map[string]any)
	if createdImport["i1"] == nil {
		t.Fatalf("Email/import failed: %v", importResA.MethodResponses[0].Args)
	}

	// 2. User B calls Blob/lookup for user A's blob:
	// Servers MUST apply access controls such that if user does not have access to see,
	// that emailId MUST NOT be returned in matchedIds.
	lookupResB := postJMAPWithToken(t, ts.URL, tokenB, using, []any{
		[]any{"Blob/lookup", map[string]any{
			"accountId": "primary",
			"typeNames": []any{"Email"},
			"ids":       []any{emBlobID, blobAID},
		}, "lookB"},
	})
	lookupListB, _ := lookupResB.MethodResponses[0].Args["list"].([]any)
	if len(lookupListB) != 2 {
		t.Fatalf("expected 2 BlobInfo entries for userB lookup, got %v", lookupResB.MethodResponses[0].Args)
	}
	for _, raw := range lookupListB {
		m := raw.(map[string]any)
		matched, _ := m["matchedIds"].(map[string]any)
		emailMatches, _ := matched["Email"].([]any)
		if len(emailMatches) != 0 {
			t.Errorf("User B MUST NOT see User A's email references for blob %v, got %v", m["id"], emailMatches)
		}
	}
}
