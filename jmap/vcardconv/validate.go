package vcardconv

import (
	"crypto/rand"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// propSchema describes the RFC 9553 object types and their properties. Each
// property maps to how it is traversed: "leaf" (scalar/opaque value),
// "obj:<Type>" (a single object), "map:<Type>" (an object of id-keyed objects),
// "arr:<Type>" (an array of objects), or "patch" (a localizations patch
// object). This table drives both validation (RFC 9553 §1.7) and the emission
// of unknown/vendor-specific properties via JSPROP.
var propSchema = map[string]map[string]string{
	"Card": {
		"@type":               "leaf",
		"uid":                 "leaf",
		"version":             "leaf",
		"kind":                "leaf",
		"language":            "leaf",
		"prodId":              "leaf",
		"created":             "leaf",
		"updated":             "leaf",
		"keywords":            "leaf",
		"members":             "leaf",
		"vCardProps":          "arr:vcardprops",
		"name":                "obj:Name",
		"speakToAs":           "obj:SpeakToAs",
		"nicknames":           "map:Nickname",
		"organizations":       "map:Organization",
		"titles":              "map:Title",
		"emails":              "map:EmailAddress",
		"phones":              "map:Phone",
		"onlineServices":      "map:OnlineService",
		"links":               "map:Link",
		"calendars":           "map:Calendar",
		"schedulingAddresses": "map:SchedulingAddress",
		"addresses":           "map:Address",
		"directories":         "map:Directory",
		"personalInfo":        "map:PersonalInfo",
		"cryptoKeys":          "map:CryptoKey",
		"media":               "map:Media",
		"notes":               "map:Note",
		"relatedTo":           "map:Relation",
		"preferredLanguages":  "map:LanguagePref",
		"anniversaries":       "map:Anniversary",
		"localizations":       "patch",
	},
	"Name": {
		"@type":           "leaf",
		"full":            "leaf",
		"isOrdered":       "leaf",
		"defaultSeparator": "leaf",
		"phoneticSystem":  "leaf",
		"phoneticScript":  "leaf",
		"vCardParams":     "leaf",
		"components":      "arr:NameComponent",
		"sortAs":          "obj:SortAs",
	},
	"SortAs":        {"surname": "leaf", "given": "leaf"},
	"NameComponent": {"@type": "leaf", "kind": "leaf", "value": "leaf", "phonetic": "leaf", "vCardParams": "leaf"},
	"Address": {
		"@type":            "leaf",
		"full":             "leaf",
		"isOrdered":        "leaf",
		"defaultSeparator": "leaf",
		"phoneticSystem":   "leaf",
		"phoneticScript":   "leaf",
		"street":           "leaf",
		"locality":         "leaf",
		"region":           "leaf",
		"postcode":         "leaf",
		"country":          "leaf",
		"countryCode":      "leaf",
		"coordinates":      "leaf",
		"timeZone":         "leaf",
		"contexts":         "leaf",
		"pref":             "leaf",
		"vCardParams":      "leaf",
		"components":       "arr:AddressComponent",
	},
	"AddressComponent": {"@type": "leaf", "kind": "leaf", "value": "leaf", "vCardParams": "leaf"},
	"Nickname":         {"@type": "leaf", "name": "leaf", "contexts": "leaf", "vCardParams": "leaf"},
	"Organization":     {"@type": "leaf", "name": "leaf", "sortAs": "leaf", "contexts": "leaf", "vCardParams": "leaf", "units": "arr:OrgUnit"},
	"OrgUnit":          {"@type": "leaf", "name": "leaf", "sortAs": "leaf"},
	"Title":            {"@type": "leaf", "kind": "leaf", "name": "leaf", "organizationId": "leaf", "contexts": "leaf", "vCardParams": "leaf"},
	"EmailAddress":     {"@type": "leaf", "address": "leaf", "contexts": "leaf", "pref": "leaf", "label": "leaf", "vCardParams": "leaf"},
	"Phone":            {"@type": "leaf", "number": "leaf", "contexts": "leaf", "features": "leaf", "pref": "leaf", "label": "leaf", "vCardParams": "leaf"},
	"OnlineService":    {"@type": "leaf", "service": "leaf", "user": "leaf", "uri": "leaf", "contexts": "leaf", "label": "leaf", "pref": "leaf", "vCardName": "leaf", "vCardParams": "leaf"},
	"Link":             {"@type": "leaf", "kind": "leaf", "uri": "leaf", "contexts": "leaf", "label": "leaf", "pref": "leaf", "vCardName": "leaf", "vCardParams": "leaf"},
	"Calendar":         {"@type": "leaf", "kind": "leaf", "uri": "leaf", "contexts": "leaf", "mediaType": "leaf", "label": "leaf", "pref": "leaf", "vCardParams": "leaf"},
	"SchedulingAddress": {"@type": "leaf", "uri": "leaf", "contexts": "leaf", "label": "leaf", "pref": "leaf", "vCardParams": "leaf"},
	"Directory":        {"@type": "leaf", "kind": "leaf", "uri": "leaf", "contexts": "leaf", "mediaType": "leaf", "label": "leaf", "pref": "leaf", "vCardParams": "leaf"},
	"PersonalInfo":     {"@type": "leaf", "kind": "leaf", "value": "leaf", "level": "leaf", "listAs": "leaf", "vCardParams": "leaf"},
	"CryptoKey":        {"@type": "leaf", "kind": "leaf", "uri": "leaf", "contexts": "leaf", "mediaType": "leaf", "label": "leaf", "pref": "leaf", "vCardParams": "leaf"},
	"Media":            {"@type": "leaf", "blobId": "leaf", "uri": "leaf", "contexts": "leaf", "mediaType": "leaf", "kind": "leaf", "label": "leaf", "pref": "leaf", "vCardName": "leaf", "vCardParams": "leaf"},
	"Note":             {"@type": "leaf", "note": "leaf", "created": "leaf", "author": "obj:Author", "vCardParams": "leaf"},
	"Author":           {"@type": "leaf", "name": "leaf", "uri": "leaf"},
	"Relation":         {"@type": "leaf", "relation": "leaf", "label": "leaf", "vCardParams": "leaf"},
	"LanguagePref":     {"@type": "leaf", "language": "leaf", "contexts": "leaf", "pref": "leaf", "vCardParams": "leaf"},
	"SpeakToAs":        {"@type": "leaf", "grammaticalGender": "leaf", "pronouns": "map:Pronouns", "vCardParams": "leaf"},
	"Pronouns":         {"@type": "leaf", "pronouns": "leaf", "pref": "leaf"},
	"Anniversary":      {"@type": "leaf", "kind": "leaf", "date": "obj:Date", "place": "obj:Address", "vCardParams": "leaf"},
	"Date":             {"@type": "leaf", "year": "leaf", "month": "leaf", "day": "leaf", "utc": "leaf"},
	"vcardprops":       {"group": "leaf"},
}

// ValidateCard checks a JSContact Card against the RFC 9553 §1.7 rules:
// reserved properties ("extra") and case-variants of known properties are
// invalid; unknown and vendor-specific properties are preserved. The
// localizations patches must not address missing intermediate objects.
func ValidateCard(card map[string]any) error {
	if t, ok := card["@type"].(string); ok && t != "" && t != "Card" {
		return &ValidationError{Path: "@type", Reason: "must be \"Card\""}
	}
	return validateObject(card, "Card", "")
}

func validateObject(obj map[string]any, typeName, path string) error {
	known := propSchema[typeName]
	for key := range obj {
		childPath := key
		if path != "" {
			childPath = path + "/" + key
		}
		kind, ok := known[key]
		if !ok {
			if key == "extra" {
				return &ValidationError{Path: childPath, Reason: "reserved property"}
			}
			for k := range known {
				if strings.EqualFold(key, k) {
					return &ValidationError{Path: childPath, Reason: fmt.Sprintf("invalid case for property %q", k)}
				}
			}
			if strings.EqualFold(key, "extra") {
				return &ValidationError{Path: childPath, Reason: "reserved property"}
			}
			// Unknown (syntactically valid) or vendor-specific properties are
			// preserved, never rejected (RFC 9553 §1.7.4, §1.8.1).
			continue
		}
		value := obj[key]
		switch {
		case kind == "leaf":
		case strings.HasPrefix(kind, "obj:"):
			sub, ok := value.(map[string]any)
			if !ok {
				return &ValidationError{Path: childPath, Reason: "must be an object"}
			}
			if err := validateObject(sub, strings.TrimPrefix(kind, "obj:"), childPath); err != nil {
				return err
			}
		case strings.HasPrefix(kind, "map:"):
			m, ok := value.(map[string]any)
			if !ok {
				return &ValidationError{Path: childPath, Reason: "must be an object of objects"}
			}
			typeName := strings.TrimPrefix(kind, "map:")
			for id := range m {
				sub, ok := m[id].(map[string]any)
				if !ok {
					return &ValidationError{Path: childPath + "/" + id, Reason: "must be an object"}
				}
				if err := validateObject(sub, typeName, childPath+"/"+id); err != nil {
					return err
				}
			}
		case strings.HasPrefix(kind, "arr:"):
			arr, ok := value.([]any)
			if !ok {
				return &ValidationError{Path: childPath, Reason: "must be an array"}
			}
			typeName := strings.TrimPrefix(kind, "arr:")
			if typeName == "vcardprops" {
				for i, el := range arr {
					prop, ok := el.([]any)
					if !ok || len(prop) < 4 {
						return &ValidationError{Path: fmt.Sprintf("%s/%d", childPath, i), Reason: "must be a vCard property tuple"}
					}
				}
				continue
			}
			for i, el := range arr {
				sub, ok := el.(map[string]any)
				if !ok {
					return &ValidationError{Path: fmt.Sprintf("%s/%d", childPath, i), Reason: "must be an object"}
				}
				if err := validateObject(sub, typeName, fmt.Sprintf("%s/%d", childPath, i)); err != nil {
					return err
				}
			}
		}
	}
	return validateLocalizations(obj, path)
}

// validateLocalizations checks that every patch path in localizations
// resolves to an existing intermediate object and does not create a
// reserved or case-violating leaf property (RFC 9553 §2.7.1).
func validateLocalizations(obj map[string]any, path string) error {
	langs, ok := mapField(obj, "localizations")
	if !ok {
		return nil
	}
	for lang, raw := range langs {
		patch, ok := raw.(map[string]any)
		if !ok {
			return &ValidationError{Path: path + "/localizations/" + lang, Reason: "patch must be an object"}
		}
		for ptr := range patch {
			segs := splitPointer(ptr)
			if err := validatePatchPath(obj, segs, joinPath(path, "localizations/"+lang)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePatchPath(root map[string]any, segs []string, base string) error {
	var cur any = root
	for i := 0; i < len(segs); i++ {
		seg := segs[i]
		node, ok := cur.(map[string]any)
		if !ok {
			return &ValidationError{Path: base + "/" + strings.Join(segs, "~1"), Reason: "patch path does not resolve"}
		}
		next, exists := node[seg]
		if !exists {
			if i == len(segs)-1 {
				// The patch creates the leaf. Only reserved and case-violating
				// names are rejected.
				if seg == "extra" {
					return &ValidationError{Path: base + "/" + strings.Join(segs, "~1"), Reason: "reserved property"}
				}
				for k := range propSchema[propSchema["Card"]["localizations"]] {
					_ = k
				}
				return nil
			}
			return &ValidationError{Path: base + "/" + strings.Join(segs, "~1"), Reason: "patch path does not resolve"}
		}
		cur = next
	}
	return nil
}

func splitPointer(ptr string) []string {
	raw := strings.Split(ptr, "/")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.ReplaceAll(s, "~1", "/")
		s = strings.ReplaceAll(s, "~0", "~")
		out = append(out, s)
	}
	return out
}

func joinPath(base, suffix string) string {
	if base == "" {
		return suffix
	}
	return base + "/" + suffix
}

// ValidationError reports a card that does not conform to the RFC 9553
// schema. Converters map it to an HTTP 422 response.
type ValidationError struct {
	Path   string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid JSContact Card property %q: %s", e.Path, e.Reason)
}

// applyPatch applies a localizations PatchObject to a deep copy of the card
// (RFC 9553 §2.7.1). Paths are RFC 6901 JSON Pointers.
func applyPatch(card map[string]any, patch map[string]any) (map[string]any, error) {
	root, ok := deepCopy(card).(map[string]any)
	if !ok {
		return nil, errInvalidPointer
	}
	for _, ptr := range sortedKeys(patch) {
		if err := applyPointer(root, splitPointer(ptr), patch[ptr]); err != nil {
			return nil, err
		}
	}
	return root, nil
}

var errInvalidPointer = fmt.Errorf("vcardconv: invalid localizations patch path")

func applyPointer(root any, segs []string, value any) error {
	if len(segs) == 0 {
		return errInvalidPointer
	}
	cur := root
	for i := 0; i < len(segs)-1; i++ {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[segs[i]]
			if !ok {
				return errInvalidPointer
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(segs[i])
			if err != nil || idx < 0 || idx >= len(node) {
				return errInvalidPointer
			}
			cur = node[idx]
		default:
			return errInvalidPointer
		}
	}
	switch node := cur.(type) {
	case map[string]any:
		node[segs[len(segs)-1]] = value
	default:
		return errInvalidPointer
	}
	return nil
}

// deepCopy returns a deep copy of a JSON-like value.
func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = deepCopy(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = deepCopy(val)
		}
		return out
	case string, bool, float64, nil:
		return v
	}
	return reflect.ValueOf(v).Interface()
}

// newUID generates a version 4 UUID for vCards whose Card lacks a uid.
func newUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
