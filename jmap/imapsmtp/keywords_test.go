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
}

func TestColorLabelKeywordFlagRoundTrip(t *testing.T) {
	keywords := map[string]bool{
		"$seen":       true,
		"$label:red":  true,
		"$label:blue": true,
		"$flagged":    false,
	}

	flags := MapKeywordsToIMAPFlags(keywords)
	wantFlags := map[imap.Flag]bool{
		imap.FlagSeen:          true,
		imap.Flag("$label:red"):  true,
		imap.Flag("$label:blue"): true,
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
	if !back["$label:red"] || !back["$label:blue"] || !back["$seen"] {
		t.Errorf("MapIMAPFlagsToKeywords lost label keyword, got %v", back)
	}
	if back["$flagged"] {
		t.Error("$flagged should not be present")
	}
}

func TestBuildIMAPSearchCriteria_LabelAndOperator(t *testing.T) {
	// 1. Direct hasKeyword filter
	crit1 := buildIMAPSearchCriteria(map[string]any{"hasKeyword": "$label:red"})
	if len(crit1.Flag) != 1 || crit1.Flag[0] != imap.Flag("$label:red") {
		t.Errorf("expected Flag [$label:red], got %v", crit1.Flag)
	}

	// 2. AND operator: hasKeyword + notKeyword (Bulwark unread tag count query)
	crit2 := buildIMAPSearchCriteria(map[string]any{
		"operator": "AND",
		"conditions": []any{
			map[string]any{"hasKeyword": "$label:red"},
			map[string]any{"notKeyword": "$seen"},
		},
	})
	if len(crit2.Flag) != 1 || crit2.Flag[0] != imap.Flag("$label:red") {
		t.Errorf("expected Flag [$label:red], got %v", crit2.Flag)
	}
	if len(crit2.NotFlag) != 1 || crit2.NotFlag[0] != imap.FlagSeen {
		t.Errorf("expected NotFlag [\\Seen], got %v", crit2.NotFlag)
	}

	// 3. Wildcard term cleaning
	crit3 := buildIMAPSearchCriteria(map[string]any{
		"text":    "welcome* test*",
		"subject": "jmap*",
		"from":    "admin*",
	})
	if len(crit3.Text) != 2 || crit3.Text[0] != "welcome" || crit3.Text[1] != "test" {
		t.Errorf("expected cleaned Text terms [welcome, test], got %v", crit3.Text)
	}
	if len(crit3.Header) != 2 {
		t.Fatalf("expected 2 Header criteria, got %v", crit3.Header)
	}
	if crit3.Header[0].Value != "admin" || crit3.Header[1].Value != "jmap" {
		t.Errorf("expected cleaned header values, got %v", crit3.Header)
	}

	// 4. N-way OR operator
	crit4 := buildIMAPSearchCriteria(map[string]any{
		"operator": "OR",
		"conditions": []any{
			map[string]any{"from": "alice*"},
			map[string]any{"to": "bob*"},
			map[string]any{"subject": "charlie*"},
		},
	})
	if len(crit4.Or) != 1 {
		t.Fatalf("expected top-level Or pair, got %v", crit4.Or)
	}
}

func TestExtractTargetFolders(t *testing.T) {
	allFolders := []string{"INBOX", "Archive", "Sent", "Trash", "Drafts"}

	// 1. inMailboxOtherThan Trash
	trashID := MailboxIDForName("Trash")
	filter := map[string]any{
		"inMailboxOtherThan": []any{string(trashID)},
	}
	res := extractTargetFolders(filter, allFolders)
	if len(res) != 4 {
		t.Fatalf("expected 4 folders, got %v", res)
	}
	for _, f := range res {
		if f == "Trash" {
			t.Errorf("Trash should have been excluded")
		}
	}

	// 2. inMailbox INBOX
	inboxID := MailboxIDForName("INBOX")
	filter2 := map[string]any{
		"inMailbox": string(inboxID),
	}
	res2 := extractTargetFolders(filter2, allFolders)
	if len(res2) != 1 || res2[0] != "INBOX" {
		t.Fatalf("expected [INBOX], got %v", res2)
	}

	// 3. nested in AND
	filter3 := map[string]any{
		"operator": "AND",
		"conditions": []any{
			map[string]any{"inMailbox": string(inboxID)},
			map[string]any{"hasKeyword": "$label:red"},
		},
	}
	res3 := extractTargetFolders(filter3, allFolders)
	if len(res3) != 1 || res3[0] != "INBOX" {
		t.Fatalf("expected [INBOX], got %v", res3)
	}
}
