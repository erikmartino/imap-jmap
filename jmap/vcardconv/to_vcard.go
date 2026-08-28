package vcardconv

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// ToVCard converts a JSContact Card (RFC 9553, decoded as a JSON object) into
// a vCard 4.0 (RFC 6350) document following RFC 9555 and RFC 9554. The card
// is validated first (RFC 9553 schema); an invalid card returns an error so
// callers can respond with HTTP 422 as required by the jscontact-tests suite.
func ToVCard(card map[string]any) (string, error) {
	if err := ValidateCard(card); err != nil {
		return "", err
	}
	c := &converter{card: card}
	return encodeVCard(c.convertCard()), nil
}

type converter struct {
	card      map[string]any
	nextGroup int
}

// group allocates a unique vCard property group name.
func (c *converter) group() string {
	c.nextGroup++
	return "group" + strconv.Itoa(c.nextGroup)
}

// langVariants returns the list of localization language tags for the card.
func (c *converter) langVariants() []string {
	return sortedKeys(mapFieldOrEmpty(c.card, "localizations"))
}

// altID returns the ALTID value to use for a property group. It is non-empty
// when localized variants or a phonetic name require ALTID grouping.
func (c *converter) altID() string {
	if len(c.langVariants()) > 0 {
		return "1"
	}
	return ""
}

func (c *converter) convertCard() []vcardField {
	var fields []vcardField
	card := c.card

	if uid := strField(card, "uid"); uid != "" {
		fields = append(fields, vcardField{Name: "UID", Value: uid})
	} else {
		fields = append(fields, vcardField{Name: "UID", Value: newUID()})
	}
	if kind := strField(card, "kind"); kind != "" {
		fields = append(fields, vcardField{Name: "KIND", Value: kind})
	}
	if prodID := strField(card, "prodId"); prodID != "" {
		fields = append(fields, vcardField{Name: "PRODID", Value: prodID})
	}
	if created := strField(card, "created"); created != "" {
		if ts, ok := toVCardTimestamp(created); ok {
			fields = append(fields, vcardField{Name: "CREATED", Params: []vcardParam{{Name: "VALUE", Value: "timestamp"}}, Value: ts})
		}
	}
	if updated := strField(card, "updated"); updated != "" {
		if ts, ok := toVCardTimestamp(updated); ok {
			fields = append(fields, vcardField{Name: "REV", Value: ts})
		}
	}
	if lang := strField(card, "language"); lang != "" {
		fields = append(fields, vcardField{Name: "LANGUAGE", Value: lang})
	}
	if version := strField(card, "version"); version != "" {
		fields = append(fields, jspropVersion(version))
	}

	fields = append(fields, c.convertNames()...)
	fields = append(fields, c.convertNicknames()...)
	fields = append(fields, c.convertOrganizationsAndTitles()...)
	fields = append(fields, c.convertEmails()...)
	fields = append(fields, c.convertPhones()...)
	fields = append(fields, c.convertOnlineServices()...)
	fields = append(fields, c.convertLinks()...)
	fields = append(fields, c.convertCalendars()...)
	fields = append(fields, c.convertSchedulingAddresses()...)
	fields = append(fields, c.convertAddresses()...)
	fields = append(fields, c.convertDirectories()...)
	fields = append(fields, c.convertPersonalInfo()...)
	fields = append(fields, c.convertCryptoKeys()...)
	fields = append(fields, c.convertMedia()...)
	fields = append(fields, c.convertNotes()...)
	fields = append(fields, c.convertKeywords()...)
	fields = append(fields, c.convertMembers()...)
	fields = append(fields, c.convertRelatedTo()...)
	fields = append(fields, c.convertAnniversaries()...)
	fields = append(fields, c.convertSpeakToAs()...)
	fields = append(fields, c.convertPreferredLanguages()...)
	fields = append(fields, c.convertVCardProps()...)
	fields = append(fields, c.convertUnknownProps()...)

	return fields
}

// convertVariants converts a property group on the base card and on each
// localized variant. convert receives the (possibly patched) card, the
// language tag (empty for the base) and the shared ALTID value.
func (c *converter) convertVariants(convert func(card map[string]any, lang, altID string) []vcardField) []vcardField {
	altID := c.altID()
	var fields []vcardField
	fields = append(fields, convert(c.card, "", altID)...)
	for _, lang := range c.langVariants() {
		patched := c.patchedCard(lang)
		if reflect.DeepEqual(patched, c.card) {
			continue
		}
		fields = append(fields, convert(patched, lang, altID)...)
	}
	return fields
}

// ---- name, FN, N ---------------------------------------------------------

func (c *converter) convertNames() []vcardField {
	if mapFieldOrNil(c.card, "name") == nil {
		// FN is mandatory in vCard; without a JSContact name it is empty.
		return []vcardField{{Name: "FN"}}
	}
	fields := c.convertVariants(nameToVCard)
	// If no localized variant changed the name, emit only the base FN (no
	// redundant LANGUAGE/ALTID copies).
	return fields
}

// nameToVCard emits FN and N (plus a phonetic N) for the name on card.
// language and altID set the LANGUAGE and ALTID parameters.
func nameToVCard(card map[string]any, language, altID string) []vcardField {
	name, ok := mapField(card, "name")
	if !ok {
		return nil
	}
	var fields []vcardField
	params := languageParams(language, altID)

	full := strField(name, "full")
	comps := nameComponents(name)
	if len(comps) == 0 && full != "" {
		parts := strings.Fields(full)
		if len(parts) == 1 {
			comps = []nameComponent{{kind: "given", value: parts[0]}}
		} else if len(parts) >= 2 {
			comps = []nameComponent{
				{kind: "given", value: parts[0]},
				{kind: "surname", value: strings.Join(parts[1:], " ")},
			}
		}
	}
	switch {
	case full != "":
		fields = append(fields, vcardField{Name: "FN", Params: params, Value: full})
	case len(comps) > 0:
		fnParams := append(params, vcardParam{Name: "DERIVED", Value: "TRUE"})
		fields = append(fields, vcardField{Name: "FN", Params: fnParams, Value: deriveFullName(name, comps)})
	default:
		fields = append(fields, vcardField{Name: "FN", Params: params})
	}

	if !needsExtendedN(name, comps, altID) {
		fields = append(fields, vcardField{Name: "N", Params: params, Value: nameNValue(name, comps, false)})
	} else {
		n := append([]vcardParam{}, params...)
		if jscomps := jscompsForName(name, comps); jscomps != "" {
			n = append(n, vcardParam{Name: "JSCOMPS", Value: jscomps, Mode: paramQuotedAlways})
		}
		if sortAs := mapFieldOrEmpty(name, "sortAs"); len(sortAs) > 0 {
			n = append(n, vcardParam{Name: "SORT-AS", Value: sortAsString(sortAs)})
		}
		fields = append(fields, vcardField{Name: "N", Params: n, Value: nameNValue(name, comps, true)})
	}

	// Phonetic representation: a second N sharing the ALTID with the
	// PHONETIC parameter set to the phoneticSystem.
	if phonetic := strField(name, "phoneticSystem"); phonetic != "" && hasPhonetics(comps) {
		pParams := append(languageParams("", altID), vcardParam{Name: "PHONETIC", Value: phonetic})
		if jscomps := jscompsForName(name, comps); jscomps != "" && isOrdered(name) {
			pParams = append(pParams, vcardParam{Name: "JSCOMPS", Value: jscomps, Mode: paramQuotedAlways})
		}
		fields = append(fields, vcardField{Name: "N", Params: pParams, Value: phoneticNValue(comps)})
	}
	return fields
}

func languageParams(language, altID string) []vcardParam {
	var params []vcardParam
	if language != "" {
		params = append(params, vcardParam{Name: "LANGUAGE", Value: language})
	}
	if altID != "" {
		params = append(params, vcardParam{Name: "ALTID", Value: altID})
	}
	return params
}

// jscompsForName returns the JSCOMPS parameter value for an ordered name, or
// the empty string when the name is not ordered.
func jscompsForName(name map[string]any, comps []nameComponent) string {
	if !isOrdered(name) && !hasNameSeparators(comps) {
		return ""
	}
	_, hasDefault := name["defaultSeparator"]
	return nameJSCOMPS(toComponents(comps), strField(name, "defaultSeparator"), hasDefault)
}

// toComponents converts name components to the generic component form used by
// the JSCOMPS codec.
func toComponents[T any](comps []T) []component {
	out := make([]component, 0, len(comps))
	for _, c := range comps {
		switch v := any(c).(type) {
		case nameComponent:
			out = append(out, component{kind: v.kind, value: v.value})
		case addressComponent:
			out = append(out, component{kind: v.kind, value: v.value})
		}
	}
	return out
}

type nameComponent struct {
	kind     string
	value    string
	phonetic string
}

func nameComponents(name map[string]any) []nameComponent {
	var comps []nameComponent
	for _, raw := range strSliceField(name, "components") {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		comps = append(comps, nameComponent{
			kind:     strField(m, "kind"),
			value:    strField(m, "value"),
			phonetic: strField(m, "phonetic"),
		})
	}
	return comps
}

func isOrdered(m map[string]any) bool {
	b, _ := m["isOrdered"].(bool)
	return b
}

func hasNameSeparators(comps []nameComponent) bool {
	for _, c := range comps {
		if c.kind == "separator" {
			return true
		}
	}
	return false
}

func hasPhonetics(comps []nameComponent) bool {
	for _, c := range comps {
		if c.phonetic != "" {
			return true
		}
	}
	return false
}

// deriveFullName concatenates component values in order, using separator
// components and the defaultSeparator (or a space) between values.
func deriveFullName(name map[string]any, comps []nameComponent) string {
	defaultSep := strField(name, "defaultSeparator")
	if defaultSep == "" {
		defaultSep = " "
	}
	var parts []string
	sep := ""
	hasCustomSep := false
	for _, comp := range comps {
		if comp.kind == "separator" {
			sep = comp.value
			hasCustomSep = true
			continue
		}
		if len(parts) > 0 {
			if hasCustomSep {
				parts = append(parts, sep)
			} else {
				parts = append(parts, defaultSep)
			}
			sep = ""
			hasCustomSep = false
		}
		parts = append(parts, comp.value)
	}
	return strings.Join(parts, "")
}

// needsExtendedN reports whether the RFC 9554 seven-component N form must be
// used: when ordered, when extension components (surname2, generation,
// phonetic, sortAs) are present, or when ALTID grouping requires stable
// positions across localized variants.
func needsExtendedN(name map[string]any, comps []nameComponent, altID string) bool {
	if isOrdered(name) {
		return true
	}
	if altID != "" {
		return true
	}
	if len(mapFieldOrEmpty(name, "sortAs")) > 0 {
		return true
	}
	for _, c := range comps {
		switch c.kind {
		case "surname2", "generation":
			return true
		}
		if c.phonetic != "" {
			return true
		}
	}
	return false
}

// nameNValue renders the N structured value. The basic form has five
// components (family;given;additional;prefix;suffix); the extended RFC 9554
// form adds secondary surname and generation. The family component also
// receives the surname2 values, and the suffix component receives the
// generation values, for backwards compatibility (RFC 9554 §2.2).
func nameNValue(name map[string]any, comps []nameComponent, extended bool) string {
	family, given, given2, title, suffix, s2, generation := splitNameComponents(comps)

	fields := []string{
		strings.Join(append(family, s2...), ","),
		strings.Join(given, ","),
		strings.Join(given2, ","),
		strings.Join(title, ","),
		strings.Join(append(suffix, generation...), ","),
	}
	if extended {
		fields = append(fields, strings.Join(s2, ","), strings.Join(generation, ","))
	}
	return strings.Join(fields, ";")
}

func splitNameComponents(comps []nameComponent) (family, given, given2, title, suffix, s2, generation []string) {
	for _, comp := range comps {
		if comp.kind == "separator" {
			continue
		}
		switch comp.kind {
		case "surname":
			family = append(family, comp.value)
		case "given":
			given = append(given, comp.value)
		case "given2":
			given2 = append(given2, comp.value)
		case "title":
			title = append(title, comp.value)
		case "credential":
			suffix = append(suffix, comp.value)
		case "generation":
			generation = append(generation, comp.value)
		case "surname2":
			s2 = append(s2, comp.value)
		}
	}
	return
}

// phoneticNValue renders the N value for the phonetic representation: the
// phonetic component values in the same structured layout.
func phoneticNValue(comps []nameComponent) string {
	var family, given, given2, title, suffix, s2, generation []string
	for _, comp := range comps {
		if comp.kind == "separator" {
			continue
		}
		v := comp.phonetic
		switch comp.kind {
		case "surname":
			family = append(family, v)
		case "given":
			given = append(given, v)
		case "given2":
			given2 = append(given2, v)
		case "title":
			title = append(title, v)
		case "credential":
			suffix = append(suffix, v)
		case "generation":
			generation = append(generation, v)
		case "surname2":
			s2 = append(s2, v)
		}
	}
	fields := []string{
		strings.Join(append(family, s2...), ","),
		strings.Join(given, ","),
		strings.Join(given2, ","),
		strings.Join(title, ","),
		strings.Join(append(suffix, generation...), ","),
		strings.Join(s2, ","),
		strings.Join(generation, ","),
	}
	return strings.Join(fields, ";")
}

// sortAsString renders the SORT-AS parameter value: family sort, then given
// sort, comma separated (RFC 9555 §2.5.5).
func sortAsString(sortAs map[string]any) string {
	var parts []string
	for _, k := range []string{"surname", "given"} {
		if v, ok := sortAs[k].(string); ok {
			parts = append(parts, v)
		} else {
			parts = append(parts, "")
		}
	}
	return strings.Join(parts, ",")
}

// ---- nicknames -----------------------------------------------------------

func (c *converter) convertNicknames() []vcardField {
	return c.convertVariants(func(card map[string]any, lang, altID string) []vcardField {
		var fields []vcardField
		for _, id := range sortedKeys(mapFieldOrEmpty(card, "nicknames")) {
			obj := mapAt(card, "nicknames", id)
			if obj == nil {
				continue
			}
			f := vcardField{
				Name:   "NICKNAME",
				Params: []vcardParam{{Name: "PROP-ID", Value: id}},
				Value:  strField(obj, "name"),
			}
			f.Params = append(f.Params, languageParams(lang, altID)...)
			f.Params = append(f.Params, contextParams(obj)...)
			fields = append(fields, f)
		}
		return fields
	})
}

// ---- organizations and titles --------------------------------------------

func (c *converter) convertOrganizationsAndTitles() []vcardField {
	var fields []vcardField
	orgs := mapFieldOrEmpty(c.card, "organizations")
	emittedOrgs := map[string]bool{}
	orgGroups := map[string]string{}

	titleFields := c.convertVariants(func(card map[string]any, lang, altID string) []vcardField {
		var tfields []vcardField
		for _, id := range sortedKeys(mapFieldOrEmpty(card, "titles")) {
			obj := mapAt(card, "titles", id)
			if obj == nil {
				continue
			}
			kind := strField(obj, "kind")
			if kind == "" {
				kind = "title"
			}
			f := vcardField{
				Name:   strings.ToUpper(kind),
				Params: []vcardParam{{Name: "PROP-ID", Value: id}},
				Value:  strField(obj, "name"),
			}
			f.Params = append(f.Params, languageParams(lang, altID)...)
			if orgID := strField(obj, "organizationId"); orgID != "" && mapFieldOrEmpty(card, "organizations")[orgID] != nil {
				g, ok := orgGroups[orgID]
				if !ok {
					g = c.group()
					orgGroups[orgID] = g
				}
				f.Group = g
				emittedOrgs[orgID] = true
				org := vcardField{
					Group:  g,
					Name:   "ORG",
					Params: []vcardParam{{Name: "PROP-ID", Value: orgID}},
					Value:  orgValue(mapAt(card, "organizations", orgID)),
				}
				if _, has := mapAt(card, "organizations", orgID)["sortAs"]; has {
					org.Params = append(org.Params, vcardParam{Name: "SORT-AS", Value: strField(mapAt(card, "organizations", orgID), "sortAs")})
				}
				tfields = append(tfields, org)
			}
			tfields = append(tfields, f)
		}
		return tfields
	})
	fields = append(fields, titleFields...)

	for _, id := range sortedKeys(orgs) {
		if emittedOrgs[id] {
			continue
		}
		obj := mapAt(c.card, "organizations", id)
		if obj == nil {
			continue
		}
		f := vcardField{
			Name:   "ORG",
			Params: []vcardParam{{Name: "PROP-ID", Value: id}},
			Value:  orgValue(obj),
		}
		if _, has := obj["sortAs"]; has {
			f.Params = append(f.Params, vcardParam{Name: "SORT-AS", Value: strField(obj, "sortAs")})
		}
		fields = append(fields, f)
	}
	return fields
}

func orgValue(org map[string]any) string {
	var parts []string
	if name := strField(org, "name"); name != "" {
		parts = append(parts, name)
	}
	for _, u := range strSliceField(org, "units") {
		if m, ok := u.(map[string]any); ok {
			parts = append(parts, strField(m, "name"))
			continue
		}
		if s, ok := u.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ";")
}

// ---- emails / phones ------------------------------------------------------

func (c *converter) convertEmails() []vcardField {
	return c.convertVariants(func(card map[string]any, lang, altID string) []vcardField {
		var fields []vcardField
		for _, id := range sortedKeys(mapFieldOrEmpty(card, "emails")) {
			obj := mapAt(card, "emails", id)
			if obj == nil {
				continue
			}
			f := vcardField{
				Name:   "EMAIL",
				Params: commonParams(obj, id),
				Value:  strField(obj, "address"),
			}
			f.Params = append(f.Params, languageParams(lang, altID)...)
			fields = append(fields, f)
		}
		return fields
	})
}

func (c *converter) convertPhones() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "phones")) {
		obj := mapAt(c.card, "phones", id)
		if obj == nil {
			continue
		}
		params := []vcardParam{{Name: "VALUE", Value: "uri"}}
		params = append(params, commonParams(obj, id)...)
		fields = append(fields, vcardField{
			Name:   "TEL",
			Params: params,
			Value:  strField(obj, "number"),
		})
	}
	return fields
}

// commonParams renders PROP-ID, PREF, TYPE (contexts and features) and
// vCardParams for an object.
func commonParams(obj map[string]any, id string) []vcardParam {
	var params []vcardParam
	if id != "" {
		params = append(params, vcardParam{Name: "PROP-ID", Value: id})
	}
	if pref, ok := numField(obj, "pref"); ok && pref > 0 {
		params = append(params, vcardParam{Name: "PREF", Value: strconv.FormatUint(pref, 10)})
	}
	params = append(params, contextParams(obj)...)
	if features := mapFieldOrEmpty(obj, "features"); len(features) > 0 {
		var types []string
		for _, k := range sortedKeys(features) {
			if b, _ := features[k].(bool); b {
				types = append(types, strings.ToUpper(featureToType(k)))
			}
		}
		if len(types) > 0 {
			params = append(params, vcardParam{Name: "TYPE", Value: strings.Join(types, ","), Mode: paramUnquoted})
		}
	}
	params = append(params, vcardParamsOf(obj)...)
	return params
}

func contextParams(obj map[string]any) []vcardParam {
	ctx := mapFieldOrEmpty(obj, "contexts")
	var types []string
	for _, k := range sortedKeys(ctx) {
		if b, _ := ctx[k].(bool); !b {
			continue
		}
		switch k {
		case "work":
			types = append(types, "WORK")
		case "private":
			types = append(types, "HOME")
		default:
			types = append(types, strings.ToUpper(k))
		}
	}
	if len(types) == 0 {
		return nil
	}
	return []vcardParam{{Name: "TYPE", Value: strings.Join(types, ","), Mode: paramUnquoted}}
}

func featureToType(feature string) string {
	switch feature {
	case "mobile":
		return "cell"
	case "main-number":
		return "main-number"
	default:
		return feature
	}
}

func vcardParamsOf(obj map[string]any) []vcardParam {
	var params []vcardParam
	raw := mapFieldOrEmpty(obj, "vCardParams")
	for _, k := range sortedKeys(raw) {
		switch v := raw[k].(type) {
		case string:
			params = append(params, vcardParam{Name: strings.ToUpper(k), Value: v})
		case bool:
			params = append(params, vcardParam{Name: strings.ToUpper(k), Value: strconv.FormatBool(v)})
		case float64:
			params = append(params, vcardParam{Name: strings.ToUpper(k), Value: strconv.FormatFloat(v, 'f', -1, 64)})
		}
	}
	return params
}

// ---- online services ------------------------------------------------------

func (c *converter) convertOnlineServices() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "onlineServices")) {
		obj := mapAt(c.card, "onlineServices", id)
		if obj == nil {
			continue
		}
		propName := strField(obj, "vCardName")
		isSocial := false
		if propName == "" {
			if strField(obj, "service") != "" {
				propName = "socialprofile"
				isSocial = true
			} else {
				propName = "impp"
			}
		}
		f := vcardField{
			Name:   strings.ToUpper(propName),
			Params: []vcardParam{{Name: "PROP-ID", Value: id}},
			Value:  strField(obj, "uri"),
		}
		if service := strField(obj, "service"); service != "" {
			f.Params = append(f.Params, vcardParam{Name: "SERVICE-TYPE", Value: service})
		}
		if user := strField(obj, "user"); user != "" {
			f.Params = append(f.Params, vcardParam{Name: "USERNAME", Value: user})
		}
		if isSocial {
			f.Params = append(f.Params, vcardParam{Name: "VALUE", Value: "uri"})
		}
		f.Params = append(f.Params, contextParams(obj)...)
		if label := strField(obj, "label"); label != "" {
			g := c.group()
			f.Group = g
			fields = append(fields, vcardField{Group: g, Name: "X-ABLABEL", Value: label})
		}
		fields = append(fields, f)
	}
	return fields
}

// ---- links ----------------------------------------------------------------

func (c *converter) convertLinks() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "links")) {
		obj := mapAt(c.card, "links", id)
		if obj == nil {
			continue
		}
		propName := "URL"
		if kind := strField(obj, "kind"); kind == "contact" {
			propName = "CONTACT-URI"
		} else if vn := strField(obj, "vCardName"); vn != "" {
			propName = vn
		}
		fields = append(fields, vcardField{
			Name:   propName,
			Params: commonParams(obj, id),
			Value:  strField(obj, "uri"),
		})
	}
	return fields
}

// ---- calendars / scheduling addresses -------------------------------------

func (c *converter) convertCalendars() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "calendars")) {
		obj := mapAt(c.card, "calendars", id)
		if obj == nil {
			continue
		}
		propName := "CALURI"
		if strField(obj, "kind") == "freeBusy" {
			propName = "FBURL"
		}
		fields = append(fields, vcardField{
			Name:   propName,
			Params: commonParams(obj, id),
			Value:  strField(obj, "uri"),
		})
	}
	return fields
}

func (c *converter) convertSchedulingAddresses() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "schedulingAddresses")) {
		obj := mapAt(c.card, "schedulingAddresses", id)
		if obj == nil {
			continue
		}
		f := vcardField{
			Name:   "CALADRURI",
			Params: commonParams(obj, id),
			Value:  strField(obj, "uri"),
		}
		if label := strField(obj, "label"); label != "" {
			g := c.group()
			f.Group = g
			fields = append(fields, vcardField{Group: g, Name: "X-ABLABEL", Value: label})
		}
		fields = append(fields, f)
	}
	return fields
}

// ---- addresses ------------------------------------------------------------

func (c *converter) convertAddresses() []vcardField {
	return c.convertVariants(func(card map[string]any, lang, altID string) []vcardField {
		var fields []vcardField
		for _, id := range sortedKeys(mapFieldOrEmpty(card, "addresses")) {
			obj := mapAt(card, "addresses", id)
			if obj == nil {
				continue
			}
			comps := addressComponents(obj)
			f := vcardField{
				Name:   "ADR",
				Params: commonParams(obj, id),
				Value:  addressNValue(obj, comps),
			}
			f.Params = append(f.Params, languageParams(lang, altID)...)
			if full := strField(obj, "full"); full != "" {
				f.Params = append(f.Params, vcardParam{Name: "LABEL", Value: full})
			}
			if jscomps := jscompsForAddress(obj, comps); jscomps != "" {
				f.Params = append(f.Params, vcardParam{Name: "JSCOMPS", Value: jscomps, Mode: paramQuotedAlways})
			}
			if tz := strField(obj, "timeZone"); tz != "" {
				f.Params = append(f.Params, vcardParam{Name: "TZ", Value: tz})
			}
			fields = append(fields, f)
		}
		return fields
	})
}

func jscompsForAddress(obj map[string]any, comps []addressComponent) string {
	if !isOrdered(obj) && !addressHasSeparators(comps) {
		return ""
	}
	_, hasDefault := obj["defaultSeparator"]
	return addressJSCOMPS(toComponents(comps), strField(obj, "defaultSeparator"), hasDefault)
}

type addressComponent struct {
	kind  string
	value string
}

func addressComponents(addr map[string]any) []addressComponent {
	var comps []addressComponent
	for _, raw := range strSliceField(addr, "components") {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		comps = append(comps, addressComponent{kind: strField(m, "kind"), value: strField(m, "value")})
	}
	return comps
}

func addressHasSeparators(comps []addressComponent) bool {
	for _, c := range comps {
		if c.kind == "separator" {
			return true
		}
	}
	return false
}

// addressNValue renders the RFC 9554 ADR structured value (18 components).
// The legacy extended address and street components receive the corresponding
// new component values for backwards compatibility (RFC 9554 §2.1).
func addressNValue(addr map[string]any, comps []addressComponent) string {
	byKind := map[string][]string{}
	for _, c := range comps {
		if c.kind == "separator" {
			continue
		}
		byKind[c.kind] = append(byKind[c.kind], c.value)
	}
	get := func(kind string) string { return strings.Join(byKind[kind], ",") }

	ext := joinNonEmpty(get("room"), get("floor"), get("apartment"), get("building"))
	street := joinNonEmpty(get("number"), get("name"), get("block"), get("direction"), get("landmark"), get("subdistrict"), get("district"))

	fields := []string{
		get("postOfficeBox"),
		ext,
		street,
		get("locality"),
		get("region"),
		get("postcode"),
		get("country"),
		get("room"),
		get("apartment"),
		get("floor"),
		get("number"),
		get("name"),
		get("building"),
		get("block"),
		get("subdistrict"),
		get("district"),
		get("landmark"),
		get("direction"),
	}
	return strings.Join(fields, ";")
}

func joinNonEmpty(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, ",")
}

// ---- directories / personal info / crypto keys / media --------------------

func (c *converter) convertDirectories() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "directories")) {
		obj := mapAt(c.card, "directories", id)
		if obj == nil {
			continue
		}
		propName := "SOURCE"
		if strField(obj, "kind") == "directory" {
			propName = "ORG-DIRECTORY"
		}
		f := vcardField{
			Name:   propName,
			Params: commonParams(obj, id),
			Value:  strField(obj, "uri"),
		}
		f.Params = append(f.Params, vcardParam{Name: "VALUE", Value: "uri"})
		fields = append(fields, f)
	}
	return fields
}

func (c *converter) convertPersonalInfo() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "personalInfo")) {
		obj := mapAt(c.card, "personalInfo", id)
		if obj == nil {
			continue
		}
		kind := strField(obj, "kind")
		params := []vcardParam{{Name: "PROP-ID", Value: id}}
		if listAs, ok := numField(obj, "listAs"); ok && listAs > 0 {
			params = append(params, vcardParam{Name: "INDEX", Value: strconv.FormatUint(listAs, 10)})
		}
		if level := strField(obj, "level"); level != "" {
			params = append(params, vcardParam{Name: "LEVEL", Value: levelToVCard(kind, level)})
		}
		fields = append(fields, vcardField{
			Name:   strings.ToUpper(kind),
			Params: params,
			Value:  strField(obj, "value"),
		})
	}
	return fields
}

// levelToVCard maps a JSContact PersonalInfo level to the vCard LEVEL
// parameter: expertise levels convert via beginner/expert, other kinds
// verbatim uppercase (RFC 9555 §2.3.13 reverse).
func levelToVCard(kind, level string) string {
	if kind == "expertise" {
		switch level {
		case "low":
			return "beginner"
		case "medium":
			return "average"
		case "high":
			return "expert"
		}
	}
	return strings.ToUpper(level)
}

func (c *converter) convertCryptoKeys() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "cryptoKeys")) {
		obj := mapAt(c.card, "cryptoKeys", id)
		if obj == nil {
			continue
		}
		f := vcardField{
			Name:   "KEY",
			Params: commonParams(obj, id),
			Value:  strField(obj, "uri"),
		}
		f.Params = append(f.Params, vcardParam{Name: "VALUE", Value: "uri"})
		fields = append(fields, f)
	}
	return fields
}

func (c *converter) convertMedia() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "media")) {
		obj := mapAt(c.card, "media", id)
		if obj == nil {
			continue
		}
		kind := strField(obj, "kind")
		if kind == "" {
			kind = "photo"
		}
		fields = append(fields, vcardField{
			Name:   strings.ToUpper(kind),
			Params: commonParams(obj, id),
			Value:  strField(obj, "uri"),
		})
	}
	return fields
}

// ---- notes ----------------------------------------------------------------

func (c *converter) convertNotes() []vcardField {
	return c.convertVariants(func(card map[string]any, lang, altID string) []vcardField {
		var fields []vcardField
		for _, id := range sortedKeys(mapFieldOrEmpty(card, "notes")) {
			obj := mapAt(card, "notes", id)
			if obj == nil {
				continue
			}
			f := vcardField{
				Name:   "NOTE",
				Params: []vcardParam{{Name: "PROP-ID", Value: id}},
				Value:  strField(obj, "note"),
			}
			f.Params = append(f.Params, languageParams(lang, altID)...)
			if created := strField(obj, "created"); created != "" {
				if ts, ok := toVCardTimestamp(created); ok {
					f.Params = append(f.Params, vcardParam{Name: "CREATED", Value: ts})
				}
			}
			if author, ok := mapField(obj, "author"); ok {
				if name := strField(author, "name"); name != "" {
					f.Params = append(f.Params, vcardParam{Name: "AUTHOR-NAME", Value: name})
				}
				if uri := strField(author, "uri"); uri != "" {
					f.Params = append(f.Params, vcardParam{Name: "AUTHOR", Value: uri})
				}
			}
			fields = append(fields, f)
		}
		return fields
	})
}

// ---- keywords / members / relatedTo / anniversaries / speakToAs / lang ----

func (c *converter) convertKeywords() []vcardField {
	kw := mapFieldOrEmpty(c.card, "keywords")
	var values []string
	for _, k := range sortedKeys(kw) {
		if b, _ := kw[k].(bool); b {
			values = append(values, k)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return []vcardField{{Name: "CATEGORIES", Value: strings.Join(values, ",")}}
}

func (c *converter) convertMembers() []vcardField {
	members := mapFieldOrEmpty(c.card, "members")
	var fields []vcardField
	for _, k := range sortedKeys(members) {
		if b, _ := members[k].(bool); b {
			fields = append(fields, vcardField{Name: "MEMBER", Value: k})
		}
	}
	return fields
}

func (c *converter) convertRelatedTo() []vcardField {
	var fields []vcardField
	for _, key := range sortedKeys(mapFieldOrEmpty(c.card, "relatedTo")) {
		obj := mapAt(c.card, "relatedTo", key)
		if obj == nil {
			continue
		}
		f := vcardField{Name: "RELATED", Value: key}
		if rel := mapFieldOrEmpty(obj, "relation"); len(rel) > 0 {
			var types []string
			for _, k := range sortedKeys(rel) {
				if b, _ := rel[k].(bool); b {
					types = append(types, strings.ToUpper(k))
				}
			}
			if len(types) > 0 {
				f.Params = append(f.Params, vcardParam{Name: "TYPE", Value: strings.Join(types, ","), Mode: paramUnquoted})
			}
		}
		if !strings.HasPrefix(key, "urn:uuid:") && !strings.Contains(key, ":") {
			f.Params = append(f.Params, vcardParam{Name: "VALUE", Value: "text"})
		}
		fields = append(fields, f)
	}
	return fields
}

func (c *converter) convertAnniversaries() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "anniversaries")) {
		obj := mapAt(c.card, "anniversaries", id)
		if obj == nil {
			continue
		}
		kind := strField(obj, "kind")
		switch kind {
		case "birth":
			if date := anniversaryDateToVCard(mapFieldOrNil(obj, "date")); date != "" {
				fields = append(fields, vcardField{Name: "BDAY", Params: []vcardParam{{Name: "PROP-ID", Value: id}}, Value: date})
			}
			if place := strField(mapFieldOrNil(obj, "place"), "full"); place != "" {
				fields = append(fields, vcardField{Name: "BIRTHPLACE", Params: []vcardParam{{Name: "PROP-ID", Value: id}}, Value: place})
			}
		case "death":
			if date := anniversaryDateToVCard(mapFieldOrNil(obj, "date")); date != "" {
				fields = append(fields, vcardField{Name: "DEATHDATE", Params: []vcardParam{{Name: "PROP-ID", Value: id}}, Value: date})
			}
			if place := strField(mapFieldOrNil(obj, "place"), "full"); place != "" {
				fields = append(fields, vcardField{Name: "DEATHPLACE", Params: []vcardParam{{Name: "PROP-ID", Value: id}}, Value: place})
			}
		case "wedding":
			if date := anniversaryDateToVCard(mapFieldOrNil(obj, "date")); date != "" {
				fields = append(fields, vcardField{Name: "ANNIVERSARY", Params: []vcardParam{{Name: "PROP-ID", Value: id}}, Value: date})
			}
		}
	}
	return fields
}

// anniversaryDateToVCard renders a PartialDate or Timestamp as a vCard
// date/time value.
func anniversaryDateToVCard(date map[string]any) string {
	if date == nil {
		return ""
	}
	if utc, ok := date["utc"].(string); ok {
		if ts, ok := toVCardTimestamp(utc); ok {
			return ts
		}
		return ""
	}
	year, _ := numField(date, "year")
	month, _ := numField(date, "month")
	day, _ := numField(date, "day")
	if year == 0 {
		return ""
	}
	return fmt.Sprintf("%04d%02d%02d", year, month, day)
}

func (c *converter) convertSpeakToAs() []vcardField {
	obj, ok := mapField(c.card, "speakToAs")
	if !ok {
		return nil
	}
	var fields []vcardField
	if gender := strField(obj, "grammaticalGender"); gender != "" {
		fields = append(fields, vcardField{Name: "GRAMGENDER", Value: gender})
	}
	for _, id := range sortedKeys(mapFieldOrEmpty(obj, "pronouns")) {
		p := mapAt(obj, "pronouns", id)
		if p == nil {
			continue
		}
		f := vcardField{
			Name:   "PRONOUNS",
			Params: []vcardParam{{Name: "PROP-ID", Value: id}},
			Value:  strField(p, "pronouns"),
		}
		if pref, ok := numField(p, "pref"); ok && pref > 0 {
			f.Params = append(f.Params, vcardParam{Name: "PREF", Value: strconv.FormatUint(pref, 10)})
		}
		fields = append(fields, f)
	}
	return fields
}

func (c *converter) convertPreferredLanguages() []vcardField {
	var fields []vcardField
	for _, id := range sortedKeys(mapFieldOrEmpty(c.card, "preferredLanguages")) {
		obj := mapAt(c.card, "preferredLanguages", id)
		if obj == nil {
			continue
		}
		fields = append(fields, vcardField{
			Name:   "LANG",
			Params: commonParams(obj, id),
			Value:  strField(obj, "language"),
		})
	}
	return fields
}

// ---- vCardProps and unknown/vendor properties -----------------------------

func (c *converter) convertVCardProps() []vcardField {
	var fields []vcardField
	for _, raw := range strSliceField(c.card, "vCardProps") {
		prop, ok := raw.([]any)
		if !ok || len(prop) < 4 {
			continue
		}
		name, _ := prop[0].(string)
		paramsObj, _ := prop[1].(map[string]any)
		valueType, _ := prop[2].(string)
		value, _ := prop[3].(string)
		if name == "" {
			continue
		}
		f := vcardField{Name: strings.ToUpper(name), Value: value}
		if group, ok := paramsObj["group"].(string); ok && group != "" {
			f.Group = group
		}
		for _, k := range sortedKeys(paramsObj) {
			if k == "group" {
				continue
			}
			switch v := paramsObj[k].(type) {
			case string:
				f.Params = append(f.Params, vcardParam{Name: strings.ToUpper(k), Value: v})
			case bool:
				f.Params = append(f.Params, vcardParam{Name: strings.ToUpper(k), Value: strconv.FormatBool(v)})
			case float64:
				f.Params = append(f.Params, vcardParam{Name: strings.ToUpper(k), Value: strconv.FormatFloat(v, 'f', -1, 64)})
			}
		}
		switch valueType {
		case "uri":
			f.Params = append(f.Params, vcardParam{Name: "VALUE", Value: "URI"})
		case "text":
			f.Params = append(f.Params, vcardParam{Name: "VALUE", Value: "text"})
		}
		fields = append(fields, f)
	}
	return fields
}

// convertUnknownProps turns unknown and vendor-specific JSContact properties
// into JSPROP properties (RFC 9555 §3.2.1). The JSPTR parameter carries the
// JSON Pointer of the property, the value its JSON text.
func (c *converter) convertUnknownProps() []vcardField {
	var fields []vcardField
	c.walkUnknown(c.card, "", propSchema["Card"], &fields)
	return fields
}

func (c *converter) walkUnknown(obj map[string]any, prefix string, known map[string]string, out *[]vcardField) {
	for _, key := range sortedKeys(obj) {
		kind, ok := known[key]
		if !ok {
			*out = append(*out, jspropField(prefix+key, obj[key]))
			continue
		}
		switch {
		case kind == "leaf":
		case strings.HasPrefix(kind, "obj:"):
			if sub, ok := obj[key].(map[string]any); ok {
				c.walkUnknown(sub, prefix+key+"/", propSchema[strings.TrimPrefix(kind, "obj:")], out)
			}
		case strings.HasPrefix(kind, "map:"):
			if m, ok := obj[key].(map[string]any); ok {
				typeName := strings.TrimPrefix(kind, "map:")
				for _, id := range sortedKeys(m) {
					if sub, ok := m[id].(map[string]any); ok {
						c.walkUnknown(sub, prefix+key+"/"+id+"/", propSchema[typeName], out)
					}
				}
			}
		case strings.HasPrefix(kind, "arr:"):
			if arr, ok := obj[key].([]any); ok {
				typeName := strings.TrimPrefix(kind, "arr:")
				for i, el := range arr {
					if sub, ok := el.(map[string]any); ok {
						c.walkUnknown(sub, fmt.Sprintf("%s%s/%d/", prefix, key, i), propSchema[typeName], out)
					}
				}
			}
		case kind == "patch":
			// localizations patches are applied per-language; the patch
			// objects themselves are not emitted.
		}
	}
}

func jspropField(ptr string, value any) vcardField {
	b, _ := json.Marshal(value)
	return vcardField{
		Name:   "JSPROP",
		Params: []vcardParam{{Name: "JSPTR", Value: ptr}, {Name: "VALUE", Value: "TEXT"}},
		Value:  string(b),
	}
}

func jspropVersion(version string) vcardField {
	return vcardField{
		Name:   "JSPROP",
		Params: []vcardParam{{Name: "JSPTR", Value: "version"}, {Name: "VALUE", Value: "TEXT"}},
		Value:  jsonString(version),
	}
}

// ---- helpers --------------------------------------------------------------

// patchedCard returns the card with the localizations patch for language
// applied. The base card is returned unchanged for the empty language.
func (c *converter) patchedCard(language string) map[string]any {
	if language == "" {
		return c.card
	}
	langs := mapFieldOrEmpty(c.card, "localizations")
	patch, ok := langs[language].(map[string]any)
	if !ok {
		return c.card
	}
	patched, err := applyPatch(c.card, patch)
	if err != nil {
		return c.card
	}
	return patched
}

// toVCardTimestamp converts a JSContact timestamp (RFC 9553 UTCDateTime) into
// the vCard basic timestamp format.
func toVCardTimestamp(s string) (string, bool) {
	if len(s) != 20 || s[10] != 'T' || s[19] != 'Z' {
		return "", false
	}
	if s[4] != '-' || s[7] != '-' || s[13] != ':' || s[16] != ':' {
		return "", false
	}
	return s[:4] + s[5:7] + s[8:10] + "T" + s[11:13] + s[14:16] + s[17:19] + "Z", true
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mapField(m map[string]any, key string) (map[string]any, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	sub, ok := v.(map[string]any)
	return sub, ok
}

func mapFieldOrEmpty(m map[string]any, key string) map[string]any {
	sub, _ := mapField(m, key)
	return sub
}

func mapFieldOrNil(m map[string]any, key string) map[string]any {
	sub, ok := mapField(m, key)
	if !ok {
		return nil
	}
	return sub
}

func mapAt(m map[string]any, outer, key string) map[string]any {
	outerMap := mapFieldOrEmpty(m, outer)
	sub, _ := outerMap[key].(map[string]any)
	return sub
}

func strField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func numField(m map[string]any, key string) (uint64, bool) {
	switch v := m[key].(type) {
	case float64:
		if v < 0 {
			return 0, false
		}
		return uint64(v), true
	case uint64:
		return v, true
	}
	return 0, false
}

func strSliceField(m map[string]any, key string) []any {
	v, ok := m[key]
	if !ok {
		return nil
	}
	s, ok := v.([]any)
	if !ok {
		return nil
	}
	return s
}