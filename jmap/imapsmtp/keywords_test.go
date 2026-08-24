package imapsmtp

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestTagKeywordFlagRoundTrip(t *testing.T) {
	keywords := map[string]bool{
		"$seen":                  true,
		"$tag/finance":           true,
		"$tag/priority/high":     true,
		"$tag/my~20tag":          true,
		"$tag/foo/bar~2fbaz":     true,
		"$tag/caf~c3~a9":         true,
		"$draft":                 false, // must be dropped
	}

	flags := MapKeywordsToIMAPFlags(keywords)
	// $tag/... keywords must survive verbatim as IMAP keywords (order is not
	// guaranteed — the mapper iterates a map).
	wantFlags := map[imap.Flag]bool{
		imap.FlagSeen:                       true,
		imap.Flag("$tag/finance"):           true,
		imap.Flag("$tag/priority/high"):     true,
		imap.Flag("$tag/my~20tag"):          true,
		imap.Flag("$tag/foo/bar~2fbaz"):     true,
		imap.Flag("$tag/caf~c3~a9"):         true,
	}
	if len(flags) != len(wantFlags) {
		t.Fatalf("MapKeywordsToIMAPFlags = %v, want %d flags", flags, len(wantFlags))
	}
	for _, f := range flags {
		if !wantFlags[f] {
			t.Errorf("MapKeywordsToIMAPFlags produced unexpected flag %q", f)
		}
	}

	back := MapIMAPFlagsToKeywords(flags)
	for _, want := range []string{"$seen", "$tag/finance", "$tag/priority/high", "$tag/my~20tag", "$tag/foo/bar~2fbaz", "$tag/caf~c3~a9"} {
		if !back[want] {
			t.Errorf("MapIMAPFlagsToKeywords lost keyword %q (got %v)", want, back)
		}
	}
	if back["$draft"] {
		t.Error("$draft should not be present")
	}
}
