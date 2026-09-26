package jmapmail

import (
	"reflect"
	"strings"
	"testing"
)

func strptr(s string) *string { return &s }

func TestHTMLSearchText(t *testing.T) {
	in := `<html><head><title>Hidden Head Title</title><style>.x{color:red}</style></head>` +
		`<body><p>Visible text</p><img alt="A cat" src="x"><a title="link title">click</a>` +
		`<script>var z = 1</script></body></html>`
	got := htmlSearchText(in)
	for _, want := range []string{"Visible text", "A cat", "link title", "click"} {
		if !strings.Contains(got, want) {
			t.Errorf("htmlSearchText missing %q in %q", want, got)
		}
	}
	for _, unwanted := range []string{"Hidden Head Title", "script", "var z", "color:red", "<p>"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("htmlSearchText should ignore markup/head/script, found %q in %q", unwanted, got)
		}
	}
}

func TestEmailSearchTextIgnoresMarkup(t *testing.T) {
	em := &Email{
		Subject:  "hi",
		HTMLBody: []EmailBodyPart{{PartID: strptr("h"), Type: "text/html"}},
		BodyValues: map[string]EmailBodyValue{
			"h": {Value: `<div class="hiddenmarker">visibleword</div><img alt="altword">`},
		},
	}
	got := emailSearchText(em)
	if !strings.Contains(got, "visibleword") || !strings.Contains(got, "altword") {
		t.Errorf("expected visible text and alt attribute in search text, got %q", got)
	}
	if strings.Contains(got, "hiddenmarker") {
		t.Errorf("markup attribute names must not be searched, got %q", got)
	}
}

func TestSearchTerms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"wildcard", "core*", []string{"core"}},
		{"phrase", `"quarterly figures"`, []string{"quarterly figures"}},
		{"phrase plus term", `quarterly "core report"`, []string{"quarterly", "core report"}},
		{"apostrophe", "don't", []string{"don't"}},
		{"escaped quotes in phrase", `"a \"quoted\" phrase"`, []string{`a "quoted" phrase`}},
		{"empty", "", nil},
		{"bare wildcard", "*", nil},
		{"single quotes stripped", "'core'", []string{"core"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := searchTerms(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("searchTerms(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestContainsAllTermsPhrase(t *testing.T) {
	haystack := "Quarterly figures inside the Core report"
	if !containsAllTerms(haystack, searchTerms(`"quarterly figures"`)) {
		t.Errorf("quoted phrase should match the exact sequence")
	}
	if containsAllTerms(haystack, searchTerms(`"figures quarterly"`)) {
		t.Errorf("quoted phrase must respect word order")
	}
	if !containsAllTerms(haystack, searchTerms(`quarterly "core report"`)) {
		t.Errorf("a phrase combined with a term should match")
	}
}
