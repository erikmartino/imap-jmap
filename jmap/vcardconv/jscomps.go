package vcardconv

import (
	"strconv"
	"strings"
)

// Name/address component position codes used by the JSCOMPS parameter
// (RFC 9555 §3.3.1). The numbers are the zero-based index of the corresponding
// component in the N (RFC 9554 §2.2) or ADR (RFC 9554 §2.1) structured value.
const (
	namePosSurname   = 0
	namePosGiven     = 1
	namePosGiven2    = 2
	namePosTitle     = 3
	namePosCredential = 4
	namePosSurname2  = 5
	namePosGeneration = 6

	addrPosPostOfficeBox = 0
	addrPosExt           = 1
	addrPosStreet        = 2
	addrPosLocality      = 3
	addrPosRegion        = 4
	addrPosPostcode      = 5
	addrPosCountry       = 6
	addrPosRoom          = 7
	addrPosApartment     = 8
	addrPosFloor         = 9
	addrPosNumber        = 10
	addrPosName          = 11
	addrPosBuilding      = 12
	addrPosBlock         = 13
	addrPosSubdistrict   = 14
	addrPosDistrict      = 15
	addrPosLandmark      = 16
	addrPosDirection     = 17
)

var nameKindByPos = map[int]string{
	namePosSurname:   "surname",
	namePosGiven:     "given",
	namePosGiven2:    "given2",
	namePosTitle:     "title",
	namePosCredential: "credential",
	namePosSurname2:  "surname2",
	namePosGeneration: "generation",
}

var namePosByKind = map[string]int{
	"surname":    namePosSurname,
	"given":      namePosGiven,
	"given2":     namePosGiven2,
	"title":      namePosTitle,
	"credential": namePosCredential,
	"surname2":   namePosSurname2,
	"generation": namePosGeneration,
}

var addrKindByPos = map[int]string{
	addrPosPostOfficeBox: "postOfficeBox",
	addrPosExt:           "apartment",
	addrPosStreet:        "name",
	addrPosLocality:      "locality",
	addrPosRegion:        "region",
	addrPosPostcode:      "postcode",
	addrPosCountry:       "country",
	addrPosRoom:          "room",
	addrPosApartment:     "apartment",
	addrPosFloor:         "floor",
	addrPosNumber:        "number",
	addrPosName:          "name",
	addrPosBuilding:      "building",
	addrPosBlock:         "block",
	addrPosSubdistrict:   "subdistrict",
	addrPosDistrict:      "district",
	addrPosLandmark:      "landmark",
	addrPosDirection:     "direction",
}

var addrPosByKind = map[string]int{
	"postOfficeBox": addrPosPostOfficeBox,
	"apartment":     addrPosApartment,
	"floor":         addrPosFloor,
	"room":          addrPosRoom,
	"building":      addrPosBuilding,
	"number":        addrPosNumber,
	"name":          addrPosName,
	"block":         addrPosBlock,
	"subdistrict":   addrPosSubdistrict,
	"district":      addrPosDistrict,
	"landmark":      addrPosLandmark,
	"direction":     addrPosDirection,
	"locality":      addrPosLocality,
	"region":        addrPosRegion,
	"postcode":      addrPosPostcode,
	"country":       addrPosCountry,
}

// nameJSCOMPS builds the JSCOMPS parameter value for a name. posByKind maps a
// component kind to its N structured-value position. hasDefaultSeparator
// reflects whether the JSContact object defines a defaultSeparator property
// (even an empty one); an absent property yields an empty first entry.
func nameJSCOMPS(comps []component, defaultSeparator string, hasDefaultSeparator bool) string {
	return buildJSCOMPS(comps, defaultSeparator, hasDefaultSeparator, namePosByKind)
}

// addressJSCOMPS builds the JSCOMPS parameter value for an address.
func addressJSCOMPS(comps []component, defaultSeparator string, hasDefaultSeparator bool) string {
	return buildJSCOMPS(comps, defaultSeparator, hasDefaultSeparator, addrPosByKind)
}

// buildJSCOMPS renders the quoted JSCOMPS parameter value for an ordered list
// of components. The first entry is the defaultSeparator (prefixed by "s,")
// when the JSContact object defines one, and empty otherwise, followed by
// positional/separator entries in component order.
func buildJSCOMPS(comps []component, defaultSeparator string, hasDefaultSeparator bool, posByKind map[string]int) string {
	entries := []string{}
	if hasDefaultSeparator {
		entries = append(entries, "s,"+escapeJSCOMPSVerb(defaultSeparator))
	} else {
		entries = append(entries, "")
	}
	for _, c := range comps {
		if c.kind == "separator" {
			entries = append(entries, "s,"+escapeJSCOMPSVerb(c.value))
			continue
		}
		pos, ok := posByKind[c.kind]
		if !ok {
			continue
		}
		entries = append(entries, strconv.Itoa(pos))
	}
	return strings.Join(entries, ";")
}

// component is a (kind, value) pair used while assembling JSCOMPS.
type component struct {
	kind  string
	value string
}

// jscompsEntries is the decoded content of a JSCOMPS parameter value.
type jscompsEntries struct {
	defaultSeparator string
	order            []component
}

// decodeJSCOMPS parses the content of a JSCOMPS parameter (without the
// enclosing DQUOTEs). posByKind maps positions to kinds for the property type.
func decodeJSCOMPS(value string, posByKind map[int]string) (jscompsEntries, error) {
	var out jscompsEntries
	entries := strings.Split(value, ";")
	if len(entries) == 0 {
		return out, nil
	}
	if first := entries[0]; strings.HasPrefix(first, "s,") {
		out.defaultSeparator = unescapeJSCOMPSVerb(first[2:])
	} else if first != "" {
		return out, errInvalidJSCOMPS
	}
	for _, e := range entries[1:] {
		if strings.HasPrefix(e, "s,") {
			out.order = append(out.order, component{kind: "separator", value: unescapeJSCOMPSVerb(e[2:])})
			continue
		}
		pos, err := strconv.Atoi(e)
		if err != nil {
			return out, errInvalidJSCOMPS
		}
		kind, ok := posByKind[pos]
		if !ok {
			return out, errInvalidJSCOMPS
		}
		out.order = append(out.order, component{kind: kind})
	}
	return out, nil
}

var errInvalidJSCOMPS = &invalidJSCOMPSError{}

type invalidJSCOMPSError struct{}

func (*invalidJSCOMPSError) Error() string { return "vcardconv: invalid JSCOMPS parameter value" }