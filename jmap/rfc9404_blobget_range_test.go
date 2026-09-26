package jmap_test

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9404_Section4_2_BlobGetRangeAndEncoding covers Blob/get offset/length
// truncation and the encoding-problem rules (RFC 9404 Section 4.2).
func TestRFC9404_Section4_2_BlobGetRangeAndEncoding(t *testing.T) {
	spectest.Require(t, "RFC9404", "4.2", spectest.MUST,
		"isTruncated property in the result MUST be set to true to tell the")
	spectest.Require(t, "RFC9404", "4.2", spectest.MUST,
		"isEncodingProblem MUST be set to true, and the data:asText response")
	spectest.Require(t, "RFC9404", "4.2", spectest.MUST, "value MUST be null")
	spectest.Require(t, "RFC9404", "4.2", spectest.MUST,
		"data is not valid UTF-8, then data:asBase64 MUST be returned")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.BlobCapabilityURI}

	upload := func(raw []byte) string {
		args := postJMAP(t, ts.URL, using, []any{
			[]any{"Blob/upload", map[string]any{"accountId": "primary", "create": map[string]any{
				"b": map[string]any{"type": "application/octet-stream", "data": []any{
					map[string]any{"data:asBase64": base64.StdEncoding.EncodeToString(raw)},
				}},
			}}, "u"},
		}).MethodResponses[0].Args
		id, _ := args["created"].(map[string]any)["b"].(map[string]any)["id"].(string)
		if id == "" {
			t.Fatalf("upload failed: %v", args)
		}
		return id
	}

	get := func(id string, extra map[string]any) map[string]any {
		args := map[string]any{"accountId": "primary", "ids": []any{id}}
		for k, v := range extra {
			args[k] = v
		}
		list, _ := postJMAP(t, ts.URL, using, []any{[]any{"Blob/get", args, "g"}}).MethodResponses[0].Args["list"].([]any)
		return list[0].(map[string]any)
	}

	// offset/length truncates and reports isTruncated true; size is the whole blob.
	text := upload([]byte("Hello World"))
	res := get(text, map[string]any{"properties": []any{"data:asText", "size", "isTruncated"}, "offset": 1, "length": 3})
	if res["data:asText"] != "ell" {
		t.Errorf("expected slice \"ell\", got %v", res["data:asText"])
	}
	if res["size"] != float64(11) {
		t.Errorf("size must be the whole blob (11), got %v", res["size"])
	}
	if res["isTruncated"] != true {
		t.Errorf("isTruncated should be true for a length-truncated range, got %v", res["isTruncated"])
	}

	// Non-UTF-8 octets requested via "data": asText null, asBase64 returned.
	binary := upload([]byte{0xff, 0xfe, 0x00})
	res = get(binary, map[string]any{"properties": []any{"data"}})
	if res["data:asText"] != nil {
		t.Errorf("data:asText must be null for non-UTF-8, got %v", res["data:asText"])
	}
	if res["isEncodingProblem"] != true {
		t.Errorf("isEncodingProblem must be true for non-UTF-8, got %v", res["isEncodingProblem"])
	}
	if res["data:asBase64"] == nil || res["data:asBase64"] == "" {
		t.Errorf("data:asBase64 MUST be returned when data is requested for non-UTF-8, got %v", res["data:asBase64"])
	}
}
