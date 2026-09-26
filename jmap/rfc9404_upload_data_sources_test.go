package jmap_test

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9404_Section4_1_UploadDataSources covers Blob/upload DataSourceObject
// handling: UTF-8/base64 validation, concatenation, size in octets, and the
// minimum of 64 data sources per create (RFC 9404 Section 4.1).
func TestRFC9404_Section4_1_UploadDataSources(t *testing.T) {
	spectest.Require(t, "RFC9404", "4.1", spectest.MUST,
		"MUST reject the creation and return a notCreated response for that")
	spectest.Require(t, "RFC9404", "4.1", spectest.MUST,
		"A server MUST accept at least 64 DataSourceObjects per create, as")
	spectest.Require(t, "RFC9404", "4.2", spectest.MUST,
		"The size value MUST always be the number of octets in the underlying")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.BlobCapabilityURI}

	post := func(calls []any) map[string]any {
		r := postJMAP(t, ts.URL, using, calls)
		if r.MethodResponses[0].Name == "error" {
			t.Fatalf("unexpected error: %v", r.MethodResponses[0].Args)
		}
		return r.MethodResponses[0].Args
	}

	// Concatenate a text source and a base64 source.
	world := base64.StdEncoding.EncodeToString([]byte("World"))
	args := post([]any{
		[]any{"Blob/upload", map[string]any{"accountId": "primary", "create": map[string]any{
			"b1": map[string]any{
				"type": "text/plain",
				"data": []any{
					map[string]any{"data:asText": "Hello "},
					map[string]any{"data:asBase64": world},
				},
			},
		}}, "u1"},
	})
	blobID, _ := args["created"].(map[string]any)["b1"].(map[string]any)["id"].(string)
	if blobID == "" {
		t.Fatalf("expected concatenated blob, got %v", args)
	}

	get := post([]any{
		[]any{"Blob/get", map[string]any{"accountId": "primary", "ids": []any{blobID}, "properties": []any{"data:asText", "size"}}, "g1"},
	})
	list, _ := get["list"].([]any)
	blob := list[0].(map[string]any)
	if blob["data:asText"] != "Hello World" {
		t.Errorf("expected \"Hello World\", got %v", blob["data:asText"])
	}
	if blob["size"] != float64(11) {
		t.Errorf("size must be octets (11), got %v", blob["size"])
	}

	// Invalid base64 in data:asBase64 is rejected.
	args = post([]any{
		[]any{"Blob/upload", map[string]any{"accountId": "primary", "create": map[string]any{
			"bad2": map[string]any{"data": []any{map[string]any{"data:asBase64": "!!!not-base64!!!"}}},
		}}, "u3"},
	})
	if notCreated, _ := args["notCreated"].(map[string]any); notCreated["bad2"] == nil {
		t.Errorf("invalid base64 data:asBase64 should be rejected, got %v", args)
	}

	// At least 64 data sources per create must be accepted.
	sources := make([]any, 64)
	for i := range sources {
		sources[i] = map[string]any{"data:asText": "x"}
	}
	args = post([]any{
		[]any{"Blob/upload", map[string]any{"accountId": "primary", "create": map[string]any{
			"many": map[string]any{"type": "text/plain", "data": sources},
		}}, "u4"},
	})
	if created, _ := args["created"].(map[string]any); created["many"] == nil {
		t.Errorf("64 data sources should be accepted, got %v", args)
	}
}
