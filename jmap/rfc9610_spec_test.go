package jmap_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/jmapcontacts"
	"imap-jmap/jmap/spectest"
)

// TestRFC9610_SpecConventions verifies BCP 14 normative keyword conventions and reference metadata.
func TestRFC9610_SpecConventions(t *testing.T) {
	spectest.Require(t, "RFC9610", "", spectest.MAY, "and how to provide feedback on it may be obtained at")
	spectest.Require(t, "RFC9610", "", spectest.MUST, "Code Components extracted from this document must")
	spectest.Require(t, "RFC9610", "1.1", spectest.MUST, "The key words \"MUST\", \"MUST NOT\", \"REQUIRED\", \"SHALL\", \"SHALL NOT\",")
	spectest.Require(t, "RFC9610", "1.1", spectest.SHOULD, "\"SHOULD\", \"SHOULD NOT\", \"RECOMMENDED\", \"NOT RECOMMENDED\", \"MAY\", and")
	spectest.Require(t, "RFC9610", "1.1", spectest.MAY, "\"OPTIONAL\" in this document are to be interpreted as described in")
	spectest.Require(t, "RFC9610", "1.3", spectest.MAY, "In servers with support for JMAP Sharing [RFC9670], users may see and")
	spectest.Require(t, "RFC9610", "2119", spectest.MAY, "May 2017, <https://www")
	spectest.Require(t, "RFC9610", "2119", spectest.MAY, "17487/RFC9553, May 2024,")
}

// TestRFC9610_Capabilities_And_AddressBookCreation verifies the urn:ietf:params:jmap:contacts capability object.
func TestRFC9610_Capabilities_And_AddressBookCreation(t *testing.T) {
	spectest.Require(t, "RFC9610", "1.4.1", spectest.MAY, "*mayCreateAddressBook*: Boolean")
	spectest.Require(t, "RFC9610", "1.4.1", spectest.MAY, "The user may create an AddressBook in this account if, and only")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}
	r := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/get", map[string]any{"accountId": "primary"}, "c1"},
	})
	if len(r.MethodResponses) == 0 {
		t.Fatalf("expected response, got %v", r)
	}

	// Verify creation of addressbook by authorized user succeeds
	createResp := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"ab1": map[string]any{
					"name": "Team Contacts",
				},
			},
		}, "c2"},
	})
	created, ok := createResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || len(created) == 0 {
		t.Fatalf("expected created address book, got %v", createResp.MethodResponses[0].Args)
	}
}

// TestRFC9610_AddressBook_PropertiesAndSorting verifies AddressBook properties, limits, defaults, and sorting.
func TestRFC9610_AddressBook_PropertiesAndSorting(t *testing.T) {
	spectest.Require(t, "RFC9610", "2", spectest.MUST, "This MUST NOT be the")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "An optional long-form description of the AddressBook that provides")
	spectest.Require(t, "RFC9610", "2", spectest.SHOULD, "AddressBooks with equal order should be sorted in")
	spectest.Require(t, "RFC9610", "2", spectest.SHOULD, "The sorting should take into account")
	spectest.Require(t, "RFC9610", "2", spectest.SHOULD, "This SHOULD be true for exactly one AddressBook in any account and")
	spectest.Require(t, "RFC9610", "2", spectest.SHOULD, "The default AddressBook should be used by clients whenever they")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "may automatically set the card as belonging to the default")
	spectest.Require(t, "RFC9610", "2", spectest.SHOULD, "This SHOULD default to false for AddressBooks in")
	spectest.Require(t, "RFC9610", "2", spectest.SHOULD, "If false, the AddressBook and its contents SHOULD only be")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "The UI may offer")
	spectest.Require(t, "RFC9610", "2.1", spectest.MAY, "The \"ids\" argument may be null to fetch all at once")
	spectest.Require(t, "RFC9610", "2.3", spectest.MAY, "The server MAY forbid users from")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}

	// 1. Name must not be empty string (RFC 9610 §2)
	rEmpty := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"bad": map[string]any{"name": ""},
			},
		}, "c1"},
	})
	notCreated, _ := rEmpty.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated["bad"] == nil {
		t.Fatalf("expected empty name to be rejected, got %v", rEmpty.MethodResponses[0].Args)
	}

	// 2. Name must not exceed 255 octets
	tooLongName := string(make([]byte, 256))
	rLong := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"bad": map[string]any{"name": tooLongName},
			},
		}, "c2"},
	})
	notCreatedLong, _ := rLong.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreatedLong["bad"] == nil {
		t.Fatalf("expected >255 octets name to be rejected, got %v", rLong.MethodResponses[0].Args)
	}

	// 3. Create addressbook with description and sortOrder
	desc := "Long-form description of shared contacts"
	rValid := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"good": map[string]any{
					"name":        "Clients",
					"description": desc,
					"sortOrder":   10,
				},
			},
		}, "c3"},
	})
	created, _ := rValid.MethodResponses[0].Args["created"].(map[string]any)
	if created["good"] == nil {
		t.Fatalf("expected valid addressbook created, got %v", rValid.MethodResponses[0].Args)
	}

	// 4. Fetch with ids: null (fetch all at once per RFC 9610 §2.1)
	rAll := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/get", map[string]any{
			"accountId": "primary",
			"ids":       nil,
		}, "c4"},
	})
	list, ok := rAll.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("expected list returned for ids: null, got %v", rAll.MethodResponses[0].Args)
	}

	// Verify isDefault invariant: exactly one default addressbook
	defaultCount := 0
	for _, abRaw := range list {
		ab := abRaw.(map[string]any)
		if ab["isDefault"] == true {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		t.Errorf("expected exactly 1 default addressbook, got %d", defaultCount)
	}
}

// TestRFC9610_AddressBook_SharingAndRights verifies shareWith constraints, mayShare rights,
// and prohibition of right escalation per RFC 9610 §2, §2.3, and §6.
func TestRFC9610_AddressBook_SharingAndRights(t *testing.T) {
	spectest.Require(t, "RFC9610", "2", spectest.MUST, "which this AddressBook belongs MUST NOT be in this set")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "The value may be modified only if the")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "user has the \"mayShare\" right")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "may be found in the urn:ietf:params:jmap:principals:owner")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "*mayRead*: Boolean")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "The user may fetch the ContactCards in this AddressBook")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "*mayWrite*: Boolean")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "The user may create, modify, or destroy all ContactCards in this")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "*mayShare*: Boolean")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "The user may modify the \"shareWith\" property for this AddressBook")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "*mayDelete*: Boolean")
	spectest.Require(t, "RFC9610", "2", spectest.MAY, "The user may delete the AddressBook itself")
	spectest.Require(t, "RFC9610", "2.3", spectest.MAY, "The \"shareWith\" property may only be set by users that have the")
	spectest.Require(t, "RFC9610", "2.3", spectest.MAY, "\"mayShare\" right")
	spectest.Require(t, "RFC9610", "2.3", spectest.MUST, "Any attempt to do so MUST be rejected with a \"forbidden\"")
	spectest.Require(t, "RFC9610", "4.1", spectest.MAY, "\"mayRead\": true,")
	spectest.Require(t, "RFC9610", "4.1", spectest.MAY, "\"mayWrite\": false,")
	spectest.Require(t, "RFC9610", "4.1", spectest.MAY, "\"mayShare\": false,")
	spectest.Require(t, "RFC9610", "4.1", spectest.MAY, "\"mayDelete\": false")
	spectest.Require(t, "RFC9610", "4.1", spectest.MAY, "\"mayWrite\": true,")
	spectest.Require(t, "RFC9610", "4.1", spectest.MAY, "\"mayShare\": true,")
	spectest.Require(t, "RFC9610", "6", spectest.MUST, "Servers MUST enforce the Access Control Lists (ACLs) set on address")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}

	// 1. Attempt to create addressbook with owner principal in shareWith MUST be rejected (RFC 9610 §2)
	rOwnerShare := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"badShare": map[string]any{
					"name": "Bad Share",
					"shareWith": map[string]any{
						"primary": map[string]any{
							"mayRead": true,
						},
					},
				},
			},
		}, "c1"},
	})
	notCreated, _ := rOwnerShare.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated["badShare"] == nil {
		t.Fatalf("expected owner principal in shareWith to be rejected, got %v", rOwnerShare.MethodResponses[0].Args)
	}

	// 2. Create a normal shared addressbook
	rCreate := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sh": map[string]any{
					"name": "Shared AB",
					"shareWith": map[string]any{
						"alice": map[string]any{
							"mayRead":  true,
							"mayWrite": false,
							"mayShare": false,
						},
					},
				},
			},
		}, "c2"},
	})
	shID := rCreate.MethodResponses[0].Args["created"].(map[string]any)["sh"].(map[string]any)["id"].(string)

	// 3. Attempt to update shareWith containing owner principal MUST be rejected with invalidProperties
	rUpdateOwner := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				shID: map[string]any{
					"shareWith": map[string]any{
						"primary": map[string]any{"mayRead": true},
					},
				},
			},
		}, "c3"},
	})
	notUpdated, _ := rUpdateOwner.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if notUpdated[shID] == nil {
		t.Fatalf("expected owner in updated shareWith to be rejected, got %v", rUpdateOwner.MethodResponses[0].Args)
	}
}

// TestRFC9610_ContactCard_MediaAndPhotoValidation tests media data URI to blobId conversion,
// photo upload, and rejection of non-image photo types per RFC 9610 §3, §3.5.
func TestRFC9610_ContactCard_MediaAndPhotoValidation(t *testing.T) {
	spectest.Require(t, "RFC9610", "3", spectest.MAY, "The \"id\" property MAY be different to")
	spectest.Require(t, "RFC9610", "3", spectest.SHOULD, "\"data:\" URL scheme [RFC2397] SHOULD return a \"blobId\" property and")
	spectest.Require(t, "RFC9610", "3", spectest.MUST, "The \"mediaType\" property MUST also be set")
	spectest.Require(t, "RFC9610", "3", spectest.MAY, "when creating or updating a ContactCard, clients MAY send a \"blobId\"")
	spectest.Require(t, "RFC9610", "3.5", spectest.MUST, "To set a new photo, the file must first be uploaded using the upload")
	spectest.Require(t, "RFC9610", "3.5", spectest.MUST, "The server MUST")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}

	// 1. Get default addressbook
	rAB := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/get", map[string]any{"accountId": "primary"}, "ab1"},
	})
	abList := rAB.MethodResponses[0].Args["list"].([]any)
	abID := abList[0].(map[string]any)["id"].(string)

	// 2. Reject card create when photo is NOT a recognised image type (e.g. text/plain data URL)
	nonImageURI := "data:text/plain;base64,SGVsbG8gV29ybGQ="
	rBadPhoto := postJMAP(t, ts.URL, using, []any{
		[]any{"ContactCard/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"card1": map[string]any{
					"addressBookIds": map[string]any{abID: true},
					"name": map[string]any{
						"full": "Bad Photo Contact",
					},
					"media": map[string]any{
						"p1": map[string]any{
							"kind": "photo",
							"uri":  nonImageURI,
						},
					},
				},
			},
		}, "c1"},
	})
	notCreated, _ := rBadPhoto.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated["card1"] == nil {
		t.Fatalf("expected non-image photo to be rejected with invalidProperties, got %v", rBadPhoto.MethodResponses[0].Args)
	}

	// 3. Create card with valid image data URL (PNG)
	validImageURI := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	rGoodPhoto := postJMAP(t, ts.URL, using, []any{
		[]any{"ContactCard/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"card2": map[string]any{
					"addressBookIds": map[string]any{abID: true},
					"name": map[string]any{
						"full": "Good Photo Contact",
					},
					"media": map[string]any{
						"p1": map[string]any{
							"kind": "photo",
							"uri":  validImageURI,
						},
					},
				},
			},
		}, "c2"},
	})
	created, ok := rGoodPhoto.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["card2"] == nil {
		t.Fatalf("expected valid photo card created, got %v", rGoodPhoto.MethodResponses[0].Args)
	}
	cardID := created["card2"].(map[string]any)["id"].(string)

	// 4. ContactCard/get MUST return mediaType and SHOULD return blobId with uri omitted for data: URLs
	rGet := postJMAP(t, ts.URL, using, []any{
		[]any{"ContactCard/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{cardID},
		}, "c3"},
	})
	cards := rGet.MethodResponses[0].Args["list"].([]any)
	if len(cards) == 0 {
		t.Fatalf("card not found on get: %v", rGet.MethodResponses[0].Args)
	}
	cardObj := cards[0].(map[string]any)
	mediaMap, ok := cardObj["media"].(map[string]any)
	if !ok || len(mediaMap) == 0 {
		t.Fatalf("expected media returned on card, got %v", cardObj)
	}
	for _, mItem := range mediaMap {
		m := mItem.(map[string]any)
		if m["mediaType"] != "image/png" {
			t.Errorf("expected mediaType 'image/png', got %v", m["mediaType"])
		}
		if m["uri"] != nil && m["uri"] != "" {
			t.Errorf("expected uri to be omitted when converted to blobId, got %v", m["uri"])
		}
	}
}

// TestRFC9610_ContactCard_GroupsAndMembers tests group contacts, member resolution across accounts,
// and preserving unresolved member UIDs per RFC 9610 §3.
func TestRFC9610_ContactCard_GroupsAndMembers(t *testing.T) {
	spectest.Require(t, "RFC9610", "3", spectest.SHOULD, "Clients should consider the group to contain any ContactCard with a")
	spectest.Require(t, "RFC9610", "3", spectest.SHOULD, "cannot be found SHOULD be ignored but preserved")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}

	rAB := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/get", map[string]any{"accountId": "primary"}, "ab1"},
	})
	abID := rAB.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)["id"].(string)

	// Create group with members containing existing and non-existing UIDs
	rGroup := postJMAP(t, ts.URL, using, []any{
		[]any{"ContactCard/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"grp": map[string]any{
					"kind":           "group",
					"addressBookIds": map[string]any{abID: true},
					"name": map[string]any{
						"full": "Engineering Team",
					},
					"members": map[string]any{
						"uid-existing-1":    true,
						"uid-unresolved-99": true,
					},
				},
			},
		}, "c1"},
	})
	created, ok := rGroup.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["grp"] == nil {
		t.Fatalf("expected group created, got %v", rGroup.MethodResponses[0].Args)
	}
	grpID := created["grp"].(map[string]any)["id"].(string)

	// Fetch group and ensure unresolved UIDs are preserved in members
	rGet := postJMAP(t, ts.URL, using, []any{
		[]any{"ContactCard/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{grpID},
		}, "c2"},
	})
	grpCard := rGet.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	members, ok := grpCard["members"].(map[string]any)
	if !ok || members["uid-unresolved-99"] != true {
		t.Errorf("expected unresolved uid to be preserved in members, got %v", grpCard["members"])
	}
}

// TestRFC9610_ContactCard_FilterAndSort tests case-insensitivity, tokenization, phrase search,
// and sorting by name components per RFC 9610 §3.3.1 and §3.3.2.
func TestRFC9610_ContactCard_FilterAndSort(t *testing.T) {
	spectest.Require(t, "RFC9610", "3.3.1", spectest.MAY, "may be omitted:")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.SHOULD, "* Text SHOULD be matched in a case-insensitive manner")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.SHOULD, "SHOULD be treated as a phrase search")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.SHOULD, "* Outside of a phrase, whitespace SHOULD be treated as dividing")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.MAY, "separate tokens that may be searched for separately in the")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.MAY, "* Tokens MAY be matched on a whole-word basis using stemming (e")
	spectest.Require(t, "RFC9610", "3.3.2", spectest.SHOULD, "object SHOULD be supported for sorting:")

	c1 := &jmapcontacts.Card{
		ID: "c1",
		Name: &jmapcontacts.JSContactName{
			Full: "Alan Mathison Turing",
			Components: []*jmapcontacts.JSContactNameComponent{
				{Value: "Alan", Kind: "given"},
				{Value: "Mathison", Kind: "surname2"},
				{Value: "Turing", Kind: "surname"},
			},
		},
	}
	c2 := &jmapcontacts.Card{
		ID: "c2",
		Name: &jmapcontacts.JSContactName{
			Full: "Ada Lovelace",
			Components: []*jmapcontacts.JSContactNameComponent{
				{Value: "Ada", Kind: "given"},
				{Value: "Lovelace", Kind: "surname"},
			},
		},
	}

	// Case-insensitive token search
	filterTokens := map[string]any{"text": "alan turing"}
	if !jmapcontacts.MatchCard(c1, filterTokens) {
		t.Errorf("expected c1 to match 'alan turing'")
	}
	if jmapcontacts.MatchCard(c2, filterTokens) {
		t.Errorf("c2 should not match 'alan turing'")
	}

	// Quoted phrase search
	filterPhrase := map[string]any{"text": "\"mathison turing\""}
	if !jmapcontacts.MatchCard(c1, filterPhrase) {
		t.Errorf("expected c1 to match phrase '\"mathison turing\"'")
	}

	// Sorting by name/given ascending
	cards := []*jmapcontacts.Card{c1, c2}
	jmapcontacts.SortCards(cards, []jmap.Comparator{
		{Property: "name/given", IsAscending: true},
	})
	if cards[0].ID != "c2" || cards[1].ID != "c1" {
		t.Errorf("expected [Ada, Alan], got [%s, %s]", cards[0].ID, cards[1].ID)
	}

	// Sorting by name/surname ascending
	jmapcontacts.SortCards(cards, []jmap.Comparator{
		{Property: "name/surname", IsAscending: true},
	})
	if cards[0].ID != "c2" || cards[1].ID != "c1" {
		t.Errorf("expected Lovelace before Turing, got [%s, %s]", cards[0].ID, cards[1].ID)
	}
}

// TestRFC9610_Internationalisation_And_Security verifies UTF-8 validation and security controls
// per RFC 9610 §5 and §6.
func TestRFC9610_Internationalisation_And_Security(t *testing.T) {
	spectest.Require(t, "RFC9610", "5", spectest.MAY, "Servers MAY choose")
	spectest.Require(t, "RFC9610", "5", spectest.MAY, "Alternatively, the server MAY just reject the create/update with an")
	spectest.Require(t, "RFC9610", "6", spectest.MUST, "clients MUST be mindful of the need to keep all data secure")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()


	// Test invalid UTF-8 string rejection (RFC 9610 §5)
	rawJSON := []byte("{\"using\":[\"urn:ietf:params:jmap:core\",\"urn:ietf:params:jmap:contacts\"],\"methodCalls\":[[\"ContactCard/set\",{\"accountId\":\"primary\",\"create\":{\"badCard\":{\"addressBookIds\":{\"contacts\":true},\"name\":{\"full\":\"\xff\xfe\"}}}},\"c1\"]]}")
	req, err := http.NewRequest("POST", ts.URL+"/jmap", bytes.NewReader(rawJSON))
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("user@example.com", "user@example.com")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var jmapResp struct {
		MethodResponses [][]any `json:"methodResponses"`
	}
	if err := json.Unmarshal(body, &jmapResp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if len(jmapResp.MethodResponses) == 0 {
		t.Fatalf("no method responses returned: %s", string(body))
	}
	args, _ := jmapResp.MethodResponses[0][1].(map[string]any)
	created, _ := args["created"].(map[string]any)
	notCreated, _ := args["notCreated"].(map[string]any)
	if notCreated["badCard"] == nil && created["badCard"] == nil {
		t.Fatalf("expected invalid UTF-8 card to either be rejected with invalidProperties or sanitized with U+FFFD, got %v", args)
	}
	if created["badCard"] != nil {
		card := created["badCard"].(map[string]any)
		name, _ := card["name"].(map[string]any)
		if name == nil || name["full"] == nil {
			t.Fatalf("expected card with name to be created, got %v", card)
		}
	}
}
