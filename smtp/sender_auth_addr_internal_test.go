package smtp

import (
	"strings"
	"testing"
)

func TestAuthenticationResultsHeader_None(t *testing.T) {
	hdr := (&SenderAuthResult{}).AuthenticationResultsHeader("mx.example.com", "")
	if !strings.HasPrefix(hdr, "Authentication-Results: mx.example.com;") {
		t.Errorf("unexpected prefix: %q", hdr)
	}
	if !strings.Contains(hdr, "; none") {
		t.Errorf("expected a 'none' result, got: %q", hdr)
	}
	if !strings.HasSuffix(hdr, "\r\n") {
		t.Errorf("header field must end with CRLF, got: %q", hdr)
	}
	// The trace header is prepended to the message, so it must not include the
	// blank line that terminates the header block.
	if strings.Contains(hdr, "\r\n\r\n") {
		t.Errorf("header must not contain a blank-line terminator: %q", hdr)
	}
}

func TestAddressDomain(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"alice@external.org", "external.org"},
		{"ALICE@EXTERNAL.ORG", "external.org"},
		{"mailto:alice@external.org", "external.org"},
		{"client.external.org", "client.external.org"},
		{"", ""},
		{"not a domain", ""},
		{"user@", ""},
	}
	for _, tc := range tests {
		if got := addressDomain(tc.in); got != tc.want {
			t.Errorf("addressDomain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOrganizationalDomain_FallbackToInput(t *testing.T) {
	// Domains with no registrable public suffix fall back to the input.
	for _, in := range []string{"localhost", "com", "example.invalid"} {
		if got := organizationalDomain(in); got != in {
			t.Errorf("organizationalDomain(%q) = %q, want %q", in, got, in)
		}
	}
	if got := organizationalDomain(""); got != "" {
		t.Errorf("organizationalDomain(%q) = %q, want empty", "", got)
	}
}

func TestEmailAddressMatches(t *testing.T) {
	tests := []struct {
		from, auth string
		want       bool
	}{
		{"alice@example.com", "alice@example.com", true},
		{"alice@example.com", "alice@EXAMPLE.COM", true},
		{"Alice@example.com", "alice@example.com", false},
		{"alice@example.com", "bob@example.com", false},
		{"alice@example.com", "alice@other.com", false},
		{"", "alice@example.com", false},
		{"alice@example.com", "", false},
		{"not-an-address", "alice@example.com", false},
	}
	for _, tc := range tests {
		if got := emailAddressMatches(tc.from, tc.auth); got != tc.want {
			t.Errorf("emailAddressMatches(%q, %q) = %v, want %v", tc.from, tc.auth, got, tc.want)
		}
	}
}

func TestSanitizeEnvelope(t *testing.T) {
	tests := []struct {
		addr, fallback, want string
	}{
		{"alice@example.com", "fallback@example.com", "alice@example.com"},
		{"  alice@example.com  ", "fallback@example.com", "alice@example.com"},
		{"alice@example.com\r\nBcc: evil@example.com", "fallback@example.com", "fallback@example.com"},
		{"alice@example.com\nBcc: evil@example.com", "fallback@example.com", "fallback@example.com"},
		{"alice @example.com", "fallback@example.com", "fallback@example.com"},
		{"", "fallback@example.com", "fallback@example.com"},
	}
	for _, tc := range tests {
		if got := sanitizeEnvelope(tc.addr, tc.fallback); got != tc.want {
			t.Errorf("sanitizeEnvelope(%q, %q) = %q, want %q", tc.addr, tc.fallback, got, tc.want)
		}
	}
}
