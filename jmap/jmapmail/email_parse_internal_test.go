package jmapmail

import "testing"

func TestEnsureCharsetUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults to text/plain utf-8", "", "text/plain; charset=utf-8"},
		{"text without charset gains utf-8", "text/plain", "text/plain; charset=utf-8"},
		{"text with charset preserved", "text/plain; charset=iso-8859-1", "text/plain; charset=iso-8859-1"},
		{"html without charset gains utf-8", "text/html", "text/html; charset=utf-8"},
		{"non-text type unchanged", "application/json", "application/json"},
		{"parameters preserved", "text/plain; format=flowed", "text/plain; charset=utf-8; format=flowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ensureCharsetUTF8(tc.in); got != tc.want {
				t.Errorf("ensureCharsetUTF8(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractDomainFromAddress(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"user@example.com", "example.com"},
		{"USER@EXAMPLE.COM", "example.com"},
		{"alice@sub.example.org", "sub.example.org"},
		{"not-an-address", ""},
		{"user@", ""},
	}
	for _, tc := range tests {
		if got := extractDomainFromAddress(tc.in); got != tc.want {
			t.Errorf("extractDomainFromAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
