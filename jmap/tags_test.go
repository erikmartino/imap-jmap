package jmap

import (
	"strings"
	"testing"
)

func TestIsValidJMAPKeyword(t *testing.T) {
	valid := []string{
		"$seen",
		"$draft",
		"$tag/finance",
		"$tag/priority/high",
		"$tag/my~20tag",
		"work",
		"a/b/c",
		"$tag/[a",
		"~01~7f",
	}
	for _, kw := range valid {
		if !IsValidJMAPKeyword(kw) {
			t.Errorf("expected %q to be a valid keyword", kw)
		}
	}

	invalid := []string{
		"",
		"has space",
		"tab\there",
		"100%",
		"(paren)",
		"curly{brace}",
		"square]bracket",
		"star*",
		"quote\"",
		"back\\slash",
		strings.Repeat("a", 256), // too long
		"unicode-é",             // non-ASCII byte
		"control\x01",
	}
	for _, kw := range invalid {
		if IsValidJMAPKeyword(kw) {
			t.Errorf("expected %q to be an invalid keyword", kw)
		}
	}
}

func TestTagEscapeRoundTrip(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"finance", "finance"},
		{"priority", "priority"},
		{"my tag", "my~20tag"},
		{"foo/bar", "foo~2fbar"},
		{"100%", "100~25"},
		{"café", "caf~c3~a9"},
		{"tilde~here", "tilde~7ehere"},
		{"a~b/c d", "a~7eb~2fc~20d"},
	}
	for _, c := range cases {
		got := escapeTagSegment(c.in)
		if got != c.want {
			t.Errorf("escapeTagSegment(%q) = %q, want %q", c.in, got, c.want)
		}
		back, err := unescapeTagSegment(c.want)
		if err != nil || back != c.in {
			t.Errorf("unescapeTagSegment(%q) = %q, %v; want %q", c.want, back, err, c.in)
		}
	}

	if _, err := unescapeTagSegment("bad~"); err == nil {
		t.Error("expected error for dangling escape marker")
	}
	if _, err := unescapeTagSegment("bad~zz"); err == nil {
		t.Error("expected error for invalid hex digit")
	}
}

func TestTagToKeywordAndParse(t *testing.T) {
	cases := []struct {
		name, value string
		want        string
	}{
		{"finance", "", "$tag/finance"},
		{"priority", "high", "$tag/priority/high"},
		{"my tag", "", "$tag/my~20tag"},
		{"foo", "bar/baz", "$tag/foo/bar~2fbaz"},
		{"café", "", "$tag/caf~c3~a9"},
		{"MyProject", "", "$tag/myproject"},
	}
	for _, c := range cases {
		kw, err := TagToKeyword(c.name, c.value)
		if err != nil {
			t.Errorf("TagToKeyword(%q,%q) error: %v", c.name, c.value, err)
			continue
		}
		if kw != c.want {
			t.Errorf("TagToKeyword(%q,%q) = %q, want %q", c.name, c.value, kw, c.want)
		}
		if !IsValidJMAPKeyword(kw) {
			t.Errorf("TagToKeyword(%q,%q) produced invalid keyword %q", c.name, c.value, kw)
		}
		name, value, ok := KeywordToTag(kw)
		if !ok {
			t.Errorf("KeywordToTag(%q) = ok=false", kw)
			continue
		}
		if name != strings.ToLower(c.name) || value != strings.ToLower(c.value) {
			t.Errorf("KeywordToTag(%q) = (%q,%q), want (%q,%q)", kw, name, value, c.name, c.value)
		}
	}

	if _, err := TagToKeyword("", "x"); err == nil {
		t.Error("expected error for empty tag name")
	}
	if _, err := TagToKeyword(strings.Repeat("é", 100), ""); err == nil {
		t.Error("expected error for tag name that overflows 255-byte keyword")
	}
}

func TestKeywordToTagRejectsNonTags(t *testing.T) {
	for _, kw := range []string{"$seen", "work", "notatag", "$tag", "$tag/", "X$tag/a"} {
		if _, _, ok := KeywordToTag(kw); ok {
			t.Errorf("KeywordToTag(%q) unexpectedly succeeded", kw)
		}
	}

	// Lowercasing: an uppercase tag keyword still parses.
	name, value, ok := KeywordToTag("$tag/MyTag/High")
	if !ok || name != "mytag" || value != "high" {
		t.Errorf("KeywordToTag should lowercase: got (%q,%q,%v)", name, value, ok)
	}
}

func TestIsTagKeyword(t *testing.T) {
	if !IsTagKeyword("$tag/finance") {
		t.Error("$tag/finance should be a tag keyword")
	}
	if !IsTagKeyword("$tag/priority/high") {
		t.Error("$tag/priority/high should be a tag keyword")
	}
	for _, kw := range []string{"$tag", "$tag/", "$seen", "work"} {
		if IsTagKeyword(kw) {
			t.Errorf("%q should not be a tag keyword", kw)
		}
	}
}
