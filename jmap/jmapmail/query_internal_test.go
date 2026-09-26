package jmapmail

import (
	"reflect"
	"testing"
)

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
