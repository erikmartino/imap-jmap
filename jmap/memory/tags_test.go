package memory

import (
	"context"
	"testing"

	"imap-jmap/jmap"
)

// TestTagsFlowOverKeywords proves a JMAP tag (a $tag/<name>[/<value>] keyword)
// round-trips through Email/set-style storage and Email/query filters on the
// memory backend: create a tagged email, find it by hasKeyword with the tag
// (case-insensitive), exclude it with notKeyword, and read the keyword back.
func TestTagsFlowOverKeywords(t *testing.T) {
	mb := NewMemoryBackend()
	ctx := jmap.ContextWithAccountID(context.Background(), "acct-tags")

	valuedTag, err := jmap.TagToKeyword("priority", "high")
	if err != nil {
		t.Fatal(err)
	}
	booleanTag, err := jmap.TagToKeyword("finance", "")
	if err != nil {
		t.Fatal(err)
	}

	created, err := mb.CreateEmail(ctx, &jmap.Email{
		Subject:    "Tagged email",
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Keywords:   map[string]bool{valuedTag: true, booleanTag: true, "$seen": true},
	})
	if err != nil {
		t.Fatal(err)
	}

	// hasKeyword matches the valued tag, even when the filter value is not
	// lowercased by the client.
	ids, total, err := mb.QueryEmails(ctx, map[string]any{"hasKeyword": "$tag/PRIORITY/HIGH"}, nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(ids) != 1 || ids[0] != created.ID {
		t.Fatalf("hasKeyword query = ids %v total %d, want [%s]/1", ids, total, created.ID)
	}

	// notKeyword excludes the same email.
	ids, total, err = mb.QueryEmails(ctx, map[string]any{"notKeyword": "$tag/finance"}, nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("notKeyword query = total %d, want 0", total)
	}

	// The keywords round-trip through Email/get.
	got, notFound, err := mb.GetEmails(ctx, []jmap.Id{created.ID})
	if err != nil || len(notFound) != 0 {
		t.Fatalf("GetEmails err=%v notFound=%v", err, notFound)
	}
	if len(got) != 1 {
		t.Fatalf("GetEmails returned %d emails", len(got))
	}
	for _, kw := range []string{valuedTag, booleanTag} {
		if !got[0].Keywords[kw] {
			t.Errorf("keyword %q lost after round-trip: %v", kw, got[0].Keywords)
		}
	}
}
