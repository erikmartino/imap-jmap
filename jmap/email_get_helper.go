package jmap

import (
	"fmt"
	"mime"
	"net/mail"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// HeaderForm specifies the format of a header property.
type HeaderForm string

const (
	HeaderFormRaw              HeaderForm = "asRaw"
	HeaderFormText             HeaderForm = "asText"
	HeaderFormAddresses        HeaderForm = "asAddresses"
	HeaderFormGroupedAddresses HeaderForm = "asGroupedAddresses"
	HeaderFormMessageIDs       HeaderForm = "asMessageIds"
	HeaderFormDate             HeaderForm = "asDate"
	HeaderFormURLs             HeaderForm = "asURLs"
)

// ParsedHeaderProperty holds parsed header:* property metadata.
type ParsedHeaderProperty struct {
	RawProp string
	Name    string
	Form    HeaderForm
	All     bool
}

// ParseHeaderProperty parses header:name[:form][:all] per RFC 8621 Section 4.1.3.
func ParseHeaderProperty(prop string) (*ParsedHeaderProperty, error) {
	if !strings.HasPrefix(prop, "header:") {
		return nil, fmt.Errorf("not a header property")
	}
	parts := strings.Split(prop, ":")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid header property format: %q", prop)
	}
	name := parts[1]
	if name == "" {
		return nil, fmt.Errorf("empty header name in %q", prop)
	}

	form := HeaderFormRaw
	all := false

	if len(parts) == 2 {
		// header:name
	} else if len(parts) == 3 {
		if parts[2] == "all" {
			all = true
		} else {
			f := HeaderForm(parts[2])
			if !isValidHeaderForm(f) {
				return nil, fmt.Errorf("invalid header form %q in %q", parts[2], prop)
			}
			form = f
		}
	} else if len(parts) == 4 {
		f := HeaderForm(parts[2])
		if !isValidHeaderForm(f) {
			return nil, fmt.Errorf("invalid header form %q in %q", parts[2], prop)
		}
		form = f
		if parts[3] != "all" {
			return nil, fmt.Errorf("expected :all as final suffix in %q", prop)
		}
		all = true
	} else {
		return nil, fmt.Errorf("too many parts in header property: %q", prop)
	}

	return &ParsedHeaderProperty{
		RawProp: prop,
		Name:    name,
		Form:    form,
		All:     all,
	}, nil
}

func isValidHeaderForm(f HeaderForm) bool {
	switch f {
	case HeaderFormRaw, HeaderFormText, HeaderFormAddresses, HeaderFormGroupedAddresses, HeaderFormMessageIDs, HeaderFormDate, HeaderFormURLs:
		return true
	default:
		return false
	}
}

// EvaluateHeaderProperty evaluates a parsed header property on an Email object.
func EvaluateHeaderProperty(em *Email, hp *ParsedHeaderProperty) any {
	var matchingValues []string
	for _, h := range em.Headers {
		if strings.EqualFold(h.Name, hp.Name) {
			matchingValues = append(matchingValues, h.Value)
		}
	}

	if hp.All {
		out := make([]any, 0, len(matchingValues))
		for _, v := range matchingValues {
			formatted := formatHeaderValue(v, hp.Form)
			out = append(out, formatted)
		}
		return out
	}

	if len(matchingValues) == 0 {
		return nil
	}
	// Return the last matching header value per RFC 8621 Section 4.1.3
	lastVal := matchingValues[len(matchingValues)-1]
	return formatHeaderValue(lastVal, hp.Form)
}

// formatHeaderValue formats a single raw header string according to the requested HeaderForm.
func formatHeaderValue(raw string, form HeaderForm) any {
	switch form {
	case HeaderFormRaw:
		if !strings.HasPrefix(raw, " ") && !strings.HasPrefix(raw, "\t") {
			return " " + raw
		}
		return raw

	case HeaderFormText:
		return decodeHeaderText(raw)

	case HeaderFormAddresses:
		return decodeHeaderAddresses(raw)

	case HeaderFormGroupedAddresses:
		return decodeHeaderGroupedAddresses(raw)

	case HeaderFormMessageIDs:
		return decodeHeaderMessageIDs(raw)

	case HeaderFormDate:
		return decodeHeaderDate(raw)

	case HeaderFormURLs:
		return decodeHeaderURLs(raw)

	default:
		return raw
	}
}

var unfoldingRegex = regexp.MustCompile(`\r?\n[ \t]+`)

func unfoldHeader(raw string) string {
	unfolded := unfoldingRegex.ReplaceAllString(raw, " ")
	return strings.TrimSpace(unfolded)
}

func decodeHeaderText(raw string) string {
	unfolded := unfoldHeader(raw)

	dec := new(mime.WordDecoder)
	decoded, err := dec.DecodeHeader(unfolded)
	if err != nil {
		decoded = unfolded
	}

	return norm.NFC.String(decoded)
}

func decodeHeaderAddresses(raw string) any {
	unfolded := unfoldHeader(raw)

	if idx := strings.Index(unfolded, ":"); idx >= 0 {
		sub := unfolded[idx+1:]
		sub = strings.TrimSuffix(strings.TrimSpace(sub), ";")
		unfolded = sub
	}

	addrs, err := mail.ParseAddressList(unfolded)
	if err != nil {
		return nil
	}
	if len(addrs) == 0 {
		return []any{}
	}

	out := make([]any, 0, len(addrs))
	for _, a := range addrs {
		m := map[string]any{
			"email": a.Address,
		}
		if a.Name != "" {
			name := a.Name
			name = strings.ReplaceAll(name, "\\", "")
			m["name"] = decodeHeaderText(name)
		} else {
			m["name"] = nil
		}
		out = append(out, m)
	}
	return out
}

func decodeHeaderGroupedAddresses(raw string) any {
	unfolded := strings.ReplaceAll(raw, "\r\n", " ")
	unfolded = strings.ReplaceAll(unfolded, "\n", " ")
	unfolded = strings.TrimSpace(unfolded)

	colonIdx := strings.Index(unfolded, ":")
	if colonIdx >= 0 {
		groupName := strings.TrimSpace(unfolded[:colonIdx])
		addrPart := strings.TrimSuffix(strings.TrimSpace(unfolded[colonIdx+1:]), ";")
		addrsVal := decodeHeaderAddresses(addrPart)
		var addrsList []any
		if arr, ok := addrsVal.([]any); ok {
			addrsList = arr
		}
		return []any{
			map[string]any{
				"name":      decodeHeaderText(groupName),
				"addresses": addrsList,
			},
		}
	}

	addrsVal := decodeHeaderAddresses(unfolded)
	var addrsList []any
	if arr, ok := addrsVal.([]any); ok {
		addrsList = arr
	}
	return []any{
		map[string]any{
			"name":      nil,
			"addresses": addrsList,
		},
	}
}

var midRegex = regexp.MustCompile(`<([^>]+)>|([^\s<>]+)`)

func decodeHeaderMessageIDs(raw string) any {
	matches := midRegex.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return []any{}
	}
	var out []any
	for _, m := range matches {
		val := m[1]
		if val == "" {
			val = m[2]
		}
		val = strings.Trim(val, "<> \t")
		if val != "" {
			out = append(out, val)
		}
	}
	return out
}

func decodeHeaderDate(raw string) any {
	t, err := mail.ParseDate(strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	return t.Format("2006-01-02T15:04:05-07:00")
}

var urlRegex = regexp.MustCompile(`<([^>]+)>`)

func decodeHeaderURLs(raw string) any {
	matches := urlRegex.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		trimmed := strings.TrimSpace(raw)
		if u, err := url.Parse(trimmed); err == nil && u.Scheme != "" {
			return []any{trimmed}
		}
		return nil
	}
	var out []any
	for _, m := range matches {
		uStr := strings.TrimSpace(m[1])
		if u, err := url.Parse(uStr); err == nil && u.Scheme != "" {
			out = append(out, uStr)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var defaultBodyProperties = []string{
	"partId", "blobId", "size", "name", "type", "charset", "disposition", "cid", "language", "location",
}

// FilterEmailBodyPart filters an EmailBodyPart to only include requested bodyProperties.
func FilterEmailBodyPart(part EmailBodyPart, bodyProperties []string) map[string]any {
	if bodyProperties == nil {
		bodyProperties = defaultBodyProperties
	}
	if len(bodyProperties) == 0 {
		return map[string]any{}
	}

	propMap := make(map[string]bool, len(bodyProperties))
	for _, p := range bodyProperties {
		propMap[p] = true
	}

	out := make(map[string]any, len(bodyProperties))
	if propMap["partId"] {
		if part.PartID != nil {
			out["partId"] = *part.PartID
		} else {
			out["partId"] = nil
		}
	}
	if propMap["blobId"] {
		if part.BlobID != nil {
			out["blobId"] = string(*part.BlobID)
		} else {
			out["blobId"] = nil
		}
	}
	if propMap["size"] {
		out["size"] = part.Size
	}
	if propMap["name"] {
		if part.Name != nil {
			out["name"] = *part.Name
		} else {
			out["name"] = nil
		}
	}
	if propMap["type"] {
		out["type"] = part.Type
	}
	if propMap["charset"] {
		if part.Charset != nil {
			out["charset"] = *part.Charset
		} else {
			out["charset"] = nil
		}
	}
	if propMap["disposition"] {
		if part.Disposition != nil {
			out["disposition"] = *part.Disposition
		} else {
			out["disposition"] = nil
		}
	}
	if propMap["cid"] {
		if part.CID != nil {
			out["cid"] = *part.CID
		} else {
			out["cid"] = nil
		}
	}
	if propMap["language"] {
		if part.Language != nil {
			out["language"] = part.Language
		} else {
			out["language"] = nil
		}
	}
	if propMap["location"] {
		if part.Location != nil {
			out["location"] = *part.Location
		} else {
			out["location"] = nil
		}
	}
	if propMap["headers"] {
		if part.Headers != nil {
			out["headers"] = part.Headers
		} else {
			out["headers"] = []EmailHeader{}
		}
	}
	if propMap["subParts"] {
		var subs []any = []any{}
		for _, sp := range part.SubParts {
			subs = append(subs, FilterEmailBodyPart(sp, bodyProperties))
		}
		out["subParts"] = subs
	} else if len(part.SubParts) > 0 || strings.HasPrefix(part.Type, "multipart/") {
		var subs []any = []any{}
		for _, sp := range part.SubParts {
			subs = append(subs, FilterEmailBodyPart(sp, bodyProperties))
		}
		out["subParts"] = subs
	}

	return out
}
