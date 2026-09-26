package jmapmail

import (
	"strings"
	"testing"
)

func TestGenerateMessageID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantDom string
	}{
		{name: "standard email", input: "user@example.com", wantDom: "example.com"},
		{name: "email with mailto", input: "mailto:organizer@gmail.com", wantDom: "gmail.com"},
		{name: "angle bracket email", input: "<sender@my-domain.org>", wantDom: "my-domain.org"},
		{name: "domain only", input: "custom-host.net", wantDom: "custom-host.net"},
		{name: "empty string fallback", input: "", wantDom: "localhost"},
		{name: "malformed address falls back to localhost", input: "bad;user@dom ain!.com>", wantDom: "localhost"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msgID := GenerateMessageID(tc.input)
			if !strings.HasSuffix(msgID, "@"+tc.wantDom) {
				t.Errorf("expected suffix @%s, got %s", tc.wantDom, msgID)
			}
			wrapped := "<" + msgID + ">"
			if !MsgIDRegex.MatchString(wrapped) {
				t.Errorf("wrapped message ID %s does not match RFC 5322 regex", wrapped)
			}
			// Must not contain spaces or angle brackets in JMAP representation
			if strings.ContainsAny(msgID, "<> \t\r\n") {
				t.Errorf("generated MessageID %q contains invalid characters", msgID)
			}
		})
	}
}

func TestHasValidMessageID(t *testing.T) {
	valid := []byte("From: alice@example.com\r\nMessage-ID: <msg-123@example.com>\r\nSubject: Test\r\n\r\nHello")
	if !HasValidMessageID(valid) {
		t.Errorf("expected valid message to report true")
	}

	missing := []byte("From: alice@example.com\r\nSubject: Test\r\n\r\nHello")
	if HasValidMessageID(missing) {
		t.Errorf("expected missing Message-ID to report false")
	}

	invalidNoAngle := []byte("From: alice@example.com\r\nMessage-ID: msg-123@example.com\r\nSubject: Test\r\n\r\nHello")
	if HasValidMessageID(invalidNoAngle) {
		t.Errorf("expected Message-ID without angle brackets to report false")
	}

	invalidWhitespace := []byte("From: alice@example.com\r\nMessage-ID: <msg 123@example.com>\r\nSubject: Test\r\n\r\nHello")
	if HasValidMessageID(invalidWhitespace) {
		t.Errorf("expected Message-ID with whitespace to report false")
	}
}

func TestDomainFromMailboxOrDomain(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"user@example.com", "example.com"},
		{"mailto:organizer@gmail.com", "gmail.com"},
		{"<sender@my-domain.org>", "my-domain.org"},
		{"custom-host.net", "custom-host.net"},
		{"bad;user@dom ain!.com>", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := domainFromMailboxOrDomain(tc.in); got != tc.want {
			t.Errorf("domainFromMailboxOrDomain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStripBCCHeader(t *testing.T) {
	t.Run("removes bcc and preserves other headers and body", func(t *testing.T) {
		raw := []byte("From: alice@example.com\r\nBcc: secret@example.com\r\nTo: bob@example.com\r\nSubject: Hi\r\n\r\nBody line\r\n")
		got := StripBCCHeader(raw)
		if strings.Contains(strings.ToLower(string(got)), "\r\nbcc:") {
			t.Fatalf("Bcc header not removed:\n%s", string(got))
		}
		for _, want := range []string{"From: alice@example.com", "To: bob@example.com", "Subject: Hi", "Body line"} {
			if !strings.Contains(string(got), want) {
				t.Errorf("expected %q to survive:\n%s", want, string(got))
			}
		}
	})

	t.Run("removes folded bcc continuation lines", func(t *testing.T) {
		raw := []byte("From: alice@example.com\r\nBcc: first@example.com,\r\n second@example.com\r\nSubject: Hi\r\n\r\nBody\r\n")
		got := StripBCCHeader(raw)
		lower := strings.ToLower(string(got))
		if strings.Contains(lower, "bcc:") || strings.Contains(lower, "second@example.com") {
			t.Fatalf("folded Bcc not fully removed:\n%s", string(got))
		}
		if !strings.Contains(string(got), "Subject: Hi") {
			t.Errorf("expected Subject to survive:\n%s", string(got))
		}
	})

	t.Run("no bcc returns input unchanged", func(t *testing.T) {
		raw := []byte("From: alice@example.com\r\nSubject: Hi\r\n\r\nBody\r\n")
		if got := StripBCCHeader(raw); string(got) != string(raw) {
			t.Errorf("expected unchanged message, got:\n%s", string(got))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		if got := StripBCCHeader(nil); len(got) != 0 {
			t.Errorf("expected empty output, got %q", string(got))
		}
	})
}

func TestEnsureValidMessageID(t *testing.T) {
	// 1. Missing Message-ID: header added
	missing := []byte("From: alice@example.com\r\nTo: bob@example.com\r\n\r\nBody")
	ensured := EnsureValidMessageID(missing, "alice@example.com")
	if !HasValidMessageID(ensured) {
		t.Fatalf("expected ensured message to have valid Message-ID, got:\n%s", string(ensured))
	}
	if !strings.Contains(string(ensured), "@example.com>") {
		t.Errorf("expected Message-ID with domain example.com, got:\n%s", string(ensured))
	}

	// 2. Already valid Message-ID: unchanged
	valid := []byte("From: alice@example.com\r\nMessage-ID: <orig-123@example.com>\r\n\r\nBody")
	unchanged := EnsureValidMessageID(valid, "alice@example.com")
	if string(unchanged) != string(valid) {
		t.Errorf("expected valid Message-ID to remain unchanged, got:\n%s", string(unchanged))
	}

	// 3. Invalid Message-ID: replaced
	invalid := []byte("From: alice@example.com\r\nMessage-ID: not-valid-syntax\r\nTo: bob@example.com\r\n\r\nBody")
	replaced := EnsureValidMessageID(invalid, "alice@example.com")
	if !HasValidMessageID(replaced) {
		t.Fatalf("expected replaced message to have valid Message-ID, got:\n%s", string(replaced))
	}
	if strings.Contains(string(replaced), "not-valid-syntax") {
		t.Errorf("expected invalid Message-ID to be stripped, got:\n%s", string(replaced))
	}
	if !strings.Contains(string(replaced), "To: bob@example.com") {
		t.Errorf("expected other headers to be preserved, got:\n%s", string(replaced))
	}
}

func TestEnsureValidMessageID_EdgeCases(t *testing.T) {
	t.Run("empty input yields header block with message-id", func(t *testing.T) {
		got := EnsureValidMessageID(nil, "alice@example.com")
		if !HasValidMessageID(got) {
			t.Fatalf("expected valid Message-ID, got:\n%s", string(got))
		}
	})

	t.Run("malformed header block prepends message-id", func(t *testing.T) {
		raw := []byte("this is not a header\r\n\r\nBody")
		got := EnsureValidMessageID(raw, "alice@example.com")
		if !HasValidMessageID(got) {
			t.Fatalf("expected valid Message-ID, got:\n%s", string(got))
		}
		if !strings.Contains(string(got), "this is not a header") {
			t.Errorf("expected original content preserved, got:\n%s", string(got))
		}
	})

	t.Run("preserves body across lf-only line endings", func(t *testing.T) {
		raw := []byte("From: alice@example.com\nSubject: Hi\n\nBody with LF endings")
		got := EnsureValidMessageID(raw, "alice@example.com")
		if !HasValidMessageID(got) {
			t.Fatalf("expected valid Message-ID, got:\n%s", string(got))
		}
		if !strings.Contains(string(got), "Body with LF endings") {
			t.Errorf("expected body preserved, got:\n%s", string(got))
		}
	})
}
