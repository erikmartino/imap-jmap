package vcardconv

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/emersion/go-vcard"
)

// FromVCard converts a vCard 4.0 document (RFC 6350) into a JSContact Card
// (RFC 9553) following RFC 9555 and RFC 9554.
func FromVCard(text string) (map[string]any, error) {
	fixed, err := repairVCard(text)
	if err != nil {
		return nil, err
	}
	dec := vcard.NewDecoder(strings.NewReader(fixed + "\n"))
	card, err := dec.Decode()
	if err != nil {
		return nil, fmt.Errorf("vcardconv: parsing vCard: %w", err)
	}
	return cardToJSCard(card)
}

// repairVCard unfolds continuation lines and rewrites backslash escapes inside
// quoted parameter values so that go-vcard's decoder can parse them. go-vcard
// uses strconv.UnquoteChar for quoted parameter values, which rejects the
// "\," and "\;" escapes that RFC 9555 §3.3.1 mandates for JSCOMPS; rewriting
// them to their literal characters before parsing sidesteps that limitation.
// Because the rewritten COMMA re-splits the parameter value, callers must
// rejoin multi-piece parameter values with ",".
func repairVCard(text string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t")) {
			line += strings.TrimLeft(lines[i+1], " \t")
			i++
		}
		out = append(out, repairLine(line))
	}
	return strings.Join(out, "\n"), nil
}

// repairLine rewrites "\," and "\;" inside quoted parameter values to their
// literal characters. The property value (after the first unquoted COLON) is
// left untouched.
func repairLine(line string) string {
	if !strings.Contains(line, `\`) {
		return line
	}
	var b strings.Builder
	b.Grow(len(line))
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inQuote {
			if c == '\\' && i+1 < len(line) && (line[i+1] == ',' || line[i+1] == ';') {
				b.WriteByte(line[i+1])
				i++
				continue
			}
			b.WriteByte(c)
			if c == '"' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '"':
			inQuote = true
			b.WriteByte(c)
		case ':':
			// Everything from the value delimiter onwards is the property
			// value; no repairs apply.
			b.WriteString(line[i:])
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// joinParams reconstructs a parameter value that go-vcard split on commas.
func joinParams(vs []string) string { return strings.Join(vs, ",") }

// param returns the joined value of a field parameter.
func param(f *vcard.Field, name string) string {
	return joinParams(f.Params[name])
}

func paramValues(f *vcard.Field, name string) []string { return f.Params[name] }

func hasParam(f *vcard.Field, name string) bool {
	return len(f.Params[name]) > 0
}

// fieldGroup returns the (ALTID, LANGUAGE, phonetic) grouping key of a field,
// and whether the field belongs to a localization group at all.
func fieldGroup(f *vcard.Field) (altID, lang string) {
	return param(f, "ALTID"), param(f, "LANGUAGE")
}

func cardToJSCard(card vcard.Card) (map[string]any, error) {
	out := map[string]any{}
	out["@type"] = "Card"

	if uid := card.Value(vcard.FieldUID); uid != "" {
		out["uid"] = uid
	}
	out["version"] = cardVersion(card)
	if kind := card.Value(vcard.FieldKind); kind != "" {
		out["kind"] = strings.ToLower(kind)
	}
	if pid := card.Value(vcard.FieldProductID); pid != "" {
		out["prodId"] = pid
	}
	if lang := card.Value(vcard.FieldLanguage); lang != "" {
		out["language"] = lang
	}
	if rev := card.Value(vcard.FieldRevision); rev != "" {
		if ts, ok := timestampToISO(rev); ok {
			out["updated"] = ts
		}
	}
	if created := card.Value("CREATED"); created != "" {
		if ts, ok := timestampToISO(created); ok {
			out["created"] = ts
		}
	}

	applyNames(out, card)
	applyOrganizationsAndTitles(out, card)
	applyEmails(out, card)
	applyPhones(out, card)
	applyOnlineServices(out, card)
	applyLinks(out, card)
	applyCalendars(out, card)
	applySchedulingAddresses(out, card)
	applyAddresses(out, card)
	applyDirectories(out, card)
	applyPersonalInfo(out, card)
	applyCryptoKeys(out, card)
	applyMedia(out, card)
	applyNotes(out, card)
	applyKeywords(out, card)
	applyMembers(out, card)
	applyRelatedTo(out, card)
	applyAnniversaries(out, card)
	applySpeakToAs(out, card)
	applyPreferredLanguages(out, card)
	applyVCardPropsAndJSPROP(out, card)

	return out, nil
}

// cardVersion determines the JSContact Card version. The JSPROP;JSPTR=version
// property carries the version as a JSON string; a VERSION:4.0 vCard maps to
// version "1.0" (RFC 9555 §2.1.2).
func cardVersion(card vcard.Card) string {
	for _, f := range card[vcard.FieldVersion] {
		_ = f
	}
	for _, f := range card["JSPROP"] {
		if param(f, "JSPTR") == "version" {
			var v string
			if err := json.Unmarshal([]byte(unescapeJSONText(f.Value)), &v); err == nil {
				return v
			}
		}
	}
	return "1.0"
}

// unescapeJSONText removes the vCard-level escapes (backslash-comma and
// double backslash) from a JSPROP value so it can be parsed as JSON.
func unescapeJSONText(s string) string {
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\,`, `,`)
	return s
}

func timestampToISO(ts string) (string, bool) {
	if len(ts) < 15 {
		return "", false
	}
	// Basic form: YYYYMMDDTHHMMSS[Z]
	if len(ts) == 15 && ts[14] == 'Z' {
		ts = ts[:14] + "00" + "Z"
	}
	if len(ts) == 18 {
		ts = ts[:14] + ts[14:16] + "Z"
	}
	if len(ts) != 20 || ts[8] != 'T' {
		return "", false
	}
	y, err1 := strconv.Atoi(ts[0:4])
	mo, err2 := strconv.Atoi(ts[4:6])
	d, err3 := strconv.Atoi(ts[6:8])
	h, err4 := strconv.Atoi(ts[9:11])
	mi, err5 := strconv.Atoi(ts[11:13])
	s, err6 := strconv.Atoi(ts[13:15])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil {
		return "", false
	}
	_ = y
	_ = mo
	_ = d
	_ = h
	_ = mi
	_ = s
	return fmt.Sprintf("%s-%s-%sT%s:%s:%sZ", ts[0:4], ts[4:6], ts[6:8], ts[9:11], ts[11:13], ts[13:15]), true
}

// ---- name / FN / N --------------------------------------------------------

// nameGroup is one ALTID/LANGUAGE variant of the name.
type nameGroup struct {
	altID, lang string
	base        bool
	fn          *vcard.Field
	ns          []*vcard.Field // N fields (base + phonetic)
}

// applyNames builds the name object and localizations from FN and N fields,
// honoring ALTID/LANGUAGE grouping and PHONETIC representations.
func applyNames(out map[string]any, card vcard.Card) {
	fns := card[vcard.FieldFormattedName]
	ns := card[vcard.FieldName]

	groups := map[string]*nameGroup{}
	var order []string
	groupKey := func(altID, lang string) string {
		if lang != "" {
			return "l:" + altID + ":" + lang
		}
		return "b:" + altID
	}
	for _, fn := range fns {
		altID, lang := fieldGroup(fn)
		key := groupKey(altID, lang)
		if groups[key] == nil {
			groups[key] = &nameGroup{altID: altID, lang: lang, base: lang == ""}
			order = append(order, key)
		}
		groups[key].fn = fn
	}
	// N fields: base N (no PHONETIC) belongs to its (ALTID, LANGUAGE) group;
	// phonetic Ns are attached to the group with the same ALTID.
	for _, n := range ns {
		altID, lang := fieldGroup(n)
		key := groupKey(altID, lang)
		g := groups[key]
		if g == nil {
			g = &nameGroup{altID: altID, lang: lang, base: lang == ""}
			groups[key] = g
			order = append(order, key)
		}
		if hasParam(n, "PHONETIC") {
			g.ns = append(g.ns, n)
		} else {
			// Prepend base N so the non-phonetic variant comes first.
			g.ns = append([]*vcard.Field{n}, g.ns...)
		}
	}

	for _, key := range order {
		g := groups[key]
		name := nameFromGroup(g)
		if g.base {
			if name != nil {
				out["name"] = name
			} else if g.fn != nil {
				// Only FN, no N: still a name with a full value.
				out["name"] = map[string]any{"full": g.fn.Value}
			}
			continue
		}
		if name == nil || name == nil && g.fn == nil {
			continue
		}
		langs := mapFieldOrEmpty(out, "localizations")
		if langs == nil {
			langs = map[string]any{}
		}
		if name == nil {
			continue
		}
		if comps, ok := name["components"]; ok {
			patch := map[string]any{}
			if out["name"] != nil {
				patch["name/components"] = comps
			}
			langs[g.lang] = patch
		}
		out["localizations"] = langs
	}
}

// nameFromGroup builds a name object from a group's N fields.
func nameFromGroup(g *nameGroup) map[string]any {
	if len(g.ns) == 0 {
		return nil
	}
	base := g.ns[0]
	if hasParam(base, "PHONETIC") {
		return nil
	}
	parsed := parseNameField(base)
	name := map[string]any{}

	comps := parsed.components
	if parsed.isOrdered || parsed.hasSeparators {
		name["isOrdered"] = true
		if parsed.hasDefaultSeparator {
			name["defaultSeparator"] = parsed.defaultSeparator
		}
	} else {
		name["isOrdered"] = false
	}
	orderedComps := []any{}
	for _, c := range comps {
		obj := map[string]any{"kind": c.kind, "value": c.value}
		orderedComps = append(orderedComps, obj)
	}
	if len(orderedComps) > 0 {
		name["components"] = orderedComps
	}

	if parsed.sortAs != nil {
		name["sortAs"] = parsed.sortAs
	}

	// Phonetic representation.
	if len(g.ns) > 1 {
		p := g.ns[1]
		if phonetic := param(p, "PHONETIC"); phonetic != "" {
			name["phoneticSystem"] = phonetic
			pp := parseNameField(p)
			byKind := map[string]string{}
			for _, c := range pp.components {
				byKind[c.kind] = c.value
			}
			compsOut := make([]any, 0, len(orderedComps))
			for i, c := range orderedComps {
				obj := c.(map[string]any)
				if pv, ok := byKind[obj["kind"].(string)]; ok {
					obj["phonetic"] = pv
				}
				compsOut = append(compsOut, obj)
				_ = i
			}
			name["components"] = compsOut
		}
	}

	// full from the FN of the same group, unless it is derived.
	if g.fn != nil && !hasParam(g.fn, "DERIVED") {
		name["full"] = g.fn.Value
	}
	return name
}

type parsedName struct {
	components          []component
	isOrdered           bool
	hasSeparators       bool
	hasDefaultSeparator bool
	defaultSeparator    string
	sortAs              map[string]any
}

func parseNameField(f *vcard.Field) parsedName {
	raw := strings.Split(f.Value, ";")
	get := func(i int) []string {
		if i >= len(raw) {
			return nil
		}
		if raw[i] == "" {
			return nil
		}
		return strings.Split(raw[i], ",")
	}
	family := get(0)
	given := get(1)
	given2 := get(2)
	prefix := get(3)
	suffix := get(4)
	s2 := get(5)
	gen := get(6)

	// De-duplicate values repeated for compatibility: the family component
	// carries surname+surname2, the suffix component carries
	// credential+generation (RFC 9554 §2.2).
	surname := difference(family, s2)
	credential := difference(suffix, gen)

	var comps []component
	jscomps := joinParams(f.Params["JSCOMPS"])
	if jscomps != "" {
		entries, err := decodeJSCOMPS(jscomps, nameKindByPos)
		if err == nil {
			for _, c := range entries.order {
				if c.kind == "separator" {
					comps = append(comps, component{kind: "separator", value: c.value})
					continue
				}
				var value string
				switch c.kind {
				case "surname":
					value = strings.Join(surname, ",")
				case "surname2":
					value = strings.Join(s2, ",")
				case "given":
					value = strings.Join(given, ",")
				case "given2":
					value = strings.Join(given2, ",")
				case "title":
					value = strings.Join(prefix, ",")
				case "credential":
					value = strings.Join(credential, ",")
				case "generation":
					value = strings.Join(gen, ",")
				}
				if value != "" {
					comps = append(comps, component{kind: c.kind, value: value})
				}
			}
			return parsedName{
				components:          comps,
				isOrdered:           true,
				hasSeparators:       true,
				hasDefaultSeparator: entries.defaultSeparator != "" || strings.HasPrefix(jscomps, "s,"),
				defaultSeparator:    entries.defaultSeparator,
				sortAs:              parseSortAs(param(f, "SORT-AS")),
			}
		}
	}

	comps = appendComps(comps, "surname", surname)
	comps = appendComps(comps, "given", given)
	comps = appendComps(comps, "given2", given2)
	comps = appendComps(comps, "title", prefix)
	comps = appendComps(comps, "credential", credential)
	comps = appendComps(comps, "surname2", s2)
	comps = appendComps(comps, "generation", gen)
	return parsedName{
		components:          comps,
		isOrdered:           false,
		hasSeparators:       false,
		hasDefaultSeparator: false,
		sortAs:              parseSortAs(param(f, "SORT-AS")),
	}
}

func appendComps(comps []component, kind string, values []string) []component {
	for _, v := range values {
		if v != "" {
			comps = append(comps, component{kind: kind, value: v})
		}
	}
	return comps
}

func difference(a, b []string) []string {
	var out []string
	for _, v := range a {
		skip := false
		for _, w := range b {
			if v == w {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, v)
		}
	}
	return out
}

func parseSortAs(s string) map[string]any {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	sortAs := map[string]any{}
	if len(parts) > 0 {
		sortAs["surname"] = parts[0]
	}
	if len(parts) > 1 {
		sortAs["given"] = parts[1]
	}
	return sortAs
}

// ---- organizations and titles ---------------------------------------------

func applyOrganizationsAndTitles(out map[string]any, card vcard.Card) {
	orgs := map[string]any{}
	titles := map[string]any{}
	orgByGroup := map[string]string{}

	for _, f := range card[vcard.FieldOrganization] {
		id := param(f, "PROP-ID")
		if id == "" {
			id = nextID(orgs, "o")
		}
		parts := strings.Split(f.Value, ";")
		obj := map[string]any{}
		if len(parts) > 0 && parts[0] != "" {
			obj["name"] = parts[0]
		}
		if len(parts) > 1 {
			var units []any
			for _, u := range parts[1:] {
				if u != "" {
					units = append(units, map[string]any{"name": u})
				}
			}
			if len(units) > 0 {
				obj["units"] = units
			}
		}
		if sortAs := param(f, "SORT-AS"); sortAs != "" {
			obj["sortAs"] = sortAs
		}
		orgs[id] = obj
		if f.Group != "" {
			orgByGroup[f.Group] = id
		}
	}

	for _, name := range []string{"TITLE", "ROLE"} {
		for _, f := range card[name] {
			id := param(f, "PROP-ID")
			if id == "" {
				id = nextID(titles, "t")
			}
			obj := map[string]any{"kind": strings.ToLower(name), "name": f.Value}
			if f.Group != "" {
				if orgID, ok := orgByGroup[f.Group]; ok {
					obj["organizationId"] = orgID
				}
			}
			_, lang := fieldGroup(f)
			if lang != "" {
				if !applyLocalizationPatch(out, lang, "titles/"+id+"/name", f.Value) {
					continue
				}
			}
			titles[id] = obj
		}
	}
	if len(orgs) > 0 {
		out["organizations"] = orgs
	}
	if len(titles) > 0 {
		out["titles"] = titles
	}
}

// applyLocalizationPatch records a localized property value under
// localizations[lang][path] and reports whether it was recorded.
func applyLocalizationPatch(out map[string]any, lang, path, value string) bool {
	langs := mapFieldOrEmpty(out, "localizations")
	if langs == nil {
		langs = map[string]any{}
	}
	patch, ok := langs[lang].(map[string]any)
	if !ok {
		patch = map[string]any{}
	}
	patch[path] = value
	langs[lang] = patch
	out["localizations"] = langs
	return true
}

// ---- emails / phones ------------------------------------------------------

func propID(out map[string]any, section, propName string, f *vcard.Field) string {
	id := param(f, "PROP-ID")
	if id == "" {
		id = nextID(mapFieldOrEmpty(out, section), propName[0:1])
	}
	return id
}

func nextID(m map[string]any, prefix string) string {
	n := len(m) + 1
	for {
		id := fmt.Sprintf("%s%d", prefix, n)
		if m[id] == nil {
			return id
		}
		n++
	}
}

func applyEmails(out map[string]any, card vcard.Card) {
	emails := map[string]any{}
	for _, f := range card[vcard.FieldEmail] {
		id := propID(out, "emails", "EMAIL", f)
		obj := map[string]any{"address": f.Value}
		applyContextsAndPref(obj, f)
		if label := param(f, "LABEL"); label != "" {
			obj["label"] = label
		}
		emails[id] = obj
	}
	if len(emails) > 0 {
		out["emails"] = emails
	}
}

func applyPhones(out map[string]any, card vcard.Card) {
	phones := map[string]any{}
	for _, f := range card[vcard.FieldTelephone] {
		id := propID(out, "phones", "TEL", f)
		obj := map[string]any{"number": f.Value}
		applyContextsAndPref(obj, f)
		features := map[string]any{}
		for _, t := range f.Params.Types() {
			switch t {
			case "voice", "fax", "cell", "video", "pager", "text", "textphone", "main-number", "mobile":
				features[featureFromType(t)] = true
			}
		}
		if len(features) > 0 {
			obj["features"] = features
		}
		if label := param(f, "LABEL"); label != "" {
			obj["label"] = label
		}
		phones[id] = obj
	}
	if len(phones) > 0 {
		out["phones"] = phones
	}
}

func featureFromType(t string) string {
	switch t {
	case "cell":
		return "mobile"
	default:
		return t
	}
}

// applyContextsAndPref maps TYPE (work/home), PREF and X-* vCardParams onto a
// JSContact object.
func applyContextsAndPref(obj map[string]any, f *vcard.Field) {
	contexts := map[string]any{}
	for _, t := range f.Params.Types() {
		switch t {
		case "work":
			contexts["work"] = true
		case "home":
			contexts["private"] = true
		}
	}
	if len(contexts) > 0 {
		obj["contexts"] = contexts
	}
	if pref := param(f, "PREF"); pref != "" {
		if n, err := strconv.ParseUint(pref, 10, 32); err == nil {
			obj["pref"] = n
		}
	}
	var vp map[string]any
	for k, vs := range f.Params {
		uk := strings.ToLower(k)
		switch uk {
		case "pref", "type", "label", "value", "altid", "language", "group", "props", "pid", "index", "sort-as", "tz", "phonetic", "jscomps", "prop-id", "created", "author-name", "author", "level", "username", "service-type", "x-":
			continue
		}
		_ = uk
		if vp == nil {
			vp = map[string]any{}
		}
		vp[strings.ToLower(k)] = joinParams(vs)
	}
	if vp != nil {
		obj["vCardParams"] = vp
	}
}

// ---- online services / links / calendars / scheduling ----------------------

func applyOnlineServices(out map[string]any, card vcard.Card) {
	services := map[string]any{}
	labels := map[string]string{}
	for _, f := range card["X-SOCIALPROFILE"] {
		labels[f.Group] = param(f, "LABEL")
	}
	for _, f := range card["IMPP"] {
		labels[f.Group] = param(f, "LABEL")
	}
	for _, f := range card["IMPP"] {
		id := propID(out, "onlineServices", "IMPP", f)
		obj := map[string]any{"uri": f.Value, "vCardName": "impp"}
		applyContextsAndPref(obj, f)
		if label := labels[f.Group]; label != "" {
			obj["label"] = label
		}
		services[id] = obj
	}
	for _, f := range card["X-SOCIALPROFILE"] {
		id := propID(out, "onlineServices", "X-SOCIALPROFILE", f)
		obj := map[string]any{"uri": f.Value}
		if svc := param(f, "SERVICE-TYPE"); svc != "" {
			obj["service"] = svc
		}
		if user := param(f, "USERNAME"); user != "" {
			obj["user"] = user
		}
		applyContextsAndPref(obj, f)
		if label := labels[f.Group]; label != "" {
			obj["label"] = label
		}
		services[id] = obj
	}
	if len(services) > 0 {
		out["onlineServices"] = services
	}
}

func applyLinks(out map[string]any, card vcard.Card) {
	links := map[string]any{}
	for _, name := range []string{"URL", "CONTACT-URI"} {
		for _, f := range card[name] {
			id := propID(out, "links", name, f)
			obj := map[string]any{"uri": f.Value}
			if name == "CONTACT-URI" {
				obj["kind"] = "contact"
			}
			applyContextsAndPref(obj, f)
			links[id] = obj
		}
	}
	if len(links) > 0 {
		out["links"] = links
	}
}

func applyCalendars(out map[string]any, card vcard.Card) {
	calendars := map[string]any{}
	for _, f := range card[vcard.FieldCalendarURI] {
		id := propID(out, "calendars", "CALURI", f)
		obj := map[string]any{"kind": "calendar", "uri": f.Value}
		applyContextsAndPref(obj, f)
		calendars[id] = obj
	}
	for _, f := range card[vcard.FieldFreeOrBusyURL] {
		id := propID(out, "calendars", "FBURL", f)
		obj := map[string]any{"kind": "freeBusy", "uri": f.Value}
		applyContextsAndPref(obj, f)
		calendars[id] = obj
	}
	if len(calendars) > 0 {
		out["calendars"] = calendars
	}
}

func applySchedulingAddresses(out map[string]any, card vcard.Card) {
	sched := map[string]any{}
	for _, f := range card[vcard.FieldCalendarAddressURI] {
		id := propID(out, "schedulingAddresses", "CALADRURI", f)
		obj := map[string]any{"uri": f.Value}
		applyContextsAndPref(obj, f)
		if label := param(f, "LABEL"); label != "" {
			obj["label"] = label
		}
		sched[id] = obj
	}
	if len(sched) > 0 {
		out["schedulingAddresses"] = sched
	}
}

// ---- addresses ------------------------------------------------------------

func applyAddresses(out map[string]any, card vcard.Card) {
	addresses := map[string]any{}
	byGroup := map[string]string{}

	// ADR fields are processed in (ALTID, LANGUAGE) groups so localizations
	// can be reconstructed; the base variant (no LANGUAGE) builds the address.
	type addrVariant struct {
		altID, lang string
		field       *vcard.Field
	}
	var variants []addrVariant
	for _, f := range card[vcard.FieldAddress] {
		altID, lang := fieldGroup(f)
		variants = append(variants, addrVariant{altID: altID, lang: lang, field: f})
	}
	for _, v := range variants {
		f := v.field
		if v.lang != "" {
			obj := addressFromField(f)
			if full, ok := obj["full"]; ok && full != "" {
				id := param(f, "PROP-ID")
				if applyLocalizationPatch(out, v.lang, "addresses/"+id+"/full", full.(string)) {
				}
			}
			continue
		}
		id := propID(out, "addresses", "ADR", f)
		obj := addressFromField(f)
		if f.Group != "" {
			byGroup[f.Group] = id
		}
		addresses[id] = obj
	}
	if len(addresses) > 0 {
		out["addresses"] = addresses
	}
	_ = byGroup
}

func addressFromField(f *vcard.Field) map[string]any {
	obj := map[string]any{}
	raw := strings.Split(f.Value, ";")
	get := func(i int) string {
		if i >= len(raw) {
			return ""
		}
		return raw[i]
	}
	hasNewFields := len(raw) > 7 && strings.Join(raw[7:], "") != ""

	comps := []any{}
	street := splitComma(get(2))
	ext := splitComma(get(1))
	if hasNewFields {
		// New-style fields (room..direction) take precedence; legacy
		// street/ext values are ignored when present (RFC 9554 §2.1).
		posKinds := map[int]string{
			7: "room", 8: "apartment", 9: "floor", 10: "number",
			11: "name", 12: "building", 13: "block", 14: "subdistrict",
			15: "district", 16: "landmark", 17: "direction",
		}
		var all []component
		for i := 7; i < len(raw) && i <= 17; i++ {
			kind := posKinds[i]
			for _, v := range splitComma(raw[i]) {
				if v != "" {
					all = append(all, component{kind: kind, value: v})
				}
			}
		}
		// Prefix the classic single-value fields.
		if v := get(3); v != "" {
			all = append([]component{{kind: "locality", value: v}}, all...)
		}
		if v := get(4); v != "" {
			all = append([]component{{kind: "region", value: v}}, all...)
		}
		if v := get(5); v != "" {
			all = append([]component{{kind: "postcode", value: v}}, all...)
		}
		if v := get(6); v != "" {
			all = append([]component{{kind: "country", value: v}}, all...)
		}
		if v := get(0); v != "" {
			all = append([]component{{kind: "postOfficeBox", value: v}}, all...)
		}
		for _, c := range all {
			comps = append(comps, map[string]any{"kind": c.kind, "value": c.value})
		}
	} else {
		add := func(kind, value string) {
			for _, v := range splitComma(value) {
				if v != "" {
					comps = append(comps, map[string]any{"kind": kind, "value": v})
				}
			}
		}
		add("postOfficeBox", get(0))
		add("room", get(7))
		add("apartment", get(8))
		add("floor", get(9))
		add("number", get(10))
		add("name", get(11))
		add("building", get(12))
		add("block", get(13))
		add("subdistrict", get(14))
		add("district", get(15))
		add("landmark", get(16))
		add("direction", get(17))
		// Legacy street and ext values are decomposed into the new fields.
		add("number", get(2))
		add("name", get(2))
		add("block", get(2))
		add("direction", get(2))
		add("landmark", get(2))
		add("subdistrict", get(2))
		add("district", get(2))
		_ = ext
		_ = street
	}

	if len(comps) > 0 {
		obj["components"] = comps
	}
	if full := param(f, "LABEL"); full != "" {
		obj["full"] = full
	}
	if tz := param(f, "TZ"); tz != "" {
		obj["timeZone"] = tz
	}
	applyContextsAndPref(obj, f)

	jscomps := joinParams(f.Params["JSCOMPS"])
	if jscomps != "" {
		if entries, err := decodeJSCOMPS(jscomps, addrKindByPos); err == nil {
			obj["isOrdered"] = true
			if entries.defaultSeparator != "" || strings.HasPrefix(jscomps, "s,") {
				obj["defaultSeparator"] = entries.defaultSeparator
			}
		}
	}
	return obj
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// ---- directories / personal info / crypto keys / media / notes -------------

func applyDirectories(out map[string]any, card vcard.Card) {
	dirs := map[string]any{}
	for _, f := range card["SOURCE"] {
		id := propID(out, "directories", "SOURCE", f)
		obj := map[string]any{"kind": "entry", "uri": f.Value}
		applyContextsAndPref(obj, f)
		dirs[id] = obj
	}
	for _, f := range card["ORG-DIRECTORY"] {
		id := propID(out, "directories", "ORG-DIRECTORY", f)
		obj := map[string]any{"kind": "directory", "uri": f.Value}
		applyContextsAndPref(obj, f)
		dirs[id] = obj
	}
	if len(dirs) > 0 {
		out["directories"] = dirs
	}
}

func applyPersonalInfo(out map[string]any, card vcard.Card) {
	info := map[string]any{}
	for _, name := range []string{"EXPERTISE", "HOBBY", "INTEREST"} {
		for _, f := range card[name] {
			id := propID(out, "personalInfo", name, f)
			obj := map[string]any{"kind": strings.ToLower(name), "value": f.Value}
			if level := param(f, "LEVEL"); level != "" {
				obj["level"] = levelFromVCard(strings.ToLower(name), level)
			}
			if index := param(f, "INDEX"); index != "" {
				if n, err := strconv.ParseUint(index, 10, 32); err == nil {
					obj["listAs"] = n
				}
			}
			info[id] = obj
		}
	}
	if len(info) > 0 {
		out["personalInfo"] = info
	}
}

// levelFromVCard maps a vCard LEVEL parameter to the JSContact level
// (RFC 9555 §2.3.13): expertise beginner/average/expert map to low/medium/
// high, other kinds verbatim.
func levelFromVCard(kind, level string) string {
	level = strings.ToLower(level)
	if kind == "expertise" {
		switch level {
		case "beginner":
			return "low"
		case "average":
			return "medium"
		case "expert":
			return "high"
		}
	}
	return level
}

func applyCryptoKeys(out map[string]any, card vcard.Card) {
	keys := map[string]any{}
	for _, f := range card[vcard.FieldKey] {
		id := propID(out, "cryptoKeys", "KEY", f)
		obj := map[string]any{"uri": f.Value}
		applyContextsAndPref(obj, f)
		keys[id] = obj
	}
	if len(keys) > 0 {
		out["cryptoKeys"] = keys
	}
}

func applyMedia(out map[string]any, card vcard.Card) {
	media := map[string]any{}
	for _, name := range []string{"PHOTO", "LOGO", "SOUND"} {
		for _, f := range card[name] {
			id := propID(out, "media", name, f)
			obj := map[string]any{"kind": strings.ToLower(name), "uri": f.Value}
			applyContextsAndPref(obj, f)
			media[id] = obj
		}
	}
	if len(media) > 0 {
		out["media"] = media
	}
}

func applyNotes(out map[string]any, card vcard.Card) {
	notes := map[string]any{}
	for _, f := range card[vcard.FieldNote] {
		id := propID(out, "notes", "NOTE", f)
		obj := map[string]any{"note": f.Value}
		if created := param(f, "CREATED"); created != "" {
			if ts, ok := timestampToISO(created); ok {
				obj["created"] = ts
			}
		}
		if authorName := param(f, "AUTHOR-NAME"); authorName != "" {
			author := map[string]any{}
			if uri := param(f, "AUTHOR"); uri != "" {
				author["uri"] = uri
			}
			author["name"] = authorName
			obj["author"] = author
		}
		notes[id] = obj
	}
	if len(notes) > 0 {
		out["notes"] = notes
	}
}

// ---- keywords / members / relatedTo / anniversaries / speakToAs / langs ----

func applyKeywords(out map[string]any, card vcard.Card) {
	values := splitComma(card.PreferredValue(vcard.FieldCategories))
	kw := map[string]any{}
	for _, v := range values {
		if v != "" {
			kw[v] = true
		}
	}
	if len(kw) > 0 {
		out["keywords"] = kw
	}
}

func applyMembers(out map[string]any, card vcard.Card) {
	members := map[string]any{}
	for _, f := range card[vcard.FieldMember] {
		members[f.Value] = true
	}
	if len(members) > 0 {
		out["members"] = members
	}
}

func applyRelatedTo(out map[string]any, card vcard.Card) {
	related := map[string]any{}
	for _, f := range card[vcard.FieldRelated] {
		obj := map[string]any{}
		types := map[string]any{}
		for _, t := range f.Params.Types() {
			types[t] = true
		}
		if len(types) > 0 {
			obj["relation"] = types
		}
		related[f.Value] = obj
	}
	if len(related) > 0 {
		out["relatedTo"] = related
	}
}

func applyAnniversaries(out map[string]any, card vcard.Card) {
	anns := map[string]any{}
	dates := map[string]any{}
	places := map[string]any{}
	for _, f := range card[vcard.FieldBirthday] {
		dates[param(f, "PROP-ID")] = f.Value
	}
	for _, f := range card["DEATHDATE"] {
		dates[param(f, "PROP-ID")] = f.Value
	}
	for _, f := range card[vcard.FieldAnniversary] {
		dates[param(f, "PROP-ID")] = f.Value
	}
	for _, f := range card["BIRTHPLACE"] {
		places[param(f, "PROP-ID")] = f.Value
	}
	for _, f := range card["DEATHPLACE"] {
		places[param(f, "PROP-ID")] = f.Value
	}
	add := func(id, kind string) {
		if id == "" {
			id = nextID(anns, "k")
		}
		obj := map[string]any{"kind": kind}
		if d, ok := dates[id]; ok {
			if s, ok := d.(string); ok {
				obj["date"] = parseAnniversaryDate(s)
			}
		}
		if p, ok := places[id]; ok {
			obj["place"] = map[string]any{"full": p}
		}
		anns[id] = obj
	}
	for _, f := range card[vcard.FieldBirthday] {
		add(param(f, "PROP-ID"), "birth")
	}
	for _, f := range card["DEATHDATE"] {
		add(param(f, "PROP-ID"), "death")
	}
	for _, f := range card[vcard.FieldAnniversary] {
		add(param(f, "PROP-ID"), "wedding")
	}
	if len(anns) > 0 {
		out["anniversaries"] = anns
	}
}

func parseAnniversaryDate(v string) map[string]any {
	if len(v) >= 15 && v[8] == 'T' {
		if iso, ok := timestampToISO(v); ok {
			return map[string]any{"@type": "Timestamp", "utc": iso}
		}
	}
	if len(v) >= 8 {
		if y, err1 := strconv.Atoi(v[0:4]); err1 == nil {
			mo, err2 := strconv.Atoi(v[4:6])
			d, err3 := strconv.Atoi(v[6:8])
			if err2 == nil && err3 == nil {
				return map[string]any{"@type": "PartialDate", "year": y, "month": mo, "day": d}
			}
		}
	}
	return map[string]any{}
}

func applySpeakToAs(out map[string]any, card vcard.Card) {
	var obj map[string]any
	pronouns := map[string]any{}
	hasPronouns := false
	for _, f := range card["GRAMGENDER"] {
		if obj == nil {
			obj = map[string]any{}
		}
		obj["grammaticalGender"] = strings.ToLower(f.Value)
	}
	for _, f := range card["PRONOUNS"] {
		id := propID(out, "speakToAs", "PRONOUNS", f)
		p := map[string]any{"pronouns": f.Value}
		if pref := param(f, "PREF"); pref != "" {
			if n, err := strconv.ParseUint(pref, 10, 32); err == nil {
				p["pref"] = n
			}
		}
		pronouns[id] = p
		hasPronouns = true
	}
	if hasPronouns {
		if obj == nil {
			obj = map[string]any{}
		}
		obj["pronouns"] = pronouns
	}
	if obj != nil {
		out["speakToAs"] = obj
	}
}

func applyPreferredLanguages(out map[string]any, card vcard.Card) {
	langs := map[string]any{}
	for _, f := range card[vcard.FieldLanguage] {
		id := propID(out, "preferredLanguages", "LANG", f)
		obj := map[string]any{"language": f.Value}
		applyContextsAndPref(obj, f)
		langs[id] = obj
	}
	if len(langs) > 0 {
		out["preferredLanguages"] = langs
	}
}

// ---- vCardProps and JSPROP ------------------------------------------------

// handledProps lists the vCard properties consumed by the conversion; all
// remaining properties are preserved in vCardProps.
var handledProps = map[string]bool{
	"VERSION": true, "UID": true, "KIND": true, "PRODID": true,
	"REV": true, "CREATED": true, "LANGUAGE": true, "FN": true, "N": true,
	"NICKNAME": true, "ORG": true, "TITLE": true, "ROLE": true, "EMAIL": true,
	"TEL": true, "IMPP": true, "X-SOCIALPROFILE": true, "URL": true,
	"CONTACT-URI": true, "CALURI": true, "FBURL": true, "CALADRURI": true,
	"ADR": true, "SOURCE": true, "ORG-DIRECTORY": true, "EXPERTISE": true,
	"HOBBY": true, "INTEREST": true, "KEY": true, "PHOTO": true, "LOGO": true,
	"SOUND": true, "NOTE": true, "CATEGORIES": true, "MEMBER": true,
	"RELATED": true, "BDAY": true, "DEATHDATE": true, "ANNIVERSARY": true,
	"BIRTHPLACE": true, "DEATHPLACE": true, "GRAMGENDER": true, "PRONOUNS": true,
	"LANG": true, "JSPROP": true, "X-ABLABEL": true,
}

func applyVCardPropsAndJSPROP(out map[string]any, card vcard.Card) {
	var props []any
	for name, fields := range card {
		upper := strings.ToUpper(name)
		if handledProps[upper] || upper == "BEGIN" || upper == "END" {
			continue
		}
		for _, f := range fields {
			entry := vCardPropEntry(upper, f)
			props = append(props, entry)
		}
	}
	if len(props) > 0 {
		out["vCardProps"] = props
	}

	// JSPROP properties (other than version) carry unknown/vendor JSContact
	// properties as JSON pointers.
	for _, f := range card["JSPROP"] {
		ptr := param(f, "JSPTR")
		if ptr == "" || ptr == "version" {
			continue
		}
		applyJSPTR(out, ptr, f.Value)
	}
}

func vCardPropEntry(name string, f *vcard.Field) []any {
	params := map[string]any{}
	if f.Group != "" {
		params["group"] = f.Group
	}
	for k, vs := range f.Params {
		uk := strings.ToLower(k)
		v := joinParams(vs)
		switch uk {
		case "pref", "pid", "index":
			if n, err := strconv.ParseUint(v, 10, 32); err == nil {
				params[uk] = n
				continue
			}
		}
		params[uk] = v
	}
	valueType := "unknown"
	if f.Params.Get("VALUE") != "" {
		valueType = strings.ToLower(f.Params.Get("VALUE"))
	}
	return []any{strings.ToLower(name), params, valueType, f.Value}
}

// applyJSPTR sets a JSContact property addressed by a JSON Pointer.
func applyJSPTR(out map[string]any, ptr, rawValue string) {
	var value any
	if err := json.Unmarshal([]byte(unescapeJSONText(rawValue)), &value); err != nil {
		return
	}
	segs := splitPointer(ptr)
	applyPointer(out, segs, value)
}

// sortedKeys is provided by to_vcard.go.
func sortedKeysStable(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}