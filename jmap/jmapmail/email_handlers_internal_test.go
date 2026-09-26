package jmapmail

import (
	"testing"
	"unicode/utf8"
)

func TestApplyMaxBodyValueBytesHTMLTag(t *testing.T) {
	html := `<p>Hello</p><a href="https://example.com">link</a>`
	got := ApplyMaxBodyValueBytes(EmailBodyValue{Value: html}, 25)
	if got.Value != "<p>Hello</p>" {
		t.Errorf("expected truncation to back off the unterminated tag, got %q", got.Value)
	}
	if !got.IsTruncated {
		t.Errorf("expected IsTruncated=true")
	}
}

func TestApplyMaxBodyValueBytesPlainAndUTF8(t *testing.T) {
	if got := ApplyMaxBodyValueBytes(EmailBodyValue{Value: "hello world"}, 5); got.Value != "hello" || !got.IsTruncated {
		t.Errorf("plain truncation = %q (truncated=%v), want \"hello\"", got.Value, got.IsTruncated)
	}
	got := ApplyMaxBodyValueBytes(EmailBodyValue{Value: "héllo"}, 2) // would split the é
	if !utf8.ValidString(got.Value) {
		t.Errorf("truncation must produce valid UTF-8, got %q", got.Value)
	}
	if v := ApplyMaxBodyValueBytes(EmailBodyValue{Value: "short"}, 100); v.Value != "short" || v.IsTruncated {
		t.Errorf("no truncation expected, got %q truncated=%v", v.Value, v.IsTruncated)
	}
}
