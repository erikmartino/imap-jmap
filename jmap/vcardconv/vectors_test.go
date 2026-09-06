package vcardconv

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"imap-jmap/jmap/spectest"
)

type testVector struct {
	ID            string         `json:"id"`
	JProps        map[string]any `json:"jprops"`
	SkipToVCard   bool           `json:"skip_to_vcard"`
	SkipFromVCard bool           `json:"skip_from_vcard"`
	InvalidProps  []string       `json:"invalid_props"`
}

func TestJSContactSuiteVectors(t *testing.T) {
	spectest.Require(t, "RFC9555", "1", spectest.MUST, "JSContact to and from vCard conversion")
	spectest.Require(t, "RFC9554", "1", spectest.MUST, "vCard format extensions for JSContact")
	spectest.Require(t, "RFC9553", "1", spectest.MUST, "JSContact Card specification")

	data, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatalf("read testdata/vectors.json: %v", err)
	}
	var vectors []testVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("parse testdata/vectors.json: %v", err)
	}

	for _, tc := range vectors {
		t.Run(tc.ID, func(t *testing.T) {
			jscard := deepCopyMap(tc.JProps)
			if _, ok := jscard["@type"]; !ok {
				jscard["@type"] = "Card"
			}
			if _, ok := jscard["uid"]; !ok {
				jscard["uid"] = "urn:uuid:03a0e51f-d1aa-4385-8a53-e29025acd8af"
			}
			if _, ok := jscard["version"]; !ok {
				jscard["version"] = "1.0"
			}

			if tc.SkipToVCard {
				return
			}

			vcf, err := ToVCard(jscard)
			if len(tc.InvalidProps) > 0 {
				if err == nil {
					t.Fatalf("expected error for invalid card properties %v, got success", tc.InvalidProps)
				}
				return
			}
			if err != nil {
				t.Fatalf("ToVCard failed: %v", err)
			}

			if tc.SkipFromVCard {
				return
			}

			gotJSCard, err := FromVCard(vcf)
			if err != nil {
				t.Fatalf("FromVCard failed: %v\nGenerated vCard:\n%s", err, vcf)
			}

			want := deepCopyMap(jscard)
			have := deepCopyMap(gotJSCard)

			// Remove server-synthesized or non-asserted properties
			if _, ok := want["created"]; !ok {
				delete(have, "created")
			}
			if _, ok := want["updated"]; !ok {
				delete(have, "updated")
			}
			if _, ok := want["prodId"]; !ok {
				delete(have, "prodId")
			}
			if _, ok := want["vCardProps"]; !ok {
				delete(have, "vCardProps")
			}

			normalizeJSCard(want)
			normalizeJSCard(have)

			// Check localizations: apply patch and compare patched cards
			wantLoc, _ := want["localizations"].(map[string]any)
			haveLoc, _ := have["localizations"].(map[string]any)
			delete(want, "localizations")
			delete(have, "localizations")

			if !reflect.DeepEqual(want, have) {
				wantJSON, _ := json.MarshalIndent(want, "", "  ")
				haveJSON, _ := json.MarshalIndent(have, "", "  ")
				t.Fatalf("Card mismatch:\nWANT:\n%s\nHAVE:\n%s\nVCARD:\n%s", wantJSON, haveJSON, vcf)
			}

			if wantLoc != nil || haveLoc != nil {
				if len(wantLoc) != len(haveLoc) {
					t.Fatalf("localizations count mismatch: want %d, have %d", len(wantLoc), len(haveLoc))
				}
				for lang, wantPatch := range wantLoc {
					havePatch, ok := haveLoc[lang]
					if !ok {
						t.Fatalf("missing localization for %q", lang)
					}
					wp, _ := wantPatch.(map[string]any)
					hp, _ := havePatch.(map[string]any)

					patchedWant, err := applyPatch(want, wp)
					if err != nil {
						t.Fatalf("applyPatch want: %v", err)
					}
					patchedHave, err := applyPatch(have, hp)
					if err != nil {
						t.Fatalf("applyPatch have: %v", err)
					}

					normalizeJSCard(patchedWant)
					normalizeJSCard(patchedHave)

					if !reflect.DeepEqual(patchedWant, patchedHave) {
						wJSON, _ := json.MarshalIndent(patchedWant, "", "  ")
						hJSON, _ := json.MarshalIndent(patchedHave, "", "  ")
						t.Fatalf("localized card mismatch for %q:\nWANT:\n%s\nHAVE:\n%s", lang, wJSON, hJSON)
					}
				}
			}
		})
	}
}

func deepCopyMap(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func normalizeJSCard(m map[string]any) {
	if m == nil {
		return
	}
	// Strip default @type
	delete(m, "@type")
	if kind, ok := m["kind"].(string); ok && strings.EqualFold(kind, "individual") {
		delete(m, "kind")
	}

	cleanSection(m, "addresses", "Address")
	cleanSection(m, "emails", "EmailAddress")
	cleanSection(m, "phones", "Phone")
	cleanSection(m, "onlineServices", "OnlineService")
	cleanSection(m, "links", "Link")
	cleanSection(m, "calendars", "Calendar")
	cleanSection(m, "schedulingAddresses", "SchedulingAddress")
	cleanSection(m, "nicknames", "Nickname")
	cleanSection(m, "notes", "Note")
	cleanSection(m, "organizations", "Organization")
	cleanSection(m, "personalInfo", "PersonalInfo")
	cleanSection(m, "preferredLanguages", "LanguagePref")
	cleanSection(m, "relatedTo", "Relation")
	cleanSection(m, "anniversaries", "Anniversary")
	cleanSection(m, "titles", "Title")
	cleanSection(m, "directories", "Directory")
	cleanSection(m, "cryptoKeys", "CryptoKey")
	cleanSection(m, "media", "Media")

	if titles, ok := m["titles"].(map[string]any); ok {
		for _, t := range titles {
			if tm, ok := t.(map[string]any); ok {
				if k, ok := tm["kind"].(string); ok && k == "title" {
					delete(tm, "kind")
				}
			}
		}
	}

	if name, ok := m["name"].(map[string]any); ok {
		delete(name, "@type")
		if ord, ok := name["isOrdered"].(bool); ok && !ord {
			delete(name, "isOrdered")
		}
		if comps, ok := name["components"].([]any); ok {
			for _, c := range comps {
				if cm, ok := c.(map[string]any); ok {
					delete(cm, "@type")
				}
			}
			if ord, _ := name["isOrdered"].(bool); !ord {
				sortComponents(comps)
			}
		}
		if len(name) == 0 {
			delete(m, "name")
		}
	}

	if addrs, ok := m["addresses"].(map[string]any); ok {
		for _, a := range addrs {
			if am, ok := a.(map[string]any); ok {
				delete(am, "@type")
				if ord, ok := am["isOrdered"].(bool); ok && !ord {
					delete(am, "isOrdered")
				}
				if comps, ok := am["components"].([]any); ok {
					for _, c := range comps {
						if cm, ok := c.(map[string]any); ok {
							delete(cm, "@type")
						}
					}
					if ord, _ := am["isOrdered"].(bool); !ord {
						sortComponents(comps)
					}
				}
			}
		}
	}

	if anns, ok := m["anniversaries"].(map[string]any); ok {
		for _, a := range anns {
			if am, ok := a.(map[string]any); ok {
				if date, ok := am["date"].(map[string]any); ok {
					if t, ok := date["@type"].(string); ok && t == "PartialDate" {
						delete(date, "@type")
					}
				}
			}
		}
	}

	// Clean vCardParams
	cleanVCardParams(m)
}

func cleanSection(m map[string]any, secName, expectedType string) {
	sec, ok := m[secName].(map[string]any)
	if !ok {
		return
	}
	for _, v := range sec {
		if vm, ok := v.(map[string]any); ok {
			if t, ok := vm["@type"].(string); ok && t == expectedType {
				delete(vm, "@type")
			}
		}
	}
}

func cleanVCardParams(obj any) {
	if m, ok := obj.(map[string]any); ok {
		if vp, ok := m["vCardParams"].(map[string]any); ok {
			delete(vp, "group")
			if len(vp) == 0 {
				delete(m, "vCardParams")
			}
		}
		for _, v := range m {
			cleanVCardParams(v)
		}
	} else if list, ok := obj.([]any); ok {
		for _, item := range list {
			cleanVCardParams(item)
		}
	}
}

func sortComponents(comps []any) {
	sort.SliceStable(comps, func(i, j int) bool {
		bi, _ := json.Marshal(comps[i])
		bj, _ := json.Marshal(comps[j])
		return string(bi) < string(bj)
	})
}
