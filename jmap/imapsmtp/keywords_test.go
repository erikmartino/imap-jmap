package imapsmtp

import (
	"testing"

	imappkg "imap-jmap/imap"
)

func TestTagKeywordFlagRoundTrip(t *testing.T) {
	keywords := map[string]bool{
		"$seen":              true,
		"$tag/finance":       true,
		"$tag/priority/high": true,
		"$tag/my~20tag":      true,
		"$tag/foo/bar~2fbaz": true,
		"$tag/caf~c3~a9":     true,
		"$draft":             false, // must be dropped
	}

	flags := imappkg.MapKeywordsToFlags(keywords)
	wantFlags := map[string]bool{
		"\\Seen":             true,
		"$tag/finance":       true,
		"$tag/priority/high": true,
		"$tag/my~20tag":      true,
		"$tag/foo/bar~2fbaz": true,
		"$tag/caf~c3~a9":     true,
	}
	if len(flags) != len(wantFlags) {
		t.Fatalf("MapKeywordsToFlags = %v, want %d flags", flags, len(wantFlags))
	}
	for _, f := range flags {
		if !wantFlags[f] {
			t.Errorf("MapKeywordsToFlags produced unexpected flag %q", f)
		}
	}

	back := imappkg.MapFlagsToKeywords(flags)
	for _, want := range []string{"$seen", "$tag/finance", "$tag/priority/high", "$tag/my~20tag", "$tag/foo/bar~2fbaz", "$tag/caf~c3~a9"} {
		if !back[want] {
			t.Errorf("MapFlagsToKeywords lost keyword %q (got %v)", want, back)
		}
	}
}

func TestColorLabelKeywordFlagRoundTrip(t *testing.T) {
	keywords := map[string]bool{
		"$seen":       true,
		"$label:red":  true,
		"$label:blue": true,
		"$flagged":    false,
	}

	flags := imappkg.MapKeywordsToFlags(keywords)
	wantFlags := map[string]bool{
		"\\Seen":      true,
		"$label:red":  true,
		"$label:blue": true,
	}
	if len(flags) != len(wantFlags) {
		t.Fatalf("MapKeywordsToFlags = %v, want %d flags", flags, len(wantFlags))
	}
	for _, f := range flags {
		if !wantFlags[f] {
			t.Errorf("MapKeywordsToFlags produced unexpected flag %q", f)
		}
	}

	back := imappkg.MapFlagsToKeywords(flags)
	if !back["$label:red"] || !back["$label:blue"] || !back["$seen"] {
		t.Errorf("MapFlagsToKeywords lost label keyword, got %v", back)
	}
	if back["$flagged"] {
		t.Error("$flagged should not be present")
	}
}


