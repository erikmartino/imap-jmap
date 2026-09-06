// Code generated from jscontact.json. DO NOT EDIT DIRECTLY.
package spec

var JSContactRequirements = []Requirement{
	{
		Spec:    "RFC9553",
		Section: "1",
		Level:   Level("MUST"),
		Text:    "JSContact Card specification",
		Tests:   []string{"TestJSContactSuiteVectors"},
		Status:  Covered,
	},
	{
		Spec:    "RFC9554",
		Section: "1",
		Level:   Level("MUST"),
		Text:    "vCard format extensions for JSContact",
		Tests:   []string{"TestJSContactSuiteVectors"},
		Status:  Covered,
	},
	{
		Spec:    "RFC9555",
		Section: "1",
		Level:   Level("MUST"),
		Text:    "JSContact to and from vCard conversion",
		Tests:   []string{"TestJSContactSuiteVectors"},
		Status:  Covered,
	},
	{
		Spec:    "RFC9555",
		Section: "2.5.2",
		Level:   Level("MUST"),
		Text:    "The FN property converts to the Name object's full property",
		Tests:   []string{"TestJSContactSuiteVectors"},
		Status:  Covered,
	},
	{
		Spec:    "RFC9555",
		Section: "2.6.1",
		Level:   Level("MUST"),
		Text:    "If the JSCOMPS parameter is set, then the Address object's isOrdered property value is true",
		Tests:   []string{"TestJSContactSuiteVectors"},
		Status:  Covered,
	},
	{
		Spec:    "RFC9555",
		Section: "3.3.1",
		Level:   Level("MUST"),
		Text:    "The JSCOMPS parameter value is a structured type value",
		Tests:   []string{"TestJSContactSuiteVectors"},
		Status:  Covered,
	},
}
