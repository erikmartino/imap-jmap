package vcardconv

import (
	"encoding/json"
	"strings"
	"testing"
)

func jc(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	return m
}

func TestSmokeToVCard(t *testing.T) {
	cases := []struct {
		name string
		card string
		want string // substring expected in output
	}{
		{"name_given", `{"name":{"components":[{"kind":"given","value":"Jane"}]}}`, "FN;DERIVED=TRUE:Jane"},
		{"name_given_N", `{"name":{"components":[{"kind":"given","value":"Jane"}]}}`, "N:;Jane;;;"},
		{"email", `{"emails":{"e1":{"contexts":{"work":true},"address":"jqpublic@xyz.example.com"}}}`, "EMAIL;PROP-ID=e1;TYPE=WORK:jqpublic@xyz.example.com"},
		{"phone", `{"phones":{"tel0":{"contexts":{"private":true},"features":{"voice":true},"number":"tel:+1-555-555-5555;ext=5555","pref":1}}}`, "TEL;VALUE=uri;PROP-ID=tel0;PREF=1;TYPE=HOME,VOICE:tel:+1-555-555-5555;ext=5555"},
		{"uid", `{"uid":"urn:uuid:a8325755-a21d-456a-bb8b-8dc75165164c"}`, "UID:urn:uuid:a8325755-a21d-456a-bb8b-8dc75165164c"},
		{"created", `{"created":"2022-09-30T14:35:10Z"}`, "CREATED;VALUE=timestamp:20220930T143510Z"},
		{"updated", `{"updated":"2021-10-31T22:27:10Z"}`, "REV:20211031T222710Z"},
		{"kind_group", `{"kind":"group","members":{"urn:uuid:03a0e51f-d1aa-4385-8a53-e29025acd8af":true}}`, "KIND:group"},
		{"kind_group_member", `{"kind":"group","members":{"urn:uuid:03a0e51f-d1aa-4385-8a53-e29025acd8af":true}}`, "MEMBER:urn:uuid:03a0e51f-d1aa-4385-8a53-e29025acd8af"},
		{"keywords", `{"keywords":{"internet":true,"IETF":true}}`, "CATEGORIES:internet,IETF"},
		{"personalInfo_expert", `{"personalInfo":{"pi2":{"kind":"expertise","value":"chemistry","level":"high"}}}`, "EXPERTISE;PROP-ID=pi2;LEVEL=expert:chemistry"},
		{"personalInfo_hobby", `{"personalInfo":{"pi1":{"kind":"hobby","value":"reading","level":"high"}}}`, "HOBBY;PROP-ID=pi1;LEVEL=HIGH:reading"},
		{"notes", `{"notes":{"n1":{"note":"Open office hours are 1600 to 1715 EST, Mon-Fri","created":"2022-11-23T15:01:32Z","author":{"name":"John"}}}}`, "NOTE;PROP-ID=n1;CREATED=20221123T150132Z;AUTHOR-NAME=John:Open office hours are 1600 to 1715 EST\\, Mon-Fri"},
		{"org", `{"organizations":{"o1":{"name":"ABC, Inc.","units":[{"name":"North American Division"},{"name":"Marketing"}],"sortAs":"ABC"}}}`, "ORG;SORT-AS=ABC;PROP-ID=o1:ABC\\, Inc.;North American Division;Marketing"},
		{"titles_grouped", `{"titles":{"le9":{"kind":"title","name":"Research Scientist"},"k2":{"kind":"role","name":"Project Leader","organizationId":"o2"}},"organizations":{"o2":{"name":"ABC"}}}`, "group1.ORG;PROP-ID=o2:ABC"},
		{"title_standalone", `{"titles":{"le9":{"kind":"title","name":"Research Scientist"},"k2":{"kind":"role","name":"Project Leader","organizationId":"o2"}},"organizations":{"o2":{"name":"ABC"}}}`, "TITLE;PROP-ID=le9:Research Scientist"},
		{"online_impp", `{"onlineServices":{"x1":{"uri":"xmpp:alice@example.com","vCardName":"impp"}}}`, "IMPP;PROP-ID=x1:xmpp:alice@example.com"},
		{"online_social", `{"onlineServices":{"x2":{"service":"Mastodon","user":"@alice@example2.com","uri":"https://example2.com/@alice","label":"foo"}}}`, "group1.X-SOCIALPROFILE;PROP-ID=x2;SERVICE-TYPE=Mastodon;USERNAME=@alice@example2.com;VALUE=uri:https://example2.com/@alice"},
		{"link", `{"links":{"link3":{"kind":"contact","uri":"mailto:contact@example.com","pref":1}}}`, "CONTACT-URI;PROP-ID=link3;PREF=1:mailto:contact@example.com"},
		{"caluri", `{"calendars":{"calA":{"kind":"calendar","uri":"webcal://calendar.example.com/calA.ics"}}}`, "CALURI;PROP-ID=calA:webcal://calendar.example.com/calA.ics"},
		{"fburl", `{"calendars":{"project-a":{"kind":"freeBusy","uri":"https://calendar.example.com/busy/project-a"}}}`, "FBURL;PROP-ID=project-a:https://calendar.example.com/busy/project-a"},
		{"sched", `{"schedulingAddresses":{"sched1":{"uri":"mailto:janedoe@example.com"}}}`, "CALADRURI;PROP-ID=sched1:mailto:janedoe@example.com"},
		{"adr", `{"addresses":{"a1":{"components":[{"kind":"apartment","value":"apartment-val"},{"kind":"locality","value":"locality-val"},{"kind":"region","value":"region-val"}]}}}`, "ADR;PROP-ID=a1:;apartment-val;;;locality-val;region-val;;;apartment-val;;;;;;;"},
		{"anniv_birth", `{"anniversaries":{"k8":{"kind":"birth","date":{"year":1953,"month":4,"day":15}}}}`, "BDAY;PROP-ID=k8:19530415"},
		{"anniv_death", `{"anniversaries":{"k9":{"kind":"death","date":{"@type":"Timestamp","utc":"2019-10-15T23:10:00Z"},"place":{"full":"4445 Tree Street\nNew England, ND 58647\nUSA"}}}}`, "DEATHDATE;PROP-ID=k9:20191015T231000Z"},
		{"deathplace", `{"anniversaries":{"k9":{"kind":"death","date":{"@type":"Timestamp","utc":"2019-10-15T23:10:00Z"},"place":{"full":"4445 Tree Street\nNew England, ND 58647\nUSA"}}}}`, "DEATHPLACE;PROP-ID=k9:4445 Tree Street\\nNew England\\, ND 58647\\nUSA"},
		{"speak", `{"speakToAs":{"grammaticalGender":"neuter","pronouns":{"k19":{"pronouns":"they/them","pref":2}}}}`, "GRAMGENDER:neuter"},
		{"pronouns", `{"speakToAs":{"grammaticalGender":"neuter","pronouns":{"k19":{"pronouns":"they/them","pref":2}}}}`, "PRONOUNS;PROP-ID=k19;PREF=2:they/them"},
		{"langpref", `{"preferredLanguages":{"l1":{"language":"en","contexts":{"work":true},"pref":1}}}`, "LANG;PROP-ID=l1;TYPE=WORK;PREF=1:en"},
		{"nickname", `{"nicknames":{"n1":{"name":"Johnny"}}}`, "NICKNAME;PROP-ID=n1:Johnny"},
		{"vcardprops", `{"vCardProps":[["x-foo",{"group":"item2","pref":1},"unknown","bar"]]}`, "item2.X-FOO;PREF=1:bar"},
		{"vcardparams", `{"emails":{"e1":{"address":"jqpublic@xyz.example.com","vCardParams":{"x-foo":"bar"}}}}`, "EMAIL;PROP-ID=e1;X-FOO=bar:jqpublic@xyz.example.com"},
		{"vcardprops_photo", `{"name":{"full":"Jane Doe"},"vCardProps":[["photo",{},"uri","https://example.com/hello.jpg"]]}`, "PHOTO;VALUE=URI:https://example.com/hello.jpg"},
		{"vendor", `{"example.com:foo":"bar","name":{"full":"Jane Doe","example.com:foo2":{"bar":"baz"}}}`, "JSPROP;JSPTR=\"example.com:foo\";VALUE=TEXT:\"bar\""},
		{"vendor2", `{"example.com:foo":"bar","name":{"full":"Jane Doe","example.com:foo2":{"bar":"baz"}}}`, "JSPROP;JSPTR=\"name/example.com:foo2\";VALUE=TEXT:{\"bar\":\"baz\"}"},
		{"unknown", `{"foo":"bar"}`, "JSPROP;JSPTR=foo;VALUE=TEXT:\"bar\""},
		{"name_components_ordered", `{"name":{"components":[{"kind":"title","value":"Ms."},{"kind":"given","value":"Mary Jean"},{"kind":"given2","value":"Elizabeth"},{"kind":"surname","value":"van Halen"},{"kind":"surname2","value":"Barrientos"},{"kind":"generation","value":"III"},{"kind":"separator","value":", "},{"kind":"credential","value":"PhD"}],"isOrdered":true}}`, "JSCOMPS=\";3;1;2;0;5;6;s,\\, ;4\""},
		{"name_components_N", `{"name":{"components":[{"kind":"title","value":"Ms."},{"kind":"given","value":"Mary Jean"},{"kind":"given2","value":"Elizabeth"},{"kind":"surname","value":"van Halen"},{"kind":"surname2","value":"Barrientos"},{"kind":"generation","value":"III"},{"kind":"separator","value":", "},{"kind":"credential","value":"PhD"}],"isOrdered":true}}`, "N;JSCOMPS=\";3;1;2;0;5;6;s,\\, ;4\":van Halen,Barrientos;Mary Jean;Elizabeth;Ms.;PhD,III;Barrientos;III"},
		{"name_defaultsep", `{"name":{"defaultSeparator":"X","components":[{"kind":"given","value":"Jane"},{"kind":"surname","value":"Doe"}],"isOrdered":true}}`, "N;JSCOMPS=\"s,X;1;0\":Doe;Jane;;;"},
		{"name_sortas", `{"name":{"components":[{"kind":"given","value":"Robert"},{"kind":"given2","value":"Pau"},{"kind":"surname","value":"Shou Chang"}],"sortAs":{"surname":"Pau Shou Chang","given":"Robert"}}}`, "N;SORT-AS=\"Pau Shou Chang,Robert\":Shou Chang;Robert;Pau;;"},
		{"related", `{"relatedTo":{"urn:uuid:f81d4fae-7dec-11d0-a765-00a0c91e6bf6":{"relation":{"friend":true}}}}`, "RELATED;TYPE=friend:urn:uuid:f81d4fae-7dec-11d0-a765-00a0c91e6bf6"},
		{"related_text", `{"relatedTo":{"8cacdfb7d1ffdb59@example.com":{"relation":{}}}}`, "RELATED;VALUE=text:8cacdfb7d1ffdb59@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ToVCard(jc(t, tc.card))
			if err != nil {
				t.Fatalf("ToVCard: %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("missing %q in output:\n%s", tc.want, out)
			}
		})
	}
}

func TestSmokeInvalidCards(t *testing.T) {
	cases := []string{
		`{"nickNames":{"n1":{"name":"Johnny"}}}`,
		`{"name":{"full":"Jane","Full":"Jane"}}`,
		`{"name":{"full":"Jane"},"extra":"reserved"}`,
		`{"name":{"full":"Jane","extra":"reserved"}}`,
		`{"name":{"full":"Jane"},"localizations":{"de":{"name/extra":"reserved"}}}`,
	}
	for _, c := range cases {
		if _, err := ToVCard(jc(t, c)); err == nil {
			t.Errorf("expected error for card %s", c)
		}
	}
}

func TestSmokeFromVCard(t *testing.T) {
	cases := []struct {
		name  string
		vcard string
		want  string
	}{
		{"name_given", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:x\r\nFN;DERIVED=TRUE:Jane\r\nN:;Jane;;;\r\nEND:VCARD\r\n", `"components":[{"kind":"given","value":"Jane"}]`},
		{"email", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:x\r\nFN:\r\nEMAIL;PROP-ID=e1;TYPE=WORK:jqpublic@xyz.example.com\r\nEND:VCARD\r\n", `"emails":{"e1":{"address":"jqpublic@xyz.example.com","contexts":{"work":true}}}`},
		{"phone", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:x\r\nFN:\r\nTEL;VALUE=uri;PROP-ID=tel0;PREF=1;TYPE=HOME,VOICE:tel:+1-555-555-5555;ext=5555\r\nEND:VCARD\r\n", `"phones":{"tel0":{"number":"tel:+1-555-555-5555;ext=5555","contexts":{"private":true},"features":{"voice":true},"pref":1}}`},
		{"n_jscomps", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:x\r\nFN;DERIVED=TRUE:JaneXDoe\r\nN;JSCOMPS=\"s,X;1;0\":Doe;Jane;;;\r\nEND:VCARD\r\n", `"name":{"components":[{"kind":"given","value":"Jane"},{"kind":"surname","value":"Doe"}],"defaultSeparator":"X","isOrdered":true}`},
		{"adr", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:x\r\nFN:\r\nADR;PROP-ID=a1:;apartment-val;;;locality-val;region-val;;;apartment-val;;;;;;;\r\nEND:VCARD\r\n", `"addresses":{"a1":{"components":[{"kind":"apartment","value":"apartment-val"},{"kind":"locality","value":"locality-val"},{"kind":"region","value":"region-val"}]}}`},
		{"vcardprops", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:x\r\nFN:\r\nitem2.X-FOO;PREF=1:bar\r\nEND:VCARD\r\n", `"vCardProps":[["x-foo",{"group":"item2","pref":1},"unknown","bar"]]`},
		{"jspref", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:x\r\nFN:Jane Doe\r\nJSPROP;JSPTR=\"name/example.com:foo2\";VALUE=TEXT:{\"bar\":\"baz\"}\r\nJSPROP;JSPTR=\"example.com:foo\";VALUE=TEXT:\"bar\"\r\nEND:VCARD\r\n", `"example.com:foo":"bar"`},
		{"jsptr_name", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:x\r\nFN:Jane Doe\r\nJSPROP;JSPTR=\"name/example.com:foo2\";VALUE=TEXT:{\"bar\":\"baz\"}\r\nJSPROP;JSPTR=\"example.com:foo\";VALUE=TEXT:\"bar\"\r\nEND:VCARD\r\n", `"name":{"full":"Jane Doe","example.com:foo2":{"bar":"baz"}}`},
		{"localized", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:x\r\nN;ALTID=1:Vasiliev;Ivan;Petrovich;Mr.;;;\r\nN;ALTID=1;LANGUAGE=uk-Cyrl:Васильев;Иван;Петрович;г-н;;;\r\nFN;DERIVED=TRUE;ALTID=1:Mr. Ivan Petrovich Vasiliev\r\nEND:VCARD\r\n", `"localizations"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := FromVCard(tc.vcard)
			if err != nil {
				t.Fatalf("FromVCard: %v", err)
			}
			b, _ := json.Marshal(out)
			if !strings.Contains(string(b), tc.want) {
				t.Errorf("missing %q in %s", tc.want, b)
			}
		})
	}
}