package jmapmail

import "testing"

func decodedEmails(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		m, _ := item.(map[string]any)
		if e, _ := m["email"].(string); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func TestDecodeHeaderAddresses(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"single", "Alice <alice@example.com>", []string{"alice@example.com"}},
		{"multiple", "Alice <alice@example.com>, Bob <bob@example.com>", []string{"alice@example.com", "bob@example.com"}},
		// A colon inside a quoted display name must not be treated as a group separator.
		{"colon in display name", `"Weird: Name" <a@b.com>`, []string{"a@b.com"}},
		// RFC 5322 group syntax: the addresses inside the group are returned.
		{"group", "Friends: alice@example.com, bob@example.com;", []string{"alice@example.com", "bob@example.com"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decodedEmails(decodeHeaderAddresses(tc.raw))
			if len(got) != len(tc.want) {
				t.Fatalf("decodeHeaderAddresses(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("decodeHeaderAddresses(%q)[%d] = %q, want %q", tc.raw, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDecodeHeaderGroupedAddresses(t *testing.T) {
	got := decodeHeaderGroupedAddresses("Friends: alice@example.com;")
	list, _ := got.([]any)
	if len(list) != 1 {
		t.Fatalf("expected one group, got %v", got)
	}
	group, _ := list[0].(map[string]any)
	if name, _ := group["name"].(string); name != "Friends" {
		t.Errorf("group name = %q, want %q", name, "Friends")
	}
	if emails := decodedEmails(group["addresses"]); len(emails) != 1 || emails[0] != "alice@example.com" {
		t.Errorf("group addresses = %v, want [alice@example.com]", emails)
	}
}
